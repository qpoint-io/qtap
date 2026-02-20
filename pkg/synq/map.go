package synq

import (
	"maps"
	"sync"
)

// Map is a generic map that is safe for concurrent use.
// It uses sync.RWMutex for managing concurrent access.
type Map[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

// NewMap creates a new ConcurrentMap.
func NewMap[K comparable, V any]() *Map[K, V] {
	return NewMapSize[K, V](0)
}

// NewMapSize creates a new ConcurrentMap with a given size.
func NewMapSize[K comparable, V any](size int) *Map[K, V] {
	return &Map[K, V]{
		m: make(map[K]V, size),
	}
}

// Load returns the value stored in the map for a key, or nil if no value is present.
// The ok result indicates whether value was found in the map.
func (cm *Map[K, V]) Load(key K) (value V, ok bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	value, ok = cm.m[key]
	return
}

// Store sets the value for a key.
func (cm *Map[K, V]) Store(key K, value V) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.m[key] = value
}

// LoadOrInsert returns the value stored in the map for a key, or inserts a new value if the key is not present.
func (cm *Map[K, V]) LoadOrInsert(key K, value V) (actual V, loaded bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if v, ok := cm.m[key]; ok {
		return v, true
	}

	cm.m[key] = value
	return value, false
}

// Delete removes the key from the map.
func (cm *Map[K, V]) Delete(key K) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.m, key)
}

func (cm *Map[K, V]) Len() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.m)
}

// Clone returns a copy of the map's contents as a new *synq.Map.
// Use with caution, as this function copies the entire map.
func (cm *Map[K, V]) Clone() *Map[K, V] {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	dest := NewMapSize[K, V](len(cm.m))
	for k, v := range cm.m {
		dest.Store(k, v)
	}

	return dest
}

// Copy returns a copy of the map's contents as a native map[K]V.
// Use with caution, as this function copies the entire map.
func (cm *Map[K, V]) Copy() map[K]V {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	dest := make(map[K]V, len(cm.m))
	maps.Copy(dest, cm.m)

	return dest
}

func (cm *Map[K, V]) Reset() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.m = make(map[K]V)
}

// Iter iterates over the key-value pairs in the ConcurrentMap, applying the provided function f to each pair.
// It uses a two-phase approach to balance concurrency safety and memory efficiency:
//
// 1. Keys Collection Phase:
//   - Acquires a read lock on the entire map.
//   - Creates a slice of keys, pre-allocated to the map's current size to minimize allocations.
//   - Collects all keys from the map into this slice.
//   - Releases the read lock.
//
// 2. Iteration Phase:
//   - Iterates over the collected keys.
//   - For each key, acquires a read lock, retrieves the value, and releases the lock.
//   - Applies the function f to each key-value pair.
//
// Trade-offs and Design Considerations:
// - Concurrency: Allows other goroutines to modify the map between the key collection and iteration phases.
// - Consistency: Provides a "snapshot" view of keys at collection time, but values are read in real-time.
// - Memory Efficiency:
//   - Only allocates a single slice for keys, avoiding per-item allocations.
//   - Trades increased memory usage (O(n) for n keys) for reduced lock contention.
//
// - Performance:
//   - Minimizes the duration of holding the read lock on the entire map.
//   - May miss new keys added after the collection phase or process deleted keys.
//
// - Flexibility: The provided function f can safely modify the map without causing deadlocks.
//
// Note: This function provides a "fuzzy" view of the map, as it may miss new keys added after the collection phase or process deleted keys.
func (cm *Map[K, V]) Iter(f func(key K, value V) bool) {
	cm.mu.RLock()
	keys := make([]K, 0, len(cm.m))
	for k := range cm.m {
		keys = append(keys, k)
	}
	cm.mu.RUnlock()

	for _, k := range keys {
		cm.mu.RLock()
		v, ok := cm.m[k]
		cm.mu.RUnlock()
		if !ok {
			continue // Key was deleted
		}
		if !f(k, v) {
			break
		}
	}
}
