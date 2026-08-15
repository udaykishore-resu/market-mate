package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type RateLimiter struct {
	requests map[string][]time.Time
	mu       sync.Mutex
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
	}
}

func (rl *RateLimiter) RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		rl.mu.Lock()
		defer rl.mu.Unlock()

		now := time.Now()
		windowStart := now.Add(-time.Minute)

		// Clean old requests
		if requests, exists := rl.requests[ip]; exists {
			var valid []time.Time
			for _, t := range requests {
				if t.After(windowStart) {
					valid = append(valid, t)
				}
			}
			rl.requests[ip] = valid
		}

		// Check rate limit (100 requests per minute)
		if len(rl.requests[ip]) >= 100 {
			// Retry-After tells the client when to come back instead of
			// leaving it to guess or hammer (FR-012). The window is a rolling
			// minute, so the wait is until the oldest request ages out.
			retryAfter := 60
			if oldest := rl.requests[ip]; len(oldest) > 0 {
				if secs := int(time.Until(oldest[0].Add(time.Minute)).Seconds()) + 1; secs > 0 && secs <= 60 {
					retryAfter = secs
				}
			}
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded. Try again shortly.",
				"stage": "rate_limit",
			})
			c.Abort()
			return
		}

		rl.requests[ip] = append(rl.requests[ip], now)
		c.Next()
	}
}
