package services

import (
	"cmp"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactoryRegistryReplacePublishesCompleteTopology(t *testing.T) {
	oldFactory := newMockFactory("eventstore.old", "eventstore")
	newFactory := newMockFactory("eventstore.new", "eventstore")
	oldKeys := []ServiceKey{{Type: "eventstore"}, {Type: "eventstore", ID: "old"}}
	newKeys := []ServiceKey{{Type: "eventstore"}, {Type: "eventstore", ID: "new"}}
	registry := NewFactoryRegistry(oldFactory)
	registry.Register(oldFactory, "old")
	require.ElementsMatch(t, oldKeys, registry.AvailableFactoriesForType("eventstore"))
	require.Same(t, oldFactory, registry.Get(oldKeys[0]))

	const iterations = 1000
	start := make(chan struct{})
	errs := make(chan []ServiceKey, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for range iterations {
			registry.Replace(map[ServiceKey]Factory{
				newKeys[0]: newFactory,
				newKeys[1]: newFactory,
			})
			registry.Replace(map[ServiceKey]Factory{
				oldKeys[0]: oldFactory,
				oldKeys[1]: oldFactory,
			})
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range iterations * 2 {
			keys := registry.AvailableFactoriesForType("eventstore")
			slices.SortFunc(keys, func(a, b ServiceKey) int { return cmp.Compare(a.String(), b.String()) })
			if !slices.Equal(keys, oldKeys) && !slices.Equal(keys, newKeys) {
				errs <- keys
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	close(errs)
	for keys := range errs {
		t.Fatalf("reader observed partial topology: %v", keys)
	}

	keys := registry.AvailableFactoriesForType("eventstore")
	require.Len(t, keys, 2)
	keys[0] = ServiceKey{Type: "corrupted"}
	assert.NotContains(t, registry.AvailableFactoriesForType("eventstore"), ServiceKey{Type: "corrupted"})
}
