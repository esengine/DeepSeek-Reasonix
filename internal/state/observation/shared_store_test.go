package observation

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/state/trustedstate"
)

func TestTwoObserversShareStateDirectory(t *testing.T) {
	root := t.TempDir()
	for i := range 128 {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("d%04d", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for range 32 {
		state := t.TempDir()
		start := make(chan struct{})
		results := make(chan Snapshot, 2)
		for range 2 {
			o := NewObserver(trustedstate.Open(state, nil), DefaultPolicy())
			go func() {
				<-start
				results <- o.Take(t.Context(), root)
			}()
		}
		close(start)
		a, b := <-results, <-results
		if !a.Complete || !b.Complete || a.Digest != b.Digest {
			t.Fatalf("shared-store snapshots: %+v / %+v", a, b)
		}
		reopened := NewObserver(trustedstate.Open(state, nil), DefaultPolicy())
		if paths, count, err := reopened.Changed(a, b, 10); err != nil || count != 0 || len(paths) != 0 {
			t.Fatalf("reopened comparison: %v, %d, %v", paths, count, err)
		}
	}
}
