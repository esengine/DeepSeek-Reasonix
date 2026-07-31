package safety

import (
	"testing"
	"time"
)

func TestIsStale(t *testing.T) {
	window := 30 * time.Minute

	// Test with 0
	if IsStale(0, window) {
		t.Error("Expected false for 0 createTimeMs")
	}

	// Test with current time
	now := time.Now().UnixMilli()
	if IsStale(now, window) {
		t.Error("Expected false for current time")
	}

	// Test with stale time
	staleTime := time.Now().Add(-40 * time.Minute).UnixMilli()
	if !IsStale(staleTime, window) {
		t.Error("Expected true for stale time")
	}
}
