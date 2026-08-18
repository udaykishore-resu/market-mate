// Package handlers holds the HTTP layer: request decoding, the pipeline
// orchestration for a video request, search, and the per-dependency health
// report. Business rules live in services and storage, not here.
package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"market-mate/models"
	"market-mate/search"
	"market-mate/services"
	"market-mate/storage"

	"github.com/gin-gonic/gin"
)

// VideoHandlerConfig wires the handler to its providers.
//
// These are interfaces now, not concrete service pointers. That single change
// is what lets the handler be exercised in a test without three API keys and a
// network — and the absence of that seam is why the URL-parser panic and the
// hardcoded San Francisco coordinate both shipped.
//
// Every field below the providers is optional and nil-safe. The handler answers
// with no Postgres, no Redis and no Elasticsearch; those only remove work it
// would otherwise repeat.
type VideoHandlerConfig struct {
	VideoService        services.VideoProvider
	StoreFinder         services.StoreProvider
	IngredientExtractor services.IngredientProvider
	CacheService        services.Cache
	LocationService     services.LocationProvider

	// Store is the permanent transcript and extraction record. Nil when
	// MM_POSTGRES_DSN is empty, in which case every miss re-fetches.
	Store *storage.Store

	// Search indexes extractions and answers /api/recipes/search. Nil is
	// treated as search.Disabled.
	Search search.Searcher

	// StoreLookup is the shared, de-duplicated path to the store provider. Left
	// nil it is built from StoreFinder; main supplies one so that REST and
	// GraphQL collapse onto the same in-flight call.
	StoreLookup *services.StoreLookup

	// StoreCacheTTL bounds the store lookup only. Transcripts and extractions
	// are permanent; shop opening hours are not.
	StoreCacheTTL time.Duration

	// Simulated records which providers are fixtures, for the response's
	// provenance block.
	Simulated models.Provenance

	// Ready is cleared during shutdown so the health endpoint can report 503
	// while in-flight requests drain. Nil means always ready.
	Ready *atomic.Bool
}

type VideoHandler struct {
	config VideoHandlerConfig

	stores *services.StoreLookup

	// modelVersion names the model and prompt that produced an extraction. It
	// is the key half that makes the permanent cache safe: change either and
	// stored lists stop being read.
	modelVersion string

	// videoSource labels rows this process writes, and gates the ones it reads:
	// a transcript written by the fixture provider is never served to a request
	// answered live.
	videoSource string
}

func NewVideoHandler(cfg VideoHandlerConfig) *VideoHandler {
	if cfg.CacheService == nil {
		cfg.CacheService = services.NewCacheService()
	}
	if cfg.Search == nil {
		cfg.Search = search.Disabled{}
	}
	if cfg.StoreCacheTTL <= 0 {
		cfg.StoreCacheTTL = services.DefaultStoreCacheTTL
	}

	source := models.SourceYouTube
	if cfg.Simulated.Video {
		source = models.SourceFixture
	}

	if cfg.StoreLookup == nil {
		cfg.StoreLookup = services.NewStoreLookup(cfg.StoreFinder, cfg.CacheService, cfg.StoreCacheTTL)
	}

	return &VideoHandler{
		config:       cfg,
		stores:       cfg.StoreLookup,
		modelVersion: services.ProviderModelVersion(cfg.IngredientExtractor),
		videoSource:  source,
	}
}

// ModelVersion is what this process will write to and read from the extraction
// cache. Logged at boot: without it, a stale-looking result is impossible to
// diagnose.
func (h *VideoHandler) ModelVersion() string { return h.modelVersion }

// pipelineTimeout bounds the whole request. The three upstream calls are
// sequential, so without a ceiling a slow model response can hold a connection
// open far longer than any client will wait.
const pipelineTimeout = 30 * time.Second

// persistTimeout bounds the writes that follow a successful extraction. They
// are detached from the request context on purpose: the user's answer is
// already computed, and a client that hangs up must not leave a half-written
// cache behind.
const persistTimeout = 5 * time.Second

