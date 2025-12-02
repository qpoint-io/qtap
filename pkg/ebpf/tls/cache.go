package tls

import (
	"time"

	"github.com/qpoint-io/qtap/pkg/synq"
)

// ResultCache holds scan results for a single binary, keyed by scanner name.
// This allows multiple scanners to cache their results for the same binary.
type ResultCache = synq.Map[string, ScanResult]

// ScanCache is an in-memory cache for scan results, keyed by binary hash.
// It provides TTL-based expiration with refresh on access.
type ScanCache struct {
	cache *synq.TTLCache[string, *ResultCache]
}

// NewScanCache creates a new ScanCache with the specified TTL and cleanup interval.
func NewScanCache(expireDuration, cleanupInterval time.Duration) *ScanCache {
	return &ScanCache{
		cache: synq.NewTTLCache[string, *ResultCache](expireDuration, cleanupInterval),
	}
}

// Stop stops the cleanup goroutine
func (c *ScanCache) Stop() {
	c.cache.Stop()
}

// Get returns the cached results for a binary hash, renewing the TTL on access.
// Returns nil if not found.
func (c *ScanCache) Get(binaryHash string) *ResultCache {
	entry, _ := c.cache.LoadAndRenew(binaryHash)
	return entry
}

// GetOrCreate returns the cached results for a binary hash, creating a new entry if not found.
// This is useful for concurrent access where multiple scanners may try to create the entry.
func (c *ScanCache) GetOrCreate(binaryHash string) *ResultCache {
	// Try to load and renew first
	if entry, ok := c.cache.LoadAndRenew(binaryHash); ok {
		return entry
	}

	// Create new entry and store it
	entry := synq.NewMap[string, ScanResult]()
	c.cache.Store(binaryHash, entry)
	return entry
}

// GetScanResult returns a specific scanner's result for a binary hash.
// This is a convenience method that combines Get and CachedResults.Get.
func (c *ScanCache) GetScanResult(binaryHash, scannerName string) (ScanResult, bool) {
	entry := c.Get(binaryHash)
	if entry == nil {
		return nil, false
	}
	return entry.Load(scannerName)
}

// SetScanResult stores a scan result for a specific scanner and binary hash.
// This is a convenience method that combines GetOrCreate and CachedResults.Set.
func (c *ScanCache) SetScanResult(binaryHash, scannerName string, result ScanResult) {
	entry := c.GetOrCreate(binaryHash)
	entry.Store(scannerName, result)
}

// Delete removes all cached results for a binary hash
func (c *ScanCache) Delete(binaryHash string) {
	c.cache.Delete(binaryHash)
}

// Len returns the number of cached binaries
func (c *ScanCache) Len() int {
	return c.cache.Len()
}
