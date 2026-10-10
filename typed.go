package gocache

import "time"

// TypedCache is a type-safe facade over Cache.
//
// It uses the same sharded storage and synchronization as Cache while exposing
// typed Set and Get operations to application code.
type TypedCache[T any] struct {
	cache *Cache
}

// NewTyped creates a typed cache backed by the default Cache implementation.
func NewTyped[T any](ttl ...time.Duration) *TypedCache[T] {
	return &TypedCache[T]{cache: New(ttl...)}
}

// NewTypedWithOptions creates a typed cache with the same limits and
// observability options available through NewWithOptions.
func NewTypedWithOptions[T any](options Options) *TypedCache[T] {
	return &TypedCache[T]{cache: NewWithOptions(options)}
}

// Set inserts or replaces a typed value.
func (cache *TypedCache[T]) Set(key string, value T, ttl time.Duration) {
	if cache == nil {
		return
	}
	cache.cache.Set(key, value, ttl)
}

// TrySet inserts or replaces a typed value and reports whether it was stored.
func (cache *TypedCache[T]) TrySet(key string, value T, ttl time.Duration) bool {
	if cache == nil {
		return false
	}
	return cache.cache.TrySet(key, value, ttl)
}

// Get retrieves a typed value. The zero value of T is returned on a miss.
func (cache *TypedCache[T]) Get(key string) (T, bool) {
	var zero T
	if cache == nil {
		return zero, false
	}

	value, found := cache.cache.Get(key)
	if !found {
		return zero, false
	}

	result, valid := value.(T)
	if !valid {
		return zero, false
	}
	return result, true
}

// Delete removes an exact key.
func (cache *TypedCache[T]) Delete(key string) {
	if cache == nil {
		return
	}
	cache.cache.Delete(key)
}

// Clear removes every entry from the cache.
func (cache *TypedCache[T]) Clear() {
	if cache == nil {
		return
	}
	cache.cache.Clear()
}

// Stats returns cache counters and the approximate memory usage.
func (cache *TypedCache[T]) Stats() Stats {
	if cache == nil {
		return Stats{}
	}
	return cache.cache.Stats()
}

// Close stops the cache cleanup goroutine, when one is running.
func (cache *TypedCache[T]) Close() {
	if cache == nil {
		return
	}
	cache.cache.Close()
}
