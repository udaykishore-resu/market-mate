package handlers

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"market-mate/models"
	"market-mate/search"

	"github.com/gin-gonic/gin"
)

// searchTimeout bounds a search request. Shorter than the pipeline ceiling: a
// search is a keystroke away from being retried, so failing fast beats holding
// the connection.
const searchTimeout = 10 * time.Second

// reindexTimeout bounds a full replay from Postgres. It is an admin operation
// over a bounded table, not a background job.
const reindexTimeout = 2 * time.Minute

// SearchRecipes answers GET /api/recipes/search.
//
// Repeated ?ingredient= narrows the result to recipes using any of the named
// ingredients; ?q= is a free-text match over titles, channels and ingredient
// names. Both are optional, and with neither the endpoint lists what has been
// indexed — which is what the browse view wants.
func (h *VideoHandler) SearchRecipes(c *gin.Context) {
	query := search.Query{
		Text:        c.Query("q"),
		Ingredients: c.QueryArray("ingredient"),
	}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error: "limit must be a positive whole number.",
				Stage: "request",
			})
			return
		}
		query.Limit = n
	}
	query = query.Normalise()

	ctx, cancel := context.WithTimeout(c.Request.Context(), searchTimeout)
	defer cancel()

	results, err := h.config.Search.Search(ctx, query)
	if err != nil {
		log.Printf("search failed for %q: %v", query.Text, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error: "Recipe search is unavailable right now.",
			Stage: "search",
		})
		return
	}
	if results == nil {
		results = []models.Recipe{}
	}

	c.JSON(http.StatusOK, models.SearchResponse{
		Query:   query.Text,
		Results: results,
		Total:   len(results),
		Backend: h.config.Search.Kind(),
		Notice:  searchNotice(h.config.Search.Kind(), results),
	})
}

// Reindex answers POST /api/admin/reindex by replaying every recipe from
// Postgres into the search index.
//
// Postgres is the system of record and Elasticsearch is derived, so this is the
// repair tool for the one thing that can go wrong with a derived store: it
// having missed a write. It is deliberately synchronous and bounded — a
// background job that reports nothing is not a repair tool.
func (h *VideoHandler) Reindex(c *gin.Context) {
	if h.config.Store == nil {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error: "Reindex needs the Postgres record to replay from. Set MM_POSTGRES_DSN.",
			Stage: "reindex",
		})
		return
	}
	if h.config.Search.Kind() != search.KindElasticsearch {
		// Nothing to replay into: the other backends read the record directly.
		c.JSON(http.StatusOK, gin.H{
			"indexed": 0,
			"backend": h.config.Search.Kind(),
			"notice":  "No external index is configured, so there is nothing to rebuild.",
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), reindexTimeout)
	defer cancel()

	recipes, err := h.config.Store.Recipes(ctx, 0)
	if err != nil {
		log.Printf("reindex: reading recipes: %v", err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error: "Could not read the stored recipes to reindex.",
			Stage: "reindex",
		})
		return
	}

	var indexed, failed int
	for _, r := range recipes {
		if err := h.config.Search.Index(ctx, r); err != nil {
			// Keep going: one poison document should not leave the rest of the
			// index stale.
			log.Printf("reindex: indexing %s: %v", r.VideoID, err)
			failed++
			continue
		}
		indexed++
	}

	c.JSON(http.StatusOK, gin.H{
		"indexed": indexed,
		"failed":  failed,
		"total":   len(recipes),
		"backend": h.config.Search.Kind(),
	})
}

// searchNotice tells the caller when an empty or thin result is a property of
// the deployment rather than of the query.
func searchNotice(backend string, results []models.Recipe) string {
	switch backend {
	case search.KindDisabled:
		return "Recipe search is not configured on this deployment: set MM_ELASTIC_URL, or MM_POSTGRES_DSN for a basic substring search."
	case search.KindPostgres:
		if len(results) == 0 {
			return "Searching stored recipes directly: no Elasticsearch is configured, so this is a substring match with no stemming or ranking."
		}
	}
	return ""
}
