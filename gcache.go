// Package gocache provides a cache storage solution with expiration handling.
// It is designed for high-performance applications that require fast data retrieval
// with minimal contention.
package gocache

import (
	"sync"
	"time"
)

const (
	DefaultExpiration time.Duration = 0    // Uses default TTL if not specified
	NoExpiration      time.Duration = -1   // Items with no expiration time
	numShards                       = 8    // Number of shards for concurrent access
	ringSize                        = 4096 // Size of the expiration ring buffer
)

// ringNode represents an entry in the expiration ring buffer.
type ringNode struct {
	key     uint32 // Hashed key
	expires int64  // Expiration timestamp in nanoseconds
}

// shard is a partition of the cache with its own locking mechanism.
type shard struct {
	mu       sync.RWMutex     // Mutex for concurrent access
	items    map[uint32]*Item // Cached items
	ringBuf  []ringNode       // Ring buffer for tracking expiration
	ringHead int              // Current position in the ring buffer
}

// Item represents a single cache entry.
type Item struct {
	value   interface{} // Stored value
	expires int64       // Expiration timestamp
}

// Cache is a sharded in-memory cache with expiration handling.
type Cache struct {
	shards [numShards]*shard // Array of shards to reduce contention
	ttl    time.Duration     // Default time-to-live for cache entries
}

// New creates a new instance of Cache with a given TTL.
func New(ttl time.Duration) *Cache {
	c := &Cache{ttl: ttl}
	for i := 0; i < numShards; i++ {
		c.shards[i] = &shard{
			items:   make(map[uint32]*Item),
			ringBuf: make([]ringNode, ringSize),
		}
	}
	if ttl > 0 {
		go c.cleanup()
	}
	return c
}

// hashKey computes a simple hash from the string key using FNV-1a variation.
func (c *Cache) hashKey(key string) uint32 {
	var h uint32
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return h
}

// getShard selects the shard based on the hash value.
func (c *Cache) getShard(k uint32) *shard {
	return c.shards[k%numShards]
}

// Set inserts a value into the cache with an optional TTL.
func (c *Cache) Set(key string, value interface{}, ttl time.Duration) {
	var exp int64
	if ttl == DefaultExpiration {
		ttl = c.ttl
	}
	if ttl > 0 {
		exp = time.Now().Add(ttl).UnixNano()
	}

	hashed := c.hashKey(key)
	sh := c.getShard(hashed)

	sh.mu.Lock()
	sh.items[hashed] = &Item{value: value, expires: exp}
	sh.ringBuf[sh.ringHead] = ringNode{key: hashed, expires: exp}
	sh.ringHead = (sh.ringHead + 1) % ringSize
	sh.mu.Unlock()
}

// Get retrieves a value from the cache.
// If the item has expired, it is deleted and returns (nil, false).
func (c *Cache) Get(key string) (interface{}, bool) {
	hashed := c.hashKey(key)
	sh := c.getShard(hashed)

	sh.mu.RLock()
	item, exists := sh.items[hashed]
	sh.mu.RUnlock()

	if !exists {
		return nil, false
	}

	if item.expires > 0 && time.Now().UnixNano() > item.expires {
		c.Delete(key) // Remove expired item
		return nil, false
	}

	return item.value, true
}

// Delete removes an item from the cache.
func (c *Cache) Delete(key string) {
	hashed := c.hashKey(key)
	sh := c.getShard(hashed)

	sh.mu.Lock()
	delete(sh.items, hashed)
	sh.mu.Unlock()
}

// cleanup periodically removes expired items from the cache.
func (c *Cache) cleanup() {
	tick := time.NewTicker(c.ttl / 2)
	defer tick.Stop()

	for range tick.C {
		now := time.Now().UnixNano()
		for _, sh := range c.shards {
			sh.mu.Lock()
			for i := 0; i < ringSize; i++ {
				node := &sh.ringBuf[i]
				if node.expires > 0 && now > node.expires {
					delete(sh.items, node.key)
					node.expires = 0
				}
			}
			sh.mu.Unlock()
		}
	}
}
