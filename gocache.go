// Package gocache provides a sharded in-memory cache with expiration handling.
package gocache

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultExpiration uses the cache default TTL.
	DefaultExpiration time.Duration = 0

	// NoExpiration keeps an item until it is deleted or the cache is cleared.
	NoExpiration time.Duration = -1

	numShards = 8
	ringSize  = 4096
)

// Options configures optional cache limits and observability.
type Options struct {
	TTL        time.Duration
	MaxEntries int
	MaxBytes   int64
	SizeFunc   func(key string, value interface{}) int64
}

// Stats contains cache counters and approximate memory usage.
type Stats struct {
	Items          int
	Hits           uint64
	Misses         uint64
	Expired        uint64
	Collisions     uint64
	EstimatedBytes int64
}

// ringNode tracks one expiring version of one exact key.
type ringNode struct {
	hash       uint32
	key        string
	expires    int64
	generation uint64
}

// shard partitions cache writes and reads.
type shard struct {
	mu       sync.RWMutex
	items    map[uint32][]*Item
	ringBuf  []ringNode
	ringHead int
}

// Item represents a cached value.
type Item struct {
	key        string
	value      interface{}
	expires    int64
	generation uint64
	size       int64
}

// Cache is a sharded in-memory cache.
type Cache struct {
	shards [numShards]*shard
	ttl    time.Duration

	maxEntries int
	maxBytes   int64
	sizeFunc   func(key string, value interface{}) int64

	capacityMu sync.Mutex

	generation atomic.Uint64
	entries    atomic.Int64
	bytes      atomic.Int64
	hits       atomic.Uint64
	misses     atomic.Uint64
	expired    atomic.Uint64
	collisions atomic.Uint64

	stop        chan struct{}
	closeOnce   sync.Once
	cleanupDone chan struct{}
}

// New preserves the original constructor API.
func New(ttlStr ...time.Duration) *Cache {
	var ttl time.Duration
	if len(ttlStr) > 0 {
		ttl = ttlStr[0]
	}
	return NewWithOptions(Options{TTL: ttl})
}

// NewWithOptions creates a cache with optional limits and statistics sizing.
func NewWithOptions(options Options) *Cache {
	cache := &Cache{
		ttl:         options.TTL,
		maxEntries:  options.MaxEntries,
		maxBytes:    options.MaxBytes,
		sizeFunc:    options.SizeFunc,
		stop:        make(chan struct{}),
		cleanupDone: make(chan struct{}),
	}
	if cache.sizeFunc == nil {
		cache.sizeFunc = defaultSize
	}

	for i := 0; i < numShards; i++ {
		cache.shards[i] = &shard{
			items:   make(map[uint32][]*Item),
			ringBuf: make([]ringNode, ringSize),
		}
	}

	if cache.ttl > 0 {
		go cache.cleanup()
	} else {
		close(cache.cleanupDone)
	}

	return cache
}

func defaultSize(key string, value interface{}) int64 {
	size := int64(len(key) + 64)
	switch typedValue := value.(type) {
	case string:
		size += int64(len(typedValue))
	case []byte:
		size += int64(len(typedValue))
	}
	return size
}

func (cache *Cache) hashKey(key string) uint32 {
	var hash uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= 16777619
	}
	return hash
}

func (cache *Cache) getShard(hash uint32) *shard {
	return cache.shards[hash%numShards]
}

func (cache *Cache) expiration(ttl time.Duration) int64 {
	if ttl == DefaultExpiration {
		ttl = cache.ttl
	}
	if ttl <= 0 {
		return 0
	}
	return time.Now().Add(ttl).UnixNano()
}

func (cache *Cache) nextGeneration() uint64 {
	return cache.generation.Add(1)
}

// Set inserts or replaces a value. Existing callers keep the original API.
func (cache *Cache) Set(key string, value interface{}, ttl time.Duration) {
	_ = cache.TrySet(key, value, ttl)
}

