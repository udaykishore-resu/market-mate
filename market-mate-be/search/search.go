// Package search provides recipe and ingredient search over Elasticsearch,
// with a Postgres scan as the documented fallback.
//
// The Elasticsearch client here is hand-written against the REST API rather
// than go-elasticsearch: this service makes three calls (create index, index a
// document, search), and the official client is a large generated dependency
// pinned to a server major version. Three calls of net/http are cheaper to read
// and cheaper to keep working.
package search

import (
	"context"
	"strings"

	"market-mate/models"
)

// Kind values name the implementation that answered. They appear in the search
// response and the health report, so a caller can tell an empty index from a
// search layer that is not running.
const (
	KindElasticsearch = "elasticsearch"
	KindPostgres      = "postgres"
	KindDisabled      = "disabled"
)

// DefaultLimit is the page size when the caller does not ask for one.
const DefaultLimit = 20

// MaxLimit caps what a caller can ask for: this endpoint is unauthenticated and
// a limit of 10000 is a cheap way to make the service do expensive work.
const MaxLimit = 100

// Query is one search request. An empty Text with no Ingredients matches
// everything, which is what the UI's initial "browse" view wants.
type Query struct {
	Text        string
	Ingredients []string
	Limit       int
}

// Normalise applies the limit bounds and drops blank ingredient filters, so
// every backend sees the same request.
func (q Query) Normalise() Query {
	out := Query{Text: strings.TrimSpace(q.Text), Limit: q.Limit}
	if out.Limit <= 0 {
		out.Limit = DefaultLimit
	}
	if out.Limit > MaxLimit {
		out.Limit = MaxLimit
	}
	for _, ing := range q.Ingredients {
		// Lower-cased to match the index's normalised keyword field; the
		// Postgres fallback lower-cases the same way.
		if t := strings.ToLower(strings.TrimSpace(ing)); t != "" {
			out.Ingredients = append(out.Ingredients, t)
		}
	}
	return out
}

// Searcher is the seam between the two search implementations, in the same
// shape as the video and store providers: main picks one at boot and nothing
// downstream knows which it got.
type Searcher interface {
	// Index makes a recipe findable. It is a no-op for backends that search the
	// system of record directly.
	Index(ctx context.Context, recipe models.Recipe) error
	Search(ctx context.Context, q Query) ([]models.Recipe, error)
	Health(ctx context.Context) error
	// Kind names the implementation for the health endpoint and the search
	// response, so a caller can tell "no matches" from "no search backend".
	Kind() string
}

// RecipeSource is the part of the Postgres store the fallback needs. Declaring
// it here rather than importing storage keeps the dependency pointing one way.
type RecipeSource interface {
	SearchRecipes(ctx context.Context, query string, ingredients []string, limit int) ([]models.Recipe, error)
	Ping(ctx context.Context) error
}

// DBSearcher answers searches with a linear scan over Postgres.
type DBSearcher struct {
	source RecipeSource
}

func NewDBSearcher(source RecipeSource) *DBSearcher { return &DBSearcher{source: source} }

// Index is a no-op: the rows are already in the table this searcher scans.
func (d *DBSearcher) Index(context.Context, models.Recipe) error { return nil }

func (d *DBSearcher) Search(ctx context.Context, q Query) ([]models.Recipe, error) {
	q = q.Normalise()
	return d.source.SearchRecipes(ctx, q.Text, q.Ingredients, q.Limit)
}

func (d *DBSearcher) Health(ctx context.Context) error { return d.source.Ping(ctx) }
func (d *DBSearcher) Kind() string                     { return KindPostgres }

// Disabled is the searcher used when there is neither Elasticsearch nor
// Postgres. It answers every query with an empty result rather than an error:
// the endpoint stays available, and Kind tells the caller why it is empty.
type Disabled struct{}

func (Disabled) Index(context.Context, models.Recipe) error { return nil }

func (Disabled) Search(context.Context, Query) ([]models.Recipe, error) {
	return []models.Recipe{}, nil
}

func (Disabled) Health(context.Context) error { return nil }
func (Disabled) Kind() string                 { return KindDisabled }

var (
	_ Searcher = (*Client)(nil)
	_ Searcher = (*DBSearcher)(nil)
	_ Searcher = Disabled{}
)
