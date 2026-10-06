package llm

import (
	"sync"
	"time"
)

// maxCacheEntries bounds MemoryCache so a long-running service cannot grow it
// without limit; when full, new responses are simply not cached.
const maxCacheEntries = 512

// Cache holds responses by prompt hash.
type Cache interface {
	Get(key string) (Response, bool)
	Set(key string, resp Response, ttl time.Duration)
}

// MemoryCache is an in-process Cache. It is safe for concurrent use.
type MemoryCache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]cacheEntry
}

type cacheEntry struct {
	resp    Response
	expires time.Time
}

// NewMemoryCache returns an empty cache. A nil now means time.Now.
func NewMemoryCache(now func() time.Time) *MemoryCache {
	if now == nil {
		now = time.Now
	}
	return &MemoryCache{now: now, entries: map[string]cacheEntry{}}
}

// Get returns the response stored under key if it has not expired.
func (c *MemoryCache) Get(key string) (Response, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || !c.now().Before(e.expires) {
		delete(c.entries, key)
		return Response{}, false
	}
	return e.resp, true
}

// Set stores resp under key for ttl.
func (c *MemoryCache) Set(key string, resp Response, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if len(c.entries) >= maxCacheEntries {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
	}
	if _, replacing := c.entries[key]; !replacing && len(c.entries) >= maxCacheEntries {
		return
	}
	c.entries[key] = cacheEntry{resp: resp, expires: now.Add(ttl)}
}