// TrySet inserts or replaces a value and reports whether it was stored.
// It returns false when an optional capacity limit rejects a new entry.
func (cache *Cache) TrySet(key string, value interface{}, ttl time.Duration) bool {
	if cache == nil {
		return false
	}

	hash := cache.hashKey(key)
	item := &Item{
		key:        key,
		value:      value,
		expires:    cache.expiration(ttl),
		generation: cache.nextGeneration(),
		size:       cache.sizeFunc(key, value),
	}

	if item.size < 0 {
		item.size = 0
	}

	if cache.maxEntries <= 0 && cache.maxBytes <= 0 {
		return cache.setUnbounded(hash, item)
	}

	cache.capacityMu.Lock()
	defer cache.capacityMu.Unlock()
	return cache.setBounded(hash, item)
}

func (cache *Cache) setUnbounded(hash uint32, item *Item) bool {
	sh := cache.getShard(hash)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	bucket := sh.items[hash]
	index := findItem(bucket, item.key)
	if index >= 0 {
		previous := bucket[index]
		bucket[index] = item
		cache.bytes.Add(item.size - previous.size)
		cache.addRingNode(sh, hash, item)
		return true
	}

	if len(bucket) > 0 {
		cache.collisions.Add(1)
	}
	sh.items[hash] = append(bucket, item)
	cache.entries.Add(1)
	cache.bytes.Add(item.size)
	cache.addRingNode(sh, hash, item)
	return true
}

func (cache *Cache) setBounded(hash uint32, item *Item) bool {
	sh := cache.getShard(hash)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	bucket := sh.items[hash]
	index := findItem(bucket, item.key)
	if index >= 0 {
		previous := bucket[index]
		newBytes := cache.bytes.Load() - previous.size + item.size
		if cache.maxBytes > 0 && newBytes > cache.maxBytes {
			return false
		}
		bucket[index] = item
		cache.bytes.Store(newBytes)
		cache.addRingNode(sh, hash, item)
		return true
	}

	if cache.maxEntries > 0 && int(cache.entries.Load()) >= cache.maxEntries {
		return false
	}
	if cache.maxBytes > 0 && cache.bytes.Load()+item.size > cache.maxBytes {
		return false
	}

	if len(bucket) > 0 {
		cache.collisions.Add(1)
	}
	sh.items[hash] = append(bucket, item)
	cache.entries.Add(1)
	cache.bytes.Add(item.size)
	cache.addRingNode(sh, hash, item)
	return true
}

func (cache *Cache) addRingNode(sh *shard, hash uint32, item *Item) {
	if item.expires <= 0 {
		return
	}
	sh.ringBuf[sh.ringHead] = ringNode{
		hash:       hash,
		key:        item.key,
		expires:    item.expires,
		generation: item.generation,
	}
	sh.ringHead = (sh.ringHead + 1) % ringSize
}

func findItem(bucket []*Item, key string) int {
	for index, item := range bucket {
		if item.key == key {
			return index
		}
	}
	return -1
}

// Get retrieves a value and reports whether it exists and is not expired.
func (cache *Cache) Get(key string) (interface{}, bool) {
	if cache == nil {
		return nil, false
	}

	hash := cache.hashKey(key)
	sh := cache.getShard(hash)

	sh.mu.RLock()
	bucket := sh.items[hash]
	index := findItem(bucket, key)
	if index < 0 {
		sh.mu.RUnlock()
		cache.misses.Add(1)
		return nil, false
	}

	item := bucket[index]
	now := time.Now().UnixNano()
	if item.expires > 0 && now >= item.expires {
		generation := item.generation
		sh.mu.RUnlock()
		cache.expired.Add(1)
		cache.misses.Add(1)
		cache.deleteIfGeneration(hash, key, generation)
		return nil, false
	}

	value := item.value
	sh.mu.RUnlock()
	cache.hits.Add(1)
	return value, true
}

