package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"market-mate/models"
	"market-mate/services"

	"github.com/gin-gonic/gin"
)

// VideoHandlerConfig wires the handler to its providers.
//
// These are interfaces now, not concrete service pointers. That single change
// is what lets the handler be exercised in a test without three API keys and a
// network — and the absence of that seam is why the URL-parser panic and the
// hardcoded San Francisco coordinate both shipped.
type VideoHandlerConfig struct {
	VideoService        services.VideoProvider
	StoreFinder         services.StoreProvider
	IngredientExtractor services.IngredientProvider
	CacheService        *services.CacheService
	LocationService     services.LocationProvider

	// Simulated records which providers are fixtures, for the response's
	// provenance block.
	Simulated models.Provenance
}

type VideoHandler struct {
	config VideoHandlerConfig
}

func NewVideoHandler(cfg VideoHandlerConfig) *VideoHandler {
	return &VideoHandler{config: cfg}
}

// pipelineTimeout bounds the whole request. The three upstream calls are
// sequential, so without a ceiling a slow model response can hold a connection
// open far longer than any client will wait.
const pipelineTimeout = 30 * time.Second

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
	cacheKey := services.ResultKey(videoID, location.Latitude, location.Longitude)
	if cached, found := h.config.CacheService.Get(cacheKey); found {
		if resp, ok := cached.(models.RecipeResponse); ok {
			resp.Cached = true
			c.JSON(http.StatusOK, resp)
			return
		}
	}

	video, err := h.config.VideoService.GetVideoDetails(ctx, videoID)
	if err != nil {
		log.Printf("video lookup failed for %s: %v", videoID, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error: "Could not fetch that video from YouTube. It may be private, deleted, or the ID may be wrong.",
			Stage: "video",
		})
		return
	}

	ingredients, err := h.config.IngredientExtractor.ExtractIngredients(ctx, video.Description)
	if err != nil {
		log.Printf("ingredient extraction failed for %s: %v", videoID, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error: "Could not read the ingredients from that video's description.",
			Stage: "ingredients",
		})
		return
	}
	if ingredients == nil {
		ingredients = []models.Ingredient{}
	}

	stores, err := h.config.StoreFinder.FindNearbyStores(ctx, location.Latitude, location.Longitude)
	if err != nil {
		log.Printf("store lookup failed at %.4f,%.4f: %v", location.Latitude, location.Longitude, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error: "Found the ingredients, but could not look up nearby stores.",
			Stage: "stores",
		})
		return
	}
	if stores == nil {
		stores = []models.Store{}
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

	h.config.CacheService.Set(cacheKey, response)
	c.JSON(http.StatusOK, response)
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

// Health reports service status and which providers are live, so a deploy
// target has something to probe and an operator can see at a glance whether a
// demo build reached production by accident.
func (h *VideoHandler) Health(c *gin.Context) {
	hits, misses, items := h.config.CacheService.Stats()
	mode := "live"
	if h.config.Simulated.Any {
		mode = "demo"
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "market-mate",
		"mode":    mode,
		"providers": gin.H{
			"video":       providerMode(h.config.Simulated.Video),
			"ingredients": providerMode(h.config.Simulated.Ingredients),
			"stores":      providerMode(h.config.Simulated.Stores),
		},
		"cache": gin.H{"hits": hits, "misses": misses, "items": items},
		"time":  time.Now().UTC(),
	})
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func providerMode(simulated bool) string {
	if simulated {
		return "simulated"
	}
	return "live"
}
