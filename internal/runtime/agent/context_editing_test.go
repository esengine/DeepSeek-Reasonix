package agent

import (
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

// Native provider context editing was removed. All providers use the local
// summary checkpoint path. These tests lock that contract so native flags and
// request-shape switches cannot reappear silently.
func TestNativeContextEditingRemoved(t *testing.T) {
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{
		ContextWindow:  100_000,
		CompactRatio:   0.85,
		ContextEditing: "native", // deprecated input; ignored
	}, event.Discard)
	// No native lineage suffix on the prompt cache key.
	if key := a.window().currentPromptCacheKey(); key != a.window().currentPromptCacheKey() {
		t.Fatal("prompt cache key unstable")
	}
	// Nothing installs a projection outside Prepare: observing usage is not a
	// maintenance entry, and there is no longer a hook that could pretend to be.
	if got := a.window().currentProjectionVersion(); got != 0 {
		t.Fatalf("projection version %d installed without Prepare", got)
	}
}
