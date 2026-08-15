package services

import (
	"fmt"
	"time"

	"github.com/patrickmn/go-cache"
)

// CacheService memoises whole pipeline results.
//
// It was already constructed and injected into the handler before this change,
// and then never called — so the README advertised caching that did not exist
// and every repeat lookup re-billed the YouTube and OpenAI calls.
type CacheService struct {
	cache *cache.Cache
	hits  uint64
	miss  uint64
}

func NewCacheService() *CacheService {
	return &CacheService{cache: cache.New(15*time.Minute, 20*time.Minute)}
}

func (cs *CacheService) Get(key string) (interface{}, bool) {
	v, found := cs.cache.Get(key)
	if found {
		cs.hits++
	} else {
		cs.miss++
	}
	return v, found
}

func (cs *CacheService) Set(key string, value interface{}) {
	cs.cache.Set(key, value, cache.DefaultExpiration)
}

// Stats reports hit/miss counts for the health endpoint.
func (cs *CacheService) Stats() (hits, misses uint64, items int) {
	return cs.hits, cs.miss, cs.cache.ItemCount()
}

// ResultKey builds the cache key from the video and the search location.
//
// Coordinates are rounded to two decimal places (~1.1km) so that clients whose
// geo-IP lookups differ in the fourth decimal still share an entry, instead of
// each minting a private one and making the cache useless.
func ResultKey(videoID string, lat, lng float64) string {
	return fmt.Sprintf("v1:%s:%.2f:%.2f", videoID, lat, lng)
}