func (h *VideoHandler) ProcessVideo(c *gin.Context) {
	var request struct {
		URL string `json:"url"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: `Request body must be JSON of the form {"url": "..."}.`,
			Stage: "request",
		})
		return
	}

	// FR-004/005/006: a parse failure is a 400 with a real message — not a
	// garbage ID handed to the YouTube API, and not a panic.
	videoID, err := services.ParseVideoID(request.URL)
	if err != nil {
		msg := "That does not look like a YouTube video link. Try something like https://youtu.be/dQw4w9WgXcQ."
		if strings.TrimSpace(request.URL) == "" {
			msg = "Please paste a YouTube video link."
		}
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: msg, Stage: "url"})
		return
	}

	// Derived from the request context, so a client disconnect cancels the
	// upstream calls rather than letting them run to completion.
	ctx, cancel := context.WithTimeout(c.Request.Context(), pipelineTimeout)
	defer cancel()

	// FR-007: the search centre comes from the client's IP, with a bounded
	// timeout and a documented fallback. The literal 37.7749/-122.4194 that
	// used to sit in this function is gone.
	location, resolved := h.config.LocationService.Resolve(ctx, c.ClientIP())

	// FR-009: a cache hit returns the whole result with zero provider calls.
	// This layer keeps a TTL because it contains the store list; the transcript
	// and extraction underneath it are permanent in Postgres.
	cacheKey := services.ResultKey(videoID, location.Latitude, location.Longitude)
	var cached models.RecipeResponse
	if h.config.CacheService.Get(ctx, cacheKey, &cached) {
		cached.Cached = true
		c.JSON(http.StatusOK, cached)
		return
	}

	video, err := h.videoDetails(ctx, videoID)
	if err != nil {
		log.Printf("video lookup failed for %s: %v", videoID, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error: "Could not fetch that video from YouTube. It may be private, deleted, or the ID may be wrong.",
			Stage: "video",
		})
		return
	}

	ingredients, err := h.extractIngredients(ctx, video)
	if err != nil {
		log.Printf("ingredient extraction failed for %s: %v", videoID, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error: "Could not read the ingredients from that video's description.",
			Stage: "ingredients",
		})
		return
	}

	stores, _, err := h.stores.Stores(ctx, videoID, location.Latitude, location.Longitude)
	if err != nil {
		log.Printf("store lookup failed at %.4f,%.4f: %v", location.Latitude, location.Longitude, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error: "Found the ingredients, but could not look up nearby stores.",
			Stage: "stores",
		})
		return
	}

	response := models.RecipeResponse{
		Ingredients: ingredients,
		Stores:      stores,
		Video: &models.Video{
			ID:           video.ID,
			Title:        video.Title,
			ChannelTitle: video.ChannelTitle,
			ThumbnailURL: video.ThumbnailURL,
		},
		Location: models.SearchLocation{
			Latitude:  location.Latitude,
			Longitude: location.Longitude,
			Label:     location.Label,
			Estimated: !resolved,
		},
		Simulated: h.config.Simulated,
		Notice:    buildNotice(h.config.Simulated, len(ingredients) == 0),
	}

	// Detached from the request context: the answer is computed, and a client
	// that hangs up now should still leave the next one a warm cache.
	writeCtx, cancelWrite := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer cancelWrite()
	h.config.CacheService.Set(writeCtx, cacheKey, response, h.config.StoreCacheTTL)
	c.JSON(http.StatusOK, response)
}

// videoDetails reads the transcript from Postgres when it is there and fetches
// it otherwise.
//
// A YouTube video's title and description do not change in any way this product
// cares about, so re-fetching them on every cache expiry was paying quota for a
// known answer. The source check is what keeps that safe: a row written while
// running on fixtures is not an answer to a live request.
func (h *VideoHandler) videoDetails(ctx context.Context, videoID string) (*services.VideoDetails, error) {
	if h.config.Store != nil {
		row, found, err := h.config.Store.Video(ctx, videoID)
		switch {
		case err != nil:
			// A degraded cache is not a failed request: fall through and fetch.
			log.Printf("storage: reading video %s: %v", videoID, err)
		case found && row.Source == h.videoSource:
			return &services.VideoDetails{
				ID:              row.VideoID,
				Title:           row.Title,
				Description:     row.Transcript,
				ChannelTitle:    row.Channel,
				DurationSeconds: row.DurationSeconds,
				// Reconstructed rather than stored: it is a pure function of
				// the ID, and both providers already build it this way.
				ThumbnailURL: fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", row.VideoID),
			}, nil
		}
	}

	video, err := h.config.VideoService.GetVideoDetails(ctx, videoID)
	if err != nil {
		return nil, err
	}
	if h.config.Store != nil {
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
		defer cancel()
		if err := h.config.Store.SaveVideo(saveCtx, storage.Video{
			VideoID:         video.ID,
			Title:           video.Title,
			Channel:         video.ChannelTitle,
			DurationSeconds: video.DurationSeconds,
			Transcript:      video.Description,
			Source:          h.videoSource,
		}); err != nil {
			log.Printf("storage: saving video %s: %v", videoID, err)
		}
	}
	return video, nil
}

// extractIngredients reads the stored list for the current model version, or
// extracts and stores one.
//
// The list is only ever read back under the exact model-and-prompt fingerprint
// that produced it, so editing the prompt invalidates cleanly instead of
// serving output the current prompt would not generate.
func (h *VideoHandler) extractIngredients(ctx context.Context, video *services.VideoDetails) ([]models.Ingredient, error) {
	versioned := h.modelVersion != services.UnversionedModel

	if h.config.Store != nil && versioned {
		row, found, err := h.config.Store.Extraction(ctx, video.ID, h.modelVersion)
		switch {
		case err != nil:
			log.Printf("storage: reading extraction %s: %v", video.ID, err)
		case found:
			return nonNilIngredients(row.Ingredients), nil
		}
	}

	ingredients, err := h.config.IngredientExtractor.ExtractIngredients(ctx, video.Description)
	if err != nil {
		return nil, err
	}
	ingredients = nonNilIngredients(ingredients)

	if versioned {
		h.persistExtraction(ctx, video, ingredients)
	}
	return ingredients, nil
}

// persistExtraction stores the list and makes it searchable. Both are best
// effort: a search index that is behind is a worse product, but a failed write
// to it is not a reason to fail a request that already has its answer.
func (h *VideoHandler) persistExtraction(ctx context.Context, video *services.VideoDetails, ingredients []models.Ingredient) {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer cancel()

	if h.config.Store != nil {
		if err := h.config.Store.SaveExtraction(saveCtx, storage.Extraction{
			VideoID:      video.ID,
			ModelVersion: h.modelVersion,
			Ingredients:  ingredients,
		}); err != nil {
			log.Printf("storage: saving extraction %s: %v", video.ID, err)
		}
	}

	if h.config.Search.Kind() == search.KindDisabled {
		return
	}
	if err := h.config.Search.Index(saveCtx, h.recipeFor(video, ingredients)); err != nil {
		log.Printf("search: indexing %s: %v", video.ID, err)
	}
}

// recipeFor builds the searchable record. Provenance is set here and travels
// with the document: a fixture extraction stays labelled in the index, so a
// search result can never present simulated data as live.
func (h *VideoHandler) recipeFor(video *services.VideoDetails, ingredients []models.Ingredient) models.Recipe {
	return models.Recipe{
		VideoID:      video.ID,
		Title:        video.Title,
		Channel:      video.ChannelTitle,
		Ingredients:  ingredients,
		Source:       h.videoSource,
		Simulated:    h.config.Simulated.Video || h.config.Simulated.Ingredients,
		ModelVersion: h.modelVersion,
		IndexedAt:    time.Now().UTC(),
	}
}

// nonNilIngredients keeps the JSON an array. A nil slice encodes as `null` and
// the frontend maps over this field directly.
func nonNilIngredients(in []models.Ingredient) []models.Ingredient {
	if in == nil {
		return []models.Ingredient{}
	}
	return in
}

// buildNotice explains anything the user should know before trusting a response.
func buildNotice(sim models.Provenance, noIngredients bool) string {
	var parts []string
	if sim.Any {
		var which []string
		if sim.Video {
			which = append(which, "video details")
		}
		if sim.Ingredients {
			which = append(which, "ingredient extraction")
		}
		if sim.Stores {
			which = append(which, "store search")
		}
		// No "Demo mode:" prefix here — the UI banner is already headed that
		// way, and repeating it reads as a stutter.
		parts = append(parts, capitalise(strings.Join(which, ", "))+
			" simulated because no API key is configured for them.")
	}
	if noIngredients {
		parts = append(parts,
			"No ingredients were found in this video's description. Many creators put the recipe in a pinned comment or a blog link instead.")
	}
	return strings.Join(parts, " ")
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
