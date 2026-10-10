package gocache

import (
	"sync"
	"time"
)

// LegacyFastCache is the legacy hash-only cache engine.
//
// It is kept as an opt-in compatibility and benchmark API. Unlike Cache, it
// does not retain the original key in each item. Different keys that produce
// the same hash can overwrite one another, so it must not be used when data
// correctness is more important than the reduced operation overhead.
type LegacyFastCache struct {
	shards [numShards]*legacyFastShard
	ttl    time.Duration

	stop        chan struct{}
	closeOnce   sync.Once
	cleanupDone chan struct{}
}

type legacyFastShard struct {
	mu       sync.RWMutex
	items    map[uint32]*legacyFastItem
	ringBuf  []legacyFastRingNode
	ringHead int
}

type legacyFastItem struct {
	value   interface{}
	expires int64
}

type legacyFastRingNode struct {
	hash    uint32
	expires int64
}

// NewLegacyFast creates the hash-only legacy cache.
//
// Prefer New or NewTyped for production data. Use this constructor only when
// the caller accepts the collision and replacement semantics documented above.
func NewLegacyFast(ttl ...time.Duration) *LegacyFastCache {
	var defaultTTL time.Duration
	if len(ttl) > 0 {
		defaultTTL = ttl[0]
	}

	cache := &LegacyFastCache{
		ttl:         defaultTTL,
		stop:        make(chan struct{}),
		cleanupDone: make(chan struct{}),
	}
	for index := 0; index < numShards; index++ {
		cache.shards[index] = &legacyFastShard{
			items:   make(map[uint32]*legacyFastItem),
			ringBuf: make([]legacyFastRingNode, ringSize),
		}
	}

	if cache.ttl > 0 {
		go cache.cleanup()
	} else {
		close(cache.cleanupDone)
	}

	return cache
}

// NewFast creates the opt-in legacy hash-only cache.
//
// It is an alias for NewLegacyFast. Prefer New or NewTyped when cache
// correctness is required.
func NewFast(ttl ...time.Duration) *LegacyFastCache {
	return NewLegacyFast(ttl...)
}

func (cache *LegacyFastCache) hashKey(key string) uint32 {
	var hash uint32
	for index := 0; index < len(key); index++ {
		hash ^= uint32(key[index])
		hash *= 16777619
	}
	return hash
}

func (cache *LegacyFastCache) expiration(ttl time.Duration) int64 {
	if ttl == DefaultExpiration {
		ttl = cache.ttl
	}
	if ttl <= 0 {
		return 0
	}
	return time.Now().Add(ttl).UnixNano()
}

// Set inserts or replaces a value using only the hashed key.
func (cache *LegacyFastCache) Set(key string, value interface{}, ttl time.Duration) {
	if cache == nil {
		return
	}

	hash := cache.hashKey(key)
	shard := cache.shards[hash%numShards]
	item := &legacyFastItem{value: value, expires: cache.expiration(ttl)}

	shard.mu.Lock()
	shard.items[hash] = item
	if item.expires > 0 {
		shard.ringBuf[shard.ringHead] = legacyFastRingNode{
			hash:    hash,
			expires: item.expires,
		}
		shard.ringHead = (shard.ringHead + 1) % ringSize
	}
	shard.mu.Unlock()
}

// Get retrieves a value using only the hashed key.
func (cache *LegacyFastCache) Get(key string) (interface{}, bool) {
	if cache == nil {
		return nil, false
	}

	hash := cache.hashKey(key)
	shard := cache.shards[hash%numShards]

	shard.mu.RLock()
	item, found := shard.items[hash]
	if !found {
		shard.mu.RUnlock()
		return nil, false
	}
	if item.expires <= 0 || time.Now().UnixNano() < item.expires {
		value := item.value
		shard.mu.RUnlock()
		return value, true
	}
	shard.mu.RUnlock()

	cache.Delete(key)
	return nil, false
}

// Delete removes the item associated with the hashed key.
func (cache *LegacyFastCache) Delete(key string) {
	if cache == nil {
		return
	}

	hash := cache.hashKey(key)
	shard := cache.shards[hash%numShards]
	shard.mu.Lock()
	delete(shard.items, hash)
	shard.mu.Unlock()
}

// Clear removes all items and resets all expiration ring buffers.
func (cache *LegacyFastCache) Clear() {
	if cache == nil {
		return
	}

	for _, shard := range cache.shards {
		shard.mu.Lock()
		shard.items = make(map[uint32]*legacyFastItem)
		for index := range shard.ringBuf {
			shard.ringBuf[index] = legacyFastRingNode{}
		}
		shard.ringHead = 0
		shard.mu.Unlock()
	}
}

func (cache *LegacyFastCache) cleanup() {
	interval := cache.ttl / 2
	if interval <= 0 {
		interval = cache.ttl
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer close(cache.cleanupDone)

	for {
		select {
		case <-ticker.C:
			cache.cleanupExpired(time.Now().UnixNano())
		case <-cache.stop:
			return
		}
	}
}

func (cache *LegacyFastCache) cleanupExpired(now int64) {
	for _, shard := range cache.shards {
		shard.mu.Lock()
		for index := range shard.ringBuf {
			node := &shard.ringBuf[index]
			if node.expires <= 0 || now < node.expires {
				continue
			}
			if item, found := shard.items[node.hash]; found && item.expires > 0 && item.expires <= now {
				delete(shard.items, node.hash)
			}
			*node = legacyFastRingNode{}
		}
		shard.mu.Unlock()
	}
}

// Close stops the cleanup goroutine, when one is running.
func (cache *LegacyFastCache) Close() {
	if cache == nil {
		return
	}
	cache.closeOnce.Do(func() {
		close(cache.stop)
	})
	<-cache.cleanupDone
}
