package gocache

import (
	"testing"
	"time"
)

func TestLegacyFastCacheSetGetAndClear(t *testing.T) {
	cache := NewLegacyFast(NoExpiration)
	defer cache.Close()

	cache.Set("key", "value", DefaultExpiration)
	value, found := cache.Get("key")
	if !found || value != "value" {
		t.Fatalf("Get(key) = %#v, %v; want value, true", value, found)
	}

	cache.Clear()
	if _, found := cache.Get("key"); found {
		t.Fatal("Clear did not remove the value")
	}
}

func TestNewFastAliasesLegacyFast(t *testing.T) {
	cache := NewFast(NoExpiration)
	defer cache.Close()

	cache.Set("key", "value", DefaultExpiration)
	value, found := cache.Get("key")
	if !found || value != "value" {
		t.Fatalf("Get(key) = %#v, %v; want value, true", value, found)
	}
}

func TestLegacyFastCacheExpiration(t *testing.T) {
	cache := NewLegacyFast(NoExpiration)
	defer cache.Close()

	cache.Set("key", "value", time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	if _, found := cache.Get("key"); found {
		t.Fatal("expired value was returned")
	}
}

func TestLegacyFastCacheCloseIsIdempotent(t *testing.T) {
	cache := NewLegacyFast(10 * time.Millisecond)
	cache.Close()
	cache.Close()
}

func TestLegacyFastCacheNilReceiver(t *testing.T) {
	var cache *LegacyFastCache

	cache.Set("key", "value", DefaultExpiration)
	if _, found := cache.Get("key"); found {
		t.Fatal("nil receiver returned a value")
	}
	cache.Delete("key")
	cache.Clear()
	cache.Close()
}
