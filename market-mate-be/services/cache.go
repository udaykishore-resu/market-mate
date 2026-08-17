package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/redis/go-redis/v9"
)

// Cache is the TTL cache in front of the store search.
//
// Two implementations, chosen at startup exactly like the video and store
// providers: Redis when MM_REDIS_ADDR is set, the original in-process go-cache
// otherwise. The interface is the seam that keeps `go run ./cmd` working with
// no infrastructure.
//
// Values cross the interface as JSON. Redis has no choice, and holding the
// in-memory copy in the same form means a caller cannot accidentally depend on
// getting the same pointer back — which would work locally and then silently
// change behaviour the day Redis is switched on.
type Cache interface {
	// Get decodes the entry into dest and reports whether it was present. A
	// cache failure is a miss, never an error: no lookup here is worth failing
	// a request over.
	Get(ctx context.Context, key string, dest any) bool
	Set(ctx context.Context, key string, value any, ttl time.Duration)
	Stats() CacheStats
	// Ping reports backend reachability for the health endpoint.
	Ping(ctx context.Context) error
	Kind() string
	Close() error
}

// Kind values name the cache implementation that resolved.
const (
	KindRedis  = "redis"
	KindMemory = "memory"
)

// CacheStats is what the health endpoint reports. Items is -1 when the backend
// cannot answer cheaply — Redis is shared, so counting keys would be both slow
// and wrong to attribute to this process.
type CacheStats struct {
	Hits   uint64 `json:"hits"`
	Misses uint64 `json:"misses"`
	Items  int    `json:"items"`
	Kind   string `json:"kind"`
}

// DefaultStoreCacheTTL matches MM_STORE_CACHE_TTL's default. Store opening
// hours and closures change, so this layer keeps an expiry even though the
// transcript and extraction layers below it are permanent.
const DefaultStoreCacheTTL = 15 * time.Minute

// NewCacheService returns the in-memory cache. The name is retained because it
// is what main and the handler tests already call.
func NewCacheService() Cache { return NewMemoryCache(DefaultStoreCacheTTL) }

// --- memory -------------------------------------------------------------------

type memoryCache struct {
	cache  *cache.Cache
	hits   atomic.Uint64
	misses atomic.Uint64
}

func NewMemoryCache(ttl time.Duration) Cache {
	if ttl <= 0 {
		ttl = DefaultStoreCacheTTL
	}
	return &memoryCache{cache: cache.New(ttl, ttl+5*time.Minute)}
}

func (m *memoryCache) Get(_ context.Context, key string, dest any) bool {
	v, found := m.cache.Get(key)
	if !found {
		m.misses.Add(1)
		return false
	}
	raw, ok := v.([]byte)
	if !ok {
		m.misses.Add(1)
		return false
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		m.misses.Add(1)
		return false
	}
	m.hits.Add(1)
	return true
}

func (m *memoryCache) Set(_ context.Context, key string, value any, ttl time.Duration) {
	raw, err := json.Marshal(value)
	if err != nil {
		log.Printf("cache: encoding %s: %v", key, err)
		return
	}
	if ttl <= 0 {
		ttl = cache.DefaultExpiration
	}
	m.cache.Set(key, raw, ttl)
}

func (m *memoryCache) Stats() CacheStats {
	return CacheStats{
		Hits:   m.hits.Load(),
		Misses: m.misses.Load(),
		Items:  m.cache.ItemCount(),
		Kind:   m.Kind(),
	}
}

func (m *memoryCache) Ping(context.Context) error { return nil }
func (m *memoryCache) Kind() string               { return KindMemory }
func (m *memoryCache) Close() error               { return nil }

// --- redis --------------------------------------------------------------------

type redisCache struct {
	client *redis.Client
	hits   atomic.Uint64
	misses atomic.Uint64
}

// RedisOptions is the subset of connection settings the contract exposes.
type RedisOptions struct {
	Addr     string
	Password string
	DB       int
}

// NewRedisCache dials Redis and verifies the connection before returning, so a
// misconfigured address is a boot-time log line and a fallback rather than a
// per-request surprise.
func NewRedisCache(ctx context.Context, opts RedisOptions) (Cache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         opts.Addr,
		Password:     opts.Password,
		DB:           opts.DB,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis at %s: %w", opts.Addr, err)
	}
	return &redisCache{client: client}, nil
}

func (r *redisCache) Get(ctx context.Context, key string, dest any) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	raw, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		r.misses.Add(1)
		// redis.Nil is an ordinary miss; anything else is worth a line, because
		// a permanently unreachable cache looks identical to a cold one.
		if err != redis.Nil {
			log.Printf("cache: redis get %s: %v", key, err)
		}
		return false
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		r.misses.Add(1)
		return false
	}
	r.hits.Add(1)
	return true
}

func (r *redisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) {
	raw, err := json.Marshal(value)
	if err != nil {
		log.Printf("cache: encoding %s: %v", key, err)
		return
	}
	if ttl <= 0 {
		ttl = DefaultStoreCacheTTL
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := r.client.Set(ctx, key, raw, ttl).Err(); err != nil {
		log.Printf("cache: redis set %s: %v", key, err)
	}
}

func (r *redisCache) Stats() CacheStats {
	return CacheStats{Hits: r.hits.Load(), Misses: r.misses.Load(), Items: -1, Kind: r.Kind()}
}

func (r *redisCache) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return r.client.Ping(ctx).Err()
}

func (r *redisCache) Kind() string { return KindRedis }
func (r *redisCache) Close() error { return r.client.Close() }

// --- keys ---------------------------------------------------------------------

// cacheNamespace versions every key this service writes. Redis is shared with
// Guest Score (contract: logical DB 1 here, 0 there) and survives deploys, so
// keys carry both the application and a schema version — bumping v1 retires
// every entry whose shape changed, without a flush.
const cacheNamespace = "mm:v1"

// ResultKey identifies a composed pipeline response: one video, seen from one
// geohash cell.
//
// It replaces the "%.2f" coordinate rounding this function used to do; see
// Geohash for why.
func ResultKey(videoID string, lat, lng float64) string {
	return fmt.Sprintf("%s:result:%s:%s", cacheNamespace, videoID, Geohash(lat, lng, GeohashPrecision))
}

// StoreKey identifies a store search. The video ID is part of the key because
// the contract asks for it, and because a future per-recipe store ranking would
// otherwise silently serve another recipe's ordering from this entry.
//
// The requested radius is deliberately absent: the provider is queried once at
// its own radius and the result is narrowed afterwards, so a 1km and a 5km
// request share the entry instead of each paying for a Places call.
func StoreKey(videoID string, lat, lng float64) string {
	return fmt.Sprintf("%s:stores:%s:%s", cacheNamespace, videoID,
		Geohash(lat, lng, GeohashPrecision))
}
