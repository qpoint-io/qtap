package devtools

import (
	lru "github.com/hashicorp/golang-lru/v2"
)

const (
	// DefaultCacheMaxSize is the default maximum number of connections to cache
	DefaultCacheMaxSize = 10000
)

// ConnectionCache stores connection data for cross-entity filtering lookups.
// It allows request/issue/pii events to be filtered by connection or process attributes.
type ConnectionCache struct {
	cache *lru.Cache[string, map[string]any]
}

// NewConnectionCache creates a new connection cache with the specified max size
func NewConnectionCache(maxSize int) *ConnectionCache {
	if maxSize <= 0 {
		maxSize = DefaultCacheMaxSize
	}
	cache, err := lru.New[string, map[string]any](maxSize)
	if err != nil {
		panic(err)
	}
	return &ConnectionCache{
		cache: cache,
	}
}

// Set adds or updates a connection in the cache
func (c *ConnectionCache) Set(connId string, data map[string]any) {
	if connId == "" {
		return
	}
	c.cache.Add(connId, data)
}

// Get retrieves connection data from the cache
func (c *ConnectionCache) Get(connId string) map[string]any {
	if connId == "" {
		return nil
	}
	if data, ok := c.cache.Get(connId); ok {
		return data
	}
	return nil
}

// Delete removes a connection from the cache
func (c *ConnectionCache) Delete(connId string) {
	if connId == "" {
		return
	}
	c.cache.Remove(connId)
}

// Len returns the current number of cached connections
func (c *ConnectionCache) Len() int {
	return c.cache.Len()
}
