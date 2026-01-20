package lcsc

import (
	"testing"
	"time"
)

// TestMemoryCacheSet tests basic cache set operation.
func TestMemoryCacheSet(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	defer cache.Close()
	defer cache.Clear()

	key := "test:key"
	value := []byte("test value")

	cache.Set(key, value, 1*time.Minute)

	if cache.Size() != 1 {
		t.Errorf("expected cache size 1, got %d", cache.Size())
	}
}

// TestMemoryCacheGet tests basic cache get operation.
func TestMemoryCacheGet(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	defer cache.Close()
	defer cache.Clear()

	key := "test:key"
	value := []byte("test value")

	cache.Set(key, value, 1*time.Minute)

	retrieved, ok := cache.Get(key)
	if !ok {
		t.Fatal("expected to find value in cache")
	}

	if string(retrieved) != string(value) {
		t.Errorf("expected value %s, got %s", value, retrieved)
	}
}

// TestMemoryCacheGetMissing tests cache get for missing key.
func TestMemoryCacheGetMissing(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	defer cache.Close()

	_, ok := cache.Get("nonexistent")
	if ok {
		t.Fatal("expected cache miss for nonexistent key")
	}
}

// TestMemoryCacheDelete tests cache delete operation.
func TestMemoryCacheDelete(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	defer cache.Close()
	defer cache.Clear()

	key := "test:key"
	cache.Set(key, []byte("value"), 1*time.Minute)

	if cache.Size() != 1 {
		t.Errorf("expected cache size 1 after set")
	}

	cache.Delete(key)

	if cache.Size() != 0 {
		t.Errorf("expected cache size 0 after delete")
	}

	_, ok := cache.Get(key)
	if ok {
		t.Fatal("expected cache miss after delete")
	}
}

// TestMemoryCacheTTL tests that expired entries are not returned.
func TestMemoryCacheTTL(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	defer cache.Close()
	defer cache.Clear()

	key := "test:key"
	cache.Set(key, []byte("value"), 100*time.Millisecond)

	// Should be available immediately
	_, ok := cache.Get(key)
	if !ok {
		t.Fatal("expected value in cache immediately after set")
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	_, ok = cache.Get(key)
	if ok {
		t.Fatal("expected cache miss after TTL expiration")
	}
}

// TestMemoryCacheDefaultTTL tests that default TTL is used when zero is passed.
func TestMemoryCacheDefaultTTL(t *testing.T) {
	cache := NewMemoryCache(100 * time.Millisecond)
	defer cache.Close()
	defer cache.Clear()

	key := "test:key"
	// Pass 0 as TTL to use default
	cache.Set(key, []byte("value"), 0)

	// Should be available immediately
	_, ok := cache.Get(key)
	if !ok {
		t.Fatal("expected value in cache immediately after set")
	}

	// Wait for default TTL expiration
	time.Sleep(150 * time.Millisecond)

	_, ok = cache.Get(key)
	if ok {
		t.Fatal("expected cache miss after default TTL expiration")
	}
}

// TestMemoryCacheMultipleEntries tests cache with multiple entries.
func TestMemoryCacheMultipleEntries(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	defer cache.Close()
	defer cache.Clear()

	entries := map[string][]byte{
		"key1": []byte("value1"),
		"key2": []byte("value2"),
		"key3": []byte("value3"),
	}

	for key, value := range entries {
		cache.Set(key, value, 1*time.Minute)
	}

	if cache.Size() != 3 {
		t.Errorf("expected cache size 3, got %d", cache.Size())
	}

	for key, expectedValue := range entries {
		value, ok := cache.Get(key)
		if !ok {
			t.Errorf("expected to find key %s in cache", key)
			continue
		}
		if string(value) != string(expectedValue) {
			t.Errorf("expected value %s for key %s, got %s", expectedValue, key, value)
		}
	}
}

// TestMemoryCacheClear tests clearing all cache entries.
func TestMemoryCacheClear(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	defer cache.Close()

	// Add multiple entries
	for i := 0; i < 5; i++ {
		cache.Set("key"+string(rune(i)), []byte("value"), 1*time.Minute)
	}

	if cache.Size() == 0 {
		t.Fatal("expected cache to have entries")
	}

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("expected cache size 0 after clear, got %d", cache.Size())
	}
}

// TestMemoryCacheSize tests the Size method.
func TestMemoryCacheSize(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	defer cache.Close()
	defer cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("expected initial cache size 0, got %d", cache.Size())
	}

	for i := 0; i < 10; i++ {
		cache.Set("key"+string(rune(i)), []byte("value"), 1*time.Minute)
		expected := i + 1
		if cache.Size() != expected {
			t.Errorf("expected cache size %d, got %d", expected, cache.Size())
		}
	}
}

// TestMemoryCacheOverwrite tests overwriting existing cache entries.
func TestMemoryCacheOverwrite(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	defer cache.Close()
	defer cache.Clear()

	key := "test:key"

	cache.Set(key, []byte("value1"), 1*time.Minute)
	if cache.Size() != 1 {
		t.Fatal("expected cache size 1 after first set")
	}

	// Overwrite with new value
	cache.Set(key, []byte("value2"), 1*time.Minute)
	if cache.Size() != 1 {
		t.Fatal("expected cache size 1 after overwrite (should not duplicate)")
	}

	value, ok := cache.Get(key)
	if !ok {
		t.Fatal("expected to find value in cache")
	}

	if string(value) != "value2" {
		t.Errorf("expected new value value2, got %s", value)
	}
}

// TestMemoryCacheEmptyValue tests storing empty values.
func TestMemoryCacheEmptyValue(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	defer cache.Close()
	defer cache.Clear()

	key := "test:key"
	cache.Set(key, []byte(""), 1*time.Minute)

	value, ok := cache.Get(key)
	if !ok {
		t.Fatal("expected to find empty value in cache")
	}

	if len(value) != 0 {
		t.Errorf("expected empty value, got %v", value)
	}
}

// TestCacheInterface tests that MemoryCache implements Cache interface.
func TestCacheInterface(t *testing.T) {
	var _ Cache = (*MemoryCache)(nil)
}

// TestCacheCleanup tests that expired entries are cleaned up.
func TestCacheCleanup(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	defer cache.Close()
	defer cache.Clear()

	// Add entries with very short TTL
	for i := 0; i < 10; i++ {
		cache.Set("key"+string(rune(i)), []byte("value"), 100*time.Millisecond)
	}

	if cache.Size() != 10 {
		t.Errorf("expected cache size 10, got %d", cache.Size())
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Manually trigger cleanup (since the background cleanup runs every minute)
	cache.cleanup()

	if cache.Size() != 0 {
		t.Errorf("expected cache size 0 after cleanup, got %d", cache.Size())
	}
}

// TestDefaultCacheConfig tests the default cache configuration.
func TestDefaultCacheConfig(t *testing.T) {
	config := DefaultCacheConfig()

	if !config.Enabled {
		t.Error("expected cache to be enabled by default")
	}

	if config.SearchTTL != 5*time.Minute {
		t.Errorf("expected search TTL 5m, got %v", config.SearchTTL)
	}

	if config.DetailsTTL != 10*time.Minute {
		t.Errorf("expected details TTL 10m, got %v", config.DetailsTTL)
	}
}
