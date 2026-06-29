package control

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/memory"
)

// TestMemoryWriteReflectsInSnapshot verifies that a memory write lands on disk
// and that Memory() returns a freshly reloaded snapshot afterwards — the behavior
// the memoryManager (off-c.mu) extraction must preserve.
func TestMemoryWriteReflectsInSnapshot(t *testing.T) {
	dir := t.TempDir()
	c := New(Options{Memory: memory.Load(memory.Options{CWD: dir})})

	before := c.Memory()
	if before == nil {
		t.Fatal("memory should be enabled")
	}

	path, err := c.QuickAdd(memory.ScopeProject, "prefer tabs over spaces")
	if err != nil {
		t.Fatalf("QuickAdd: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	if !strings.Contains(string(body), "prefer tabs over spaces") {
		t.Fatalf("note not written to disk:\n%s", body)
	}

	after := c.Memory()
	if after == nil {
		t.Fatal("memory snapshot is nil after QuickAdd")
	}
	if after == before {
		t.Fatal("Memory() returned the stale snapshot; the manager did not swap in a reload")
	}
}

// TestMemoryWritesConcurrencySafe hammers memory writes from many goroutines
// while c.mu-guarded reads run concurrently. Under -race this proves the
// memoryManager's writeMu/mu split has no data race and no deadlock — and that
// holding writeMu (off c.mu) across the disk I/O still serializes writes so every
// note lands.
func TestMemoryWritesConcurrencySafe(t *testing.T) {
	dir := t.TempDir()
	c := New(Options{Memory: memory.Load(memory.Options{CWD: dir})})

	const writers = 8
	const each = 5

	stop := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = c.Running()       // takes c.mu
				_ = c.RuntimeStatus() // takes c.mu
				_ = c.Memory()        // takes c.mu, returns the snapshot pointer
			}
		}
	}()

	var writersWG sync.WaitGroup
	for w := range writers {
		writersWG.Add(1)
		go func(w int) {
			defer writersWG.Done()
			for i := range each {
				if _, err := c.QuickAdd(memory.ScopeProject, fmt.Sprintf("note w%d-%d", w, i)); err != nil {
					t.Errorf("QuickAdd: %v", err)
				}
			}
		}(w)
	}
	writersWG.Wait()
	close(stop)
	readers.Wait()

	body, err := os.ReadFile(c.Memory().DocPath(memory.ScopeProject))
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	for w := range writers {
		for i := range each {
			want := fmt.Sprintf("note w%d-%d", w, i)
			if !strings.Contains(string(body), want) {
				t.Fatalf("memory doc missing %q after concurrent writes:\n%s", want, body)
			}
		}
	}
}
