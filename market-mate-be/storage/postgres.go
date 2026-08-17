// Package storage is MarketMate's durable layer: the permanent transcript and
// extraction cache that the fifteen-minute in-process cache never was.
//
// Everything here is optional. With MM_POSTGRES_DSN unset the service runs
// exactly as before — the pipeline just re-fetches and re-extracts on every
// cache miss, which is the behaviour this package exists to end.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"market-mate/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryTimeout bounds every statement. A pipeline request already has a 30s
// ceiling; a cache lookup that eats it is worse than a cache miss, so the store
// gives up long before the request does.
const queryTimeout = 5 * time.Second

// defaultMaxConns keeps the pool small: this service issues short key lookups,
// and a large pool on several replicas exhausts Postgres' connection slots
// faster than it helps.
const defaultMaxConns = 10

// Store is the Postgres-backed record of videos and their extractions.
type Store struct {
	pool *pgxpool.Pool
}

// Video is one row of the videos table.
type Video struct {
	VideoID         string
	Title           string
	Channel         string
	DurationSeconds int
	Transcript      string
	Source          string
	FetchedAt       time.Time
}

// Extraction is one row of the extractions table: a video's ingredient list as
// produced by one specific model-and-prompt combination.
type Extraction struct {
	VideoID      string
	ModelVersion string
	Ingredients  []models.Ingredient
	ExtractedAt  time.Time
}

// Open dials Postgres and verifies the connection before returning, so a bad
// DSN is a boot-time log line and a documented fallback rather than a failure
// on the first user request.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing MM_POSTGRES_DSN: %w", err)
	}
	if cfg.MaxConns == 0 || cfg.MaxConns > defaultMaxConns {
		cfg.MaxConns = defaultMaxConns
	}
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connecting to Postgres: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	return s.pool.Ping(ctx)
}

// SaveVideo upserts a video. The transcript is immutable in practice, but a
// re-fetch may carry a better title or a duration the first fetch lacked, so
// the row is refreshed rather than left alone.
func (s *Store) SaveVideo(ctx context.Context, v Video) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	_, err := s.pool.Exec(ctx, `
		INSERT INTO videos (video_id, title, channel, duration_seconds, transcript, source, fetched_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (video_id) DO UPDATE SET
			title            = EXCLUDED.title,
			channel          = EXCLUDED.channel,
			duration_seconds = EXCLUDED.duration_seconds,
			transcript       = EXCLUDED.transcript,
			source           = EXCLUDED.source,
			fetched_at       = now()`,
		v.VideoID, v.Title, v.Channel, v.DurationSeconds, v.Transcript, v.Source)
	if err != nil {
		return fmt.Errorf("saving video %s: %w", v.VideoID, err)
	}
	return nil
}

// Video reads one video. The bool reports presence; a missing row is not an
// error, it is the ordinary cold-cache case.
func (s *Store) Video(ctx context.Context, videoID string) (*Video, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var v Video
	err := s.pool.QueryRow(ctx, `
		SELECT video_id, title, channel, duration_seconds, transcript, source, fetched_at
		FROM videos WHERE video_id = $1`, videoID).
		Scan(&v.VideoID, &v.Title, &v.Channel, &v.DurationSeconds, &v.Transcript, &v.Source, &v.FetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading video %s: %w", videoID, err)
	}
	return &v, true, nil
}

