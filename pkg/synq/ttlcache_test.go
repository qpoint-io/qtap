package synq

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTTLCache(t *testing.T) {
	// Create a mock time that we can control
	mockTime := time.Now()
	originalNow := now
	now = func() time.Time { return mockTime }
	defer func() { now = originalNow }()

	// Create container with 100ms expiration and 50ms cleanup
	container := NewTTLCache[string, int](100*time.Millisecond, 50*time.Millisecond)
	defer container.Stop()

	// Test Store and Load
	t.Run("Store and Load", func(t *testing.T) {
		container.Store("key1", 123)

		if val, ok := container.Load("key1"); !ok || val != 123 {
			t.Errorf("Expected to load 123, got %v, exists: %v", val, ok)
		}
	})

	// Test Delete
	t.Run("Delete", func(t *testing.T) {
		container.Store("key2", 456)
		container.Delete("key2")

		if _, ok := container.Load("key2"); ok {
			t.Error("Expected key2 to be deleted")
		}
	})

	// Test Expiration
	t.Run("Expiration", func(t *testing.T) {
		container.Store("key3", 789)

		// Advance time beyond expiration
		mockTime = mockTime.Add(150 * time.Millisecond)
		container.GCExpired()

		if _, ok := container.Load("key3"); ok {
			t.Error("Expected key3 to be expired")
		}

		// Test ExpireRecord
		container.Store("key4", 1337)
		_, ok := container.Load("key4")
		require.True(t, ok)
		container.ExpireRecord("key4")
		// it should remain in the cache until the next GC cycle
		_, ok = container.Load("key4")
		require.True(t, ok)

		mockTime = mockTime.Add(150 * time.Millisecond)
		container.GCExpired()
		_, ok = container.Load("key4")
		require.False(t, ok)
	})

	// Test Renew
	t.Run("Renew", func(t *testing.T) {
		container.Store("key4", 999)

		// Advance half the expiration time
		mockTime = mockTime.Add(50 * time.Millisecond)

		// Renew the key
		container.Renew("key4")

		// Advance another half of expiration time
		mockTime = mockTime.Add(50 * time.Millisecond)

		if val, ok := container.Load("key4"); !ok || val != 999 {
			t.Errorf("Expected renewed key to still exist with value 999, got %v, exists: %v", val, ok)
		}
	})

	// Test LoadAndRenew
	t.Run("LoadAndRenew", func(t *testing.T) {
		// Test loading non-existent key
		if _, ok := container.LoadAndRenew("nonexistent"); ok {
			t.Error("Expected LoadAndRenew to return false for non-existent key")
		}

		// Store a key
		container.Store("key4b", 888)

		// Advance half the expiration time
		mockTime = mockTime.Add(50 * time.Millisecond)

		// Load and renew the key
		if val, ok := container.LoadAndRenew("key4b"); !ok || val != 888 {
			t.Errorf("Expected LoadAndRenew to return 888 and true, got %v, exists: %v", val, ok)
		}

		// Advance another 75ms (total 125ms, would have expired without renewal)
		mockTime = mockTime.Add(75 * time.Millisecond)
		container.GCExpired()

		// Key should still exist because it was renewed
		if val, ok := container.Load("key4b"); !ok || val != 888 {
			t.Errorf("Expected renewed key to still exist with value 888, got %v, exists: %v", val, ok)
		}
	})

	// Test Length
	t.Run("Length", func(t *testing.T) {
		// Advance beyond expiration time to clear previous entries
		mockTime = mockTime.Add(150 * time.Millisecond)
		container.GCExpired()
		container.Store("key5", 555)
		container.Store("key6", 666)

		if length := container.Len(); length != 2 {
			t.Errorf("Expected length 2, got %d", length)
		}
	})

	// Test Copy
	t.Run("Copy", func(t *testing.T) {
		container.Store("key7", 777)
		copied := container.Copy()

		if len(copied) != container.Len() {
			t.Errorf("Expected copied map to have same length as container")
		}

		if val, ok := copied["key7"]; !ok || val != 777 {
			t.Errorf("Expected copied map to contain key7 with value 777")
		}
	})
}
