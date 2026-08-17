package services

import (
	"context"
	"sync"
	"time"

	"market-mate/models"
)

// ProviderRadiusMeters is how wide the live provider searches. Requests for a
// larger radius cannot be served by narrowing that result, so they are capped
// here rather than silently answered with a smaller set.
const ProviderRadiusMeters = 5000

// StoreLookup is the cached, de-duplicated entry point to the store provider.
//
// Two callers need it and neither should own it: the REST pipeline resolves
// stores once per request, and a GraphQL query resolves them once per ingredient
// or recipe in the result set. Without the collapsing below, a ten-ingredient
// query is ten identical Places calls — billed ten times, and the tenth is no
// fresher than the first.
type StoreLookup struct {
	provider StoreProvider
	cache    Cache
	ttl      time.Duration

	mu       sync.Mutex
	inflight map[string]*storeCall
}

type storeCall struct {
	done   chan struct{}
	stores []models.Store
	err    error
}

func NewStoreLookup(provider StoreProvider, c Cache, ttl time.Duration) *StoreLookup {
	if c == nil {
		c = NewMemoryCache(ttl)
	}
	if ttl <= 0 {
		ttl = DefaultStoreCacheTTL
	}
	return &StoreLookup{
		provider: provider,
		cache:    c,
		ttl:      ttl,
		inflight: make(map[string]*storeCall),
	}
}

// Stores returns the grocery stores around a coordinate. The bool reports a
// cache hit, which the response's `cached` flag and the health counters both
// care about.
func (l *StoreLookup) Stores(ctx context.Context, videoID string, lat, lng float64) ([]models.Store, bool, error) {
	key := StoreKey(videoID, lat, lng)

	var cached []models.Store
	if l.cache.Get(ctx, key, &cached) {
		return cached, true, nil
	}

	// Collapse concurrent misses for the same cell onto one provider call. The
	// waiters share the winner's result, including its error: a failing upstream
	// should not be hammered once per waiter.
	l.mu.Lock()
	if call, ok := l.inflight[key]; ok {
		l.mu.Unlock()
		select {
		case <-call.done:
			return call.stores, false, call.err
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	call := &storeCall{done: make(chan struct{})}
	l.inflight[key] = call
	l.mu.Unlock()

	call.stores, call.err = l.provider.FindNearbyStores(ctx, lat, lng)
	if call.stores == nil {
		call.stores = []models.Store{}
	}
	close(call.done)

	l.mu.Lock()
	delete(l.inflight, key)
	l.mu.Unlock()

	if call.err == nil {
		// Detached from the caller's context: the provider has already answered,
		// so a cancellation now should not throw the result away.
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		l.cache.Set(writeCtx, key, call.stores, l.ttl)
	}
	return call.stores, false, call.err
}

// WithinRadius narrows a store list to a requested radius.
//
// The provider is always queried at ProviderRadiusMeters so one cache entry can
// serve every radius; anything wider than that is the provider's limit, not a
// filter this function can lift.
func WithinRadius(stores []models.Store, radiusMeters int) []models.Store {
	if radiusMeters <= 0 || radiusMeters >= ProviderRadiusMeters {
		return stores
	}
	limitKm := float64(radiusMeters) / 1000
	out := make([]models.Store, 0, len(stores))
	for _, s := range stores {
		if parseKm(s.Distance) <= limitKm {
			out = append(out, s)
		}
	}
	return out
}
