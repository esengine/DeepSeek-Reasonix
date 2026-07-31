package safety

import (
	"testing"
	"time"
)

func TestDedupCache_IsDuplicate(t *testing.T) {
	cache := NewDedupCache(3, 50*time.Millisecond)

	// First time should not be duplicate
	if cache.IsDuplicate("event1") {
		t.Error("Expected event1 to not be duplicate")
	}

	// Second time should be duplicate
	if !cache.IsDuplicate("event1") {
		t.Error("Expected event1 to be duplicate")
	}

	// Add more items to trigger eviction
	cache.IsDuplicate("event2")
	cache.IsDuplicate("event3")
	cache.IsDuplicate("event4") // Evicts event1

	// event1 should be evicted, so not a duplicate
	if cache.IsDuplicate("event1") {
		t.Error("Expected event1 to not be duplicate after eviction")
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// event4 should be expired, so not a duplicate
	if cache.IsDuplicate("event4") {
		t.Error("Expected event4 to not be duplicate after expiration")
	}
}
