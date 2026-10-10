package gocache

import (
	"testing"
	"time"
)

func TestCacheCollisionBucketUsesExactKey(t *testing.T) {
	bucket := []*Item{
		{key: "first", value: "one"},
		{key: "second", value: "two"},
	}

	if index := findItem(bucket, "second"); index != 1 {
		t.Fatalf("findItem(second) = %d, want 1", index)
	}
	if index := findItem(bucket, "missing"); index != -1 {
		t.Fatalf("findItem(missing) = %d, want -1", index)
	}
}

func TestCacheReplacementKeepsSingleEntry(t *testing.T) {
	cache := New(NoExpiration)
	defer cache.Close()

	cache.Set("key", "old", DefaultExpiration)
	cache.Set("key", "new", DefaultExpiration)

	value, found := cache.Get("key")
	if !found || value != "new" {
		t.Fatalf("Get(key) = %#v, %v, want new, true", value, found)
	}

	stats := cache.Stats()
	if stats.Items != 1 {
		t.Fatalf("Stats().Items = %d, want 1", stats.Items)
	}
}

func TestCacheCapacity(t *testing.T) {
	cache := NewWithOptions(Options{
		TTL:        NoExpiration,
		MaxEntries: 1,
	})
	defer cache.Close()

	if !cache.TrySet("first", "one", DefaultExpiration) {
		t.Fatal("TrySet(first) rejected the first entry")
	}
	if cache.TrySet("second", "two", DefaultExpiration) {
		t.Fatal("TrySet(second) accepted an entry over MaxEntries")
	}

	stats := cache.Stats()
	if stats.Items != 1 {
		t.Fatalf("Stats().Items = %d, want 1", stats.Items)
	}
}

func TestCacheMaxBytes(t *testing.T) {
	cache := NewWithOptions(Options{
		TTL:      NoExpiration,
		MaxBytes: 5,
		SizeFunc: func(key string, value interface{}) int64 {
			return int64(len(value.(string)))
		},
	})
	defer cache.Close()

	if !cache.TrySet("first", "12345", DefaultExpiration) {
		t.Fatal("TrySet(first) rejected the entry within MaxBytes")
	}
	if cache.TrySet("second", "6", DefaultExpiration) {
		t.Fatal("TrySet(second) accepted an entry over MaxBytes")
	}

	stats := cache.Stats()
	if stats.Items != 1 || stats.EstimatedBytes != 5 {
		t.Fatalf("unexpected byte-limit stats: %+v", stats)
	}
}

func TestCacheStats(t *testing.T) {
	cache := New(NoExpiration)
	defer cache.Close()

	cache.Set("key", "value", DefaultExpiration)
	if _, found := cache.Get("key"); !found {
		t.Fatal("expected cache hit")
	}
	if _, found := cache.Get("missing"); found {
		t.Fatal("expected cache miss")
	}

	stats := cache.Stats()
	if stats.Hits != 1 || stats.Misses != 1 {
		t.Fatalf("unexpected hit/miss stats: %+v", stats)
	}
	if stats.Items != 1 || stats.EstimatedBytes <= 0 {
		t.Fatalf("unexpected size stats: %+v", stats)
	}
}

func TestCacheExpiredRingDoesNotDeleteReplacement(t *testing.T) {
	cache := New(NoExpiration)
	defer cache.Close()

	cache.Set("key", "old", time.Nanosecond)
	cache.Set("key", "new", NoExpiration)

	cache.cleanupExpired(time.Now().Add(time.Hour).UnixNano())

	value, found := cache.Get("key")
	if !found || value != "new" {
		t.Fatalf("Get(key) after cleanup = %#v, %v, want new, true", value, found)
	}
}

func TestCacheCloseIsIdempotent(t *testing.T) {
	cache := New(10 * time.Millisecond)
	cache.Close()
	cache.Close()
}

func TestCacheClearResetsEntriesAndBytes(t *testing.T) {
	cache := New(NoExpiration)
	defer cache.Close()

	cache.Set("key", "value", DefaultExpiration)
	cache.Clear()

	if _, found := cache.Get("key"); found {
		t.Fatal("expected key to be removed by Clear")
	}
	stats := cache.Stats()
	if stats.Items != 0 || stats.EstimatedBytes != 0 {
		t.Fatalf("unexpected stats after Clear: %+v", stats)
	}
}
