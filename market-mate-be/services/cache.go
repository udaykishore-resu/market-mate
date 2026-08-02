package services

import (
	"time"

	"github.com/patrickmn/go-cache"
)

type CacheService struct {
	cache *cache.Cache
}

func NewCacheService() *CacheService {
	// Create a cache with 5 minute default expiration and 10 minute cleanup interval
	c := cache.New(5*time.Minute, 10*time.Minute)
	return &CacheService{cache: c}
}

func (cs *CacheService) Get(key string) (interface{}, bool) {
	return cs.cache.Get(key)
}

func (cs *CacheService) Set(key string, value interface{}) {
	cs.cache.Set(key, value, cache.DefaultExpiration)
}
