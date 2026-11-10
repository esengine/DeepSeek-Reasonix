package serve

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/sessionstore"
)

func TestOpenLookupFindsAHeldRowWhateverTheCaseOfItsFileName(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "20261018-101500.000000000-Qwen3-Max.jsonl")
	open := map[string]string{sessionstore.CanonicalSessionPath(path): "rt-1"}

	if got := openLookup(open)(path); got != "rt-1" {
		t.Fatalf("a row for the pane's own transcript has runtime %q, want rt-1", got)
	}
	if got := openLookup(open)(filepath.Join(filepath.Dir(path), "other.jsonl")); got != "" {
		t.Fatalf("an unrelated transcript is held by %q", got)
	}
}
