package agent

import (
	"strings"
	"testing"
)

func TestPersistedToolOutputIsBoundedAndSelfDescribing(t *testing.T) {
	original := strings.Repeat("head\n", maxPersistedToolOutputBytes) + "tail-marker"

	got := persistedToolOutput(original, "bash", "call-1")
	if len(got) > maxPersistedToolOutputBytes {
		t.Fatalf("persisted tool output length = %d, want <= %d", len(got), maxPersistedToolOutputBytes)
	}
	if !strings.Contains(got, "original_bytes=") || !strings.Contains(got, "call_id=call-1") {
		t.Fatalf("bounded tool output lacks provenance marker: %q", got[:min(len(got), 300)])
	}
	if !strings.Contains(got, "tail-marker") {
		t.Fatalf("bounded tool output lost tail evidence")
	}
}

func TestPersistedToolOutputPreservesSmallResultsByteForByte(t *testing.T) {
	original := "short tool result"
	if got := persistedToolOutput(original, "bash", "call-1"); got != original {
		t.Fatalf("small tool output changed: got %q, want %q", got, original)
	}
}

func TestPersistedToolOutputBoundsProvenanceLabels(t *testing.T) {
	original := strings.Repeat("x", maxPersistedToolOutputBytes+1)
	got := persistedToolOutput(original, strings.Repeat("tool", 1000), strings.Repeat("call", 1000))
	if len(got) > maxPersistedToolOutputBytes {
		t.Fatalf("persisted tool output with long labels = %d, want <= %d", len(got), maxPersistedToolOutputBytes)
	}
}
