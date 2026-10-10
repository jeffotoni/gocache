package gocache

import (
	"testing"
	"time"
)

type typedTestValue struct {
	Code string
}

func TestTypedCacheSetGet(t *testing.T) {
	cache := NewTyped[typedTestValue](NoExpiration)
	defer cache.Close()

	want := typedTestValue{Code: "PT"}
	cache.Set("country", want, DefaultExpiration)

	got, found := cache.Get("country")
	if !found || got != want {
		t.Fatalf("Get(country) = %#v, %v; want %#v, true", got, found, want)
	}
}

func TestTypedCacheMissReturnsZeroValue(t *testing.T) {
	cache := NewTyped[typedTestValue](NoExpiration)
	defer cache.Close()

	got, found := cache.Get("missing")
	if found {
		t.Fatal("Get(missing) found an unexpected value")
	}
	if got != (typedTestValue{}) {
		t.Fatalf("Get(missing) = %#v; want zero value", got)
	}
}

func TestTypedCacheTrySetAndClear(t *testing.T) {
	cache := NewTypedWithOptions[typedTestValue](Options{
		TTL:        NoExpiration,
		MaxEntries: 1,
	})
	defer cache.Close()

	if !cache.TrySet("first", typedTestValue{Code: "1"}, DefaultExpiration) {
		t.Fatal("TrySet(first) rejected the first entry")
	}
	if cache.TrySet("second", typedTestValue{Code: "2"}, DefaultExpiration) {
		t.Fatal("TrySet(second) accepted an entry over MaxEntries")
	}

	cache.Clear()
	if _, found := cache.Get("first"); found {
		t.Fatal("Clear did not remove the typed value")
	}
}

func TestTypedCacheDeleteAndStats(t *testing.T) {
	cache := NewTyped[typedTestValue](NoExpiration)
	defer cache.Close()

	cache.Set("country", typedTestValue{Code: "ES"}, DefaultExpiration)
	if _, found := cache.Get("country"); !found {
		t.Fatal("expected typed cache hit")
	}

	cache.Delete("country")
	if _, found := cache.Get("country"); found {
		t.Fatal("Delete did not remove the typed value")
	}

	stats := cache.Stats()
	if stats.Items != 0 || stats.Hits != 1 || stats.Misses != 1 {
		t.Fatalf("unexpected typed cache stats: %+v", stats)
	}
}

func TestTypedCacheExpiration(t *testing.T) {
	cache := NewTyped[typedTestValue](NoExpiration)
	defer cache.Close()

	cache.Set("country", typedTestValue{Code: "BR"}, time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	if _, found := cache.Get("country"); found {
		t.Fatal("expired typed value was returned")
	}
}

func TestTypedCacheNilReceiver(t *testing.T) {
	var cache *TypedCache[typedTestValue]

	if cache.TrySet("key", typedTestValue{}, DefaultExpiration) {
		t.Fatal("nil receiver accepted TrySet")
	}
	if _, found := cache.Get("key"); found {
		t.Fatal("nil receiver returned a value")
	}
	cache.Set("key", typedTestValue{}, DefaultExpiration)
	cache.Delete("key")
	cache.Clear()
	cache.Close()
	if stats := cache.Stats(); stats != (Stats{}) {
		t.Fatalf("nil receiver Stats() = %+v; want zero stats", stats)
	}
}
