package synq

import (
	"time"
)

var now = time.Now

type TTLCache[K comparable, V any] struct {
	// container entries
	entries *Map[K, V]

	// expiration entries
	expirations *Map[K, time.Time]

	// expiration watcher
	ticker *time.Ticker

	// stop channel
	stop chan bool

	// expiration duration
	expireDuration time.Duration
}

func NewTTLCache[K comparable, V any](expireDuration, cleanupDuration time.Duration) *TTLCache[K, V] {
	// init the container
	container := &TTLCache[K, V]{
		entries:        NewMap[K, V](),
		expirations:    NewMap[K, time.Time](),
		ticker:         time.NewTicker(cleanupDuration),
		stop:           make(chan bool),
		expireDuration: expireDuration,
	}

	// start the container
	container.Start()

	// return the container
	return container
}

func (c *TTLCache[K, V]) Start() {
	go func() {
		for {
			select {
			case <-c.ticker.C:
				c.GCExpired()
			case <-c.stop:
				return
			}
		}
	}()
}

func (c *TTLCache[K, V]) Stop() {
	// stop the ticker
	c.ticker.Stop()

	// stop the goroutine
	c.stop <- true
}

func (c *TTLCache[K, V]) GCExpired() {
	// check for any records that have expired and delete
	for key, exp := range c.expirations.Copy() {
		if now().After(exp) {
			c.entries.Delete(key)
			c.expirations.Delete(key)
		}
	}
}

// ExpireRecord forcefully expires a record by key. It will be cleaned up by the next GC cycle.
func (c *TTLCache[K, V]) ExpireRecord(key K) {
	c.expirations.Store(key, now())
}

func (c *TTLCache[K, V]) Load(key K) (value V, ok bool) {
	return c.entries.Load(key)
}

func (c *TTLCache[K, V]) Store(key K, value V) {
	// add entry to the store
	c.entries.Store(key, value)

	// add an entry to the expirations entry
	c.expirations.Store(key, now().Add(c.expireDuration))
}

func (c *TTLCache[K, V]) Delete(key K) {
	// remove the entry from the store
	c.entries.Delete(key)

	// remove the entry from the expirations
	c.expirations.Delete(key)
}

func (c *TTLCache[K, V]) Renew(key K) {
	c.expirations.Store(key, now().Add(c.expireDuration))
}

// LoadAndRenew loads a value and renews its TTL if found.
// Returns the value and whether it was found.
func (c *TTLCache[K, V]) LoadAndRenew(key K) (value V, ok bool) {
	value, ok = c.entries.Load(key)
	if ok {
		c.expirations.Store(key, now().Add(c.expireDuration))
	}
	return value, ok
}

func (c *TTLCache[K, V]) Len() int {
	return c.entries.Len()
}

func (c *TTLCache[K, V]) Copy() map[K]V {
	return c.entries.Copy()
}
