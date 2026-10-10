package gocache

import (
	"strconv"
	"testing"
	"time"
)

type benchmarkCache interface {
	Set(string, interface{}, time.Duration)
	Get(string) (interface{}, bool)
	Delete(string)
	Clear()
	Close()
}

type benchmarkCacheFactory func() benchmarkCache

const benchmarkKeyCount = 1024

func benchmarkFactories() map[string]benchmarkCacheFactory {
	return map[string]benchmarkCacheFactory{
		"New": func() benchmarkCache {
			return New(NoExpiration)
		},
		"NewFast": func() benchmarkCache {
			return NewFast(NoExpiration)
		},
	}
}

func benchmarkKeys() []string {
	keys := make([]string, benchmarkKeyCount)
	for index := range keys {
		keys[index] = "benchmark:key:" + strconv.Itoa(index)
	}
	return keys
}

func benchmarkCommonCaches(b *testing.B, operation func(*testing.B, benchmarkCache, []string)) {
	b.Helper()
	keys := benchmarkKeys()

	for name, factory := range benchmarkFactories() {
		b.Run(name, func(b *testing.B) {
			cache := factory()
			defer cache.Close()
			operation(b, cache, keys)
		})
	}
}

func fillBenchmarkCache(cache benchmarkCache, keys []string) {
	for _, key := range keys {
		cache.Set(key, "benchmark-value", NoExpiration)
	}
}

func BenchmarkGocacheNew(b *testing.B) {
	factories := benchmarkFactories()
	for _, name := range []string{"New", "NewFast"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			factory := factories[name]
			for index := 0; index < b.N; index++ {
				cache := factory()
				cache.Close()
			}
		})
	}
}

func BenchmarkGocacheSet(b *testing.B) {
	benchmarkCommonCaches(b, func(b *testing.B, cache benchmarkCache, keys []string) {
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			cache.Set(keys[index%len(keys)], "benchmark-value", NoExpiration)
		}
	})
}

func BenchmarkGocacheSetWithTTL(b *testing.B) {
	benchmarkCommonCaches(b, func(b *testing.B, cache benchmarkCache, keys []string) {
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			cache.Set(keys[index%len(keys)], "benchmark-value", time.Hour)
		}
	})
}

func BenchmarkGocacheGetHit(b *testing.B) {
	benchmarkCommonCaches(b, func(b *testing.B, cache benchmarkCache, keys []string) {
		fillBenchmarkCache(cache, keys)
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			value, found := cache.Get(keys[index%len(keys)])
			if !found || value != "benchmark-value" {
				b.Fatal("benchmark cache hit failed")
			}
		}
	})
}

func BenchmarkGocacheGetMiss(b *testing.B) {
	benchmarkCommonCaches(b, func(b *testing.B, cache benchmarkCache, keys []string) {
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			if _, found := cache.Get(keys[index%len(keys)]); found {
				b.Fatal("benchmark cache miss returned a value")
			}
		}
	})
}

func BenchmarkGocacheSetGet(b *testing.B) {
	benchmarkCommonCaches(b, func(b *testing.B, cache benchmarkCache, keys []string) {
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			key := keys[index%len(keys)]
			cache.Set(key, "benchmark-value", NoExpiration)
			if _, found := cache.Get(key); !found {
				b.Fatal("benchmark cache set/get failed")
			}
		}
	})
}

func BenchmarkGocacheSetDelete(b *testing.B) {
	benchmarkCommonCaches(b, func(b *testing.B, cache benchmarkCache, keys []string) {
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			key := keys[index%len(keys)]
			cache.Set(key, "benchmark-value", NoExpiration)
			cache.Delete(key)
		}
	})
}

func BenchmarkGocacheClear(b *testing.B) {
	benchmarkCommonCaches(b, func(b *testing.B, cache benchmarkCache, keys []string) {
		clearKeys := keys[:16]
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			b.StopTimer()
			fillBenchmarkCache(cache, clearKeys)
			b.StartTimer()
			cache.Clear()
		}
	})
}

func BenchmarkGocacheTrySet(b *testing.B) {
	cache := NewWithOptions(Options{MaxEntries: benchmarkKeyCount})
	defer cache.Close()

	keys := benchmarkKeys()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if !cache.TrySet(keys[index%len(keys)], "benchmark-value", NoExpiration) {
			b.Fatal("benchmark TrySet rejected an existing key")
		}
	}
}

func BenchmarkGocacheStats(b *testing.B) {
	cache := New(NoExpiration)
	defer cache.Close()
	fillBenchmarkCache(cache, benchmarkKeys())

	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if cache.Stats().Items == 0 {
			b.Fatal("benchmark Stats returned no items")
		}
	}
}

func BenchmarkGocacheTypedSetGet(b *testing.B) {
	type benchmarkValue struct {
		Code string
	}

	cache := NewTyped[benchmarkValue](NoExpiration)
	defer cache.Close()
	keys := benchmarkKeys()

	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		key := keys[index%len(keys)]
		cache.Set(key, benchmarkValue{Code: "benchmark-value"}, NoExpiration)
		if value, found := cache.Get(key); !found || value.Code != "benchmark-value" {
			b.Fatal("benchmark typed set/get failed")
		}
	}
}
