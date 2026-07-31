package safety

import (
	"container/list"
	"sync"
	"time"
)

type cacheItem struct {
	key       string
	expiresAt time.Time
}

// DedupCache provides a thread-safe LRU cache with TTL for deduplicating events.
type DedupCache struct {
	mu        sync.Mutex
	capacity  int
	ttl       time.Duration
	items     map[string]*list.Element
	evictList *list.List
}

// NewDedupCache creates a new DedupCache with the given capacity and time-to-live.
func NewDedupCache(capacity int, ttl time.Duration) *DedupCache {
	return &DedupCache{
		capacity:  capacity,
		ttl:       ttl,
		items:     make(map[string]*list.Element),
		evictList: list.New(),
	}
}

// IsDuplicate checks if the key exists and is not expired.
// If it's a new key or an expired key, it adds/updates it and returns false.
// If it's a duplicate, it returns true.
func (c *DedupCache) IsDuplicate(key string) bool {
	if key == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if key exists
	if ent, ok := c.items[key]; ok {
		item := ent.Value.(*cacheItem)
		// If it's expired, remove it and we'll treat it as not duplicate
		if time.Now().After(item.expiresAt) {
			c.removeElement(ent)
			// Continue to add it below
		} else {
			// It's valid, so it's a duplicate
			// Move to front (LRU)
			c.evictList.MoveToFront(ent)
			return true
		}
	}

	// Add new item
	item := &cacheItem{
		key:       key,
		expiresAt: time.Now().Add(c.ttl),
	}
	ent := c.evictList.PushFront(item)
	c.items[key] = ent

	// Evict if over capacity
	if c.evictList.Len() > c.capacity {
		c.removeOldest()
	}

	return false
}

func (c *DedupCache) removeOldest() {
	ent := c.evictList.Back()
	if ent != nil {
		c.removeElement(ent)
	}
}

func (c *DedupCache) removeElement(e *list.Element) {
	c.evictList.Remove(e)
	kv := e.Value.(*cacheItem)
	delete(c.items, kv.key)
}