func (cache *Cache) deleteIfGeneration(hash uint32, key string, generation uint64) {
	if cache.maxEntries > 0 || cache.maxBytes > 0 {
		cache.capacityMu.Lock()
		defer cache.capacityMu.Unlock()
	}
	cache.deleteIfGenerationLocked(hash, key, generation)
}

func (cache *Cache) deleteIfGenerationLocked(hash uint32, key string, generation uint64) {
	sh := cache.getShard(hash)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	bucket := sh.items[hash]
	index := findItem(bucket, key)
	if index < 0 || bucket[index].generation != generation {
		return
	}
	cache.removeItemLocked(sh, hash, index)
}

func (cache *Cache) removeItemLocked(sh *shard, hash uint32, index int) {
	bucket := sh.items[hash]
	removed := bucket[index]
	copy(bucket[index:], bucket[index+1:])
	bucket[len(bucket)-1] = nil
	bucket = bucket[:len(bucket)-1]

	if len(bucket) == 0 {
		delete(sh.items, hash)
	} else {
		sh.items[hash] = bucket
	}
	cache.entries.Add(-1)
	cache.bytes.Add(-removed.size)
}

// Delete removes an exact key without affecting colliding keys.
func (cache *Cache) Delete(key string) {
	if cache == nil {
		return
	}

	hash := cache.hashKey(key)
	if cache.maxEntries > 0 || cache.maxBytes > 0 {
		cache.capacityMu.Lock()
		defer cache.capacityMu.Unlock()
	}

	sh := cache.getShard(hash)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	index := findItem(sh.items[hash], key)
	if index >= 0 {
		cache.removeItemLocked(sh, hash, index)
	}
}

func (cache *Cache) cleanup() {
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

func (cache *Cache) cleanupExpired(now int64) {
	if cache.maxEntries > 0 || cache.maxBytes > 0 {
		cache.capacityMu.Lock()
		defer cache.capacityMu.Unlock()
	}

	for _, sh := range cache.shards {
		sh.mu.Lock()
		for index := range sh.ringBuf {
			node := &sh.ringBuf[index]
			if node.expires <= 0 || now < node.expires {
				continue
			}

			bucket := sh.items[node.hash]
			itemIndex := findItem(bucket, node.key)
			if itemIndex >= 0 {
				item := bucket[itemIndex]
				if item.generation == node.generation &&
					item.expires > 0 &&
					item.expires <= now {
					cache.removeItemLocked(sh, node.hash, itemIndex)
				}
			}
			*node = ringNode{}
		}
		sh.mu.Unlock()
	}
}

// Close stops the optional cleanup goroutine.
func (cache *Cache) Close() {
	if cache == nil {
		return
	}
	cache.closeOnce.Do(func() {
		close(cache.stop)
	})
	<-cache.cleanupDone
}

// Clear removes all keys and resets the ring buffers.
func (cache *Cache) Clear() {
	if cache == nil {
		return
	}
	if cache.maxEntries > 0 || cache.maxBytes > 0 {
		cache.capacityMu.Lock()
		defer cache.capacityMu.Unlock()
	}

	for _, sh := range cache.shards {
		sh.mu.Lock()
		sh.items = make(map[uint32][]*Item)
		for index := range sh.ringBuf {
			sh.ringBuf[index] = ringNode{}
		}
		sh.ringHead = 0
		sh.mu.Unlock()
	}
	cache.entries.Store(0)
	cache.bytes.Store(0)
}

func (cache *Cache) Stats() Stats {
	if cache == nil {
		return Stats{}
	}
	return Stats{
		Items:          int(cache.entries.Load()),
		Hits:           cache.hits.Load(),
		Misses:         cache.misses.Load(),
		Expired:        cache.expired.Load(),
		Collisions:     cache.collisions.Load(),
		EstimatedBytes: cache.bytes.Load(),
	}
}
