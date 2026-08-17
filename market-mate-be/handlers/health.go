package handlers

import (
	"net/http"

	"market-mate/health"

	"github.com/gin-gonic/gin"
)

// Health reports service status, which provider each capability resolved to,
// and the state of every optional dependency.
//
// The response stays 200 when a configured dependency is unreachable and says
// "degraded": MarketMate can still answer from its providers with no Postgres,
// no Redis and no Elasticsearch, and a probe that fails the pod over a cache
// outage would turn a slow demo into an outage. 503 is reserved for the one
// case where the process genuinely cannot serve — shutdown, where Ready has
// been cleared and the server is draining.
func (h *VideoHandler) Health(c *gin.Context) {
	report := h.checker().Report(c.Request.Context())

	if h.config.Ready != nil && !h.config.Ready.Load() {
		report.Status = "shutting_down"
		c.JSON(http.StatusServiceUnavailable, report)
		return
	}
	c.JSON(http.StatusOK, report)
}

// Checker exposes the same probes to other transports — the GraphQL health
// field resolves through this, so the two can never disagree.
func (h *VideoHandler) checker() health.Checker {
	return health.Checker{
		Store:        h.config.Store,
		Cache:        h.config.CacheService,
		Search:       h.config.Search,
		Simulated:    h.config.Simulated,
		ModelVersion: h.modelVersion,
	}
}