// SaveExtraction stores an ingredient list under its model version. Re-running
// the same model and prompt overwrites in place: the newer list is the better
// one, and the pair is the primary key precisely so this cannot fan out.
func (s *Store) SaveExtraction(ctx context.Context, e Extraction) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	ingredients := e.Ingredients
	if ingredients == nil {
		ingredients = []models.Ingredient{}
	}
	raw, err := json.Marshal(ingredients)
	if err != nil {
		return fmt.Errorf("encoding ingredients for %s: %w", e.VideoID, err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO extractions (video_id, model_version, ingredients, extracted_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (video_id, model_version) DO UPDATE SET
			ingredients  = EXCLUDED.ingredients,
			extracted_at = now()`,
		e.VideoID, e.ModelVersion, raw)
	if err != nil {
		return fmt.Errorf("saving extraction %s/%s: %w", e.VideoID, e.ModelVersion, err)
	}
	return nil
}

// Extraction reads the list for one video under one model version. Asking for a
// version that does not exist is a miss, which is exactly how a prompt change
// invalidates: the new fingerprint simply has no rows yet.
func (s *Store) Extraction(ctx context.Context, videoID, modelVersion string) (*Extraction, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var (
		e   = Extraction{VideoID: videoID, ModelVersion: modelVersion}
		raw []byte
	)
	err := s.pool.QueryRow(ctx, `
		SELECT ingredients, extracted_at
		FROM extractions WHERE video_id = $1 AND model_version = $2`, videoID, modelVersion).
		Scan(&raw, &e.ExtractedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading extraction %s/%s: %w", videoID, modelVersion, err)
	}
	if err := json.Unmarshal(raw, &e.Ingredients); err != nil {
		return nil, false, fmt.Errorf("decoding ingredients for %s: %w", videoID, err)
	}
	return &e, true, nil
}

const recipeColumns = `video_id, title, channel, source, model_version, ingredients, extracted_at`

// Recipe reads one composed recipe from the view.
func (s *Store) Recipe(ctx context.Context, videoID string) (*models.Recipe, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	row := s.pool.QueryRow(ctx, `SELECT `+recipeColumns+` FROM recipes WHERE video_id = $1`, videoID)
	r, err := scanRecipe(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading recipe %s: %w", videoID, err)
	}
	return &r, true, nil
}

// Recipes reads the whole view, newest first. It backs the reindex endpoint,
// where the alternative — asking Elasticsearch what it already has — cannot
// distinguish a missing document from one that was never indexed.
func (s *Store) Recipes(ctx context.Context, limit int) ([]models.Recipe, error) {
	if limit <= 0 {
		limit = 1000
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := s.pool.Query(ctx,
		`SELECT `+recipeColumns+` FROM recipes ORDER BY extracted_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("listing recipes: %w", err)
	}
	defer rows.Close()
	return collectRecipes(rows)
}

// SearchRecipes is the degraded search path, used when MM_ELASTIC_URL is empty.
//
// It is a linear ILIKE scan and it is honest about that: no stemming, no
// ranking, no phrase handling. It exists so the search endpoint answers
// something true on a laptop with no Elasticsearch, not so it can pretend to be
// one.
func (s *Store) SearchRecipes(ctx context.Context, query string, ingredients []string, limit int) ([]models.Recipe, error) {
	if limit <= 0 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Lower-cased here so the comparison matches the Elasticsearch path, whose
	// ingredient keyword field is normalised the same way.
	terms := make([]string, 0, len(ingredients))
	for _, ing := range ingredients {
		if t := strings.ToLower(strings.TrimSpace(ing)); t != "" {
			terms = append(terms, t)
		}
	}

	rows, err := s.pool.Query(ctx, `
		SELECT `+recipeColumns+` FROM recipes
		WHERE ($1 = '' OR title ILIKE '%' || $1 || '%'
		              OR channel ILIKE '%' || $1 || '%'
		              OR ingredients::text ILIKE '%' || $1 || '%')
		  AND (cardinality($2::text[]) = 0 OR EXISTS (
		        SELECT 1 FROM jsonb_array_elements(ingredients) AS ing
		        WHERE lower(ing->>'name') = ANY($2::text[])))
		ORDER BY extracted_at DESC
		LIMIT $3`, strings.TrimSpace(query), terms, limit)
	if err != nil {
		return nil, fmt.Errorf("searching recipes: %w", err)
	}
	defer rows.Close()
	return collectRecipes(rows)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRecipe(row scanner) (models.Recipe, error) {
	var (
		r   models.Recipe
		raw []byte
	)
	if err := row.Scan(&r.VideoID, &r.Title, &r.Channel, &r.Source, &r.ModelVersion, &raw, &r.IndexedAt); err != nil {
		return models.Recipe{}, err
	}
	if err := json.Unmarshal(raw, &r.Ingredients); err != nil {
		return models.Recipe{}, fmt.Errorf("decoding ingredients for %s: %w", r.VideoID, err)
	}
	if r.Ingredients == nil {
		r.Ingredients = []models.Ingredient{}
	}
	// Provenance survives the round trip: a row written by the fixture provider
	// stays labelled all the way out to the search index and the API.
	r.Simulated = r.Source == models.SourceFixture
	return r, nil
}

func collectRecipes(rows pgx.Rows) ([]models.Recipe, error) {
	out := []models.Recipe{}
	for rows.Next() {
		r, err := scanRecipe(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
