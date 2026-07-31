package control

import (
	"fmt"
	"os"
	"path/filepath"
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

func TestSaveMemoryQueuesFullBodyForCurrentSession(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "user")
	cwd := filepath.Join(root, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	c := New(Options{Memory: memory.Load(memory.Options{CWD: cwd, UserDir: userDir})})

	body := "Always answer in Chinese unless the user explicitly asks for English.\nKeep technical terms precise."
	if _, err := c.SaveMemory(memory.Memory{
		Name:        "response-language",
		Description: "preferred response language",
		Type:        memory.TypeUser,
		Scope:       memory.FactScopeGlobal,
		Body:        body,
	}); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	composed := c.Compose("hello")
	if !strings.Contains(composed, "Saved memory \"response-language\"") || !strings.Contains(composed, body) {
		t.Fatalf("saved memory name and body should ride the next turn:\n%s", composed)
	}
	if again := c.Compose("again"); strings.Contains(again, body) || strings.Contains(again, "<memory-update>") {
		t.Fatalf("saved memory update should drain after one turn: %q", again)
	}
}

func TestForgetMemoryRevokesLoadedGlobalGuidanceForCurrentSession(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "user")
	cwd := filepath.Join(root, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	store := memory.StoreFor(userDir, cwd)
	const body = "Never use emoji in responses."
	if _, err := store.Save(memory.Memory{
		Name:        "no-emoji",
		Description: "avoid emoji",
		Type:        memory.TypeFeedback,
		Scope:       memory.FactScopeGlobal,
		Body:        body,
	}); err != nil {
		t.Fatal(err)
	}
	c := New(Options{Memory: memory.Load(memory.Options{CWD: cwd, UserDir: userDir})})
	if before := c.Memory().Block(); !strings.Contains(before, body) {
		t.Fatalf("test setup did not load global guidance:\n%s", before)
	}

	if err := c.ForgetMemory("no-emoji"); err != nil {
		t.Fatalf("ForgetMemory: %v", err)
	}
	if after := c.Memory().Block(); strings.Contains(after, body) {
		t.Fatalf("reloaded snapshot retained forgotten global guidance:\n%s", after)
	}
	composed := c.Compose("hello")
	for _, want := range []string{"Forgot memory \"no-emoji\"", "disregard its loaded guidance", "background-index entry"} {
		if !strings.Contains(composed, want) {
			t.Fatalf("forget update missing %q:\n%s", want, composed)
		}
	}
}

func TestRestoreArchivedMemoryQueuesFullBodyForCurrentSession(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "user")
	cwd := filepath.Join(root, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	store := memory.StoreFor(userDir, cwd)
	first, err := store.SaveWithOptions(memory.Memory{
		Name: "build-contract", Description: "project build contract", Body: "Run the focused package tests before the full suite.",
	}, memory.SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	archivePath, err := store.Archive(first.Memory.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := New(Options{Memory: memory.Load(memory.Options{CWD: cwd, UserDir: userDir})})

	restored, err := c.RestoreArchivedMemory(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID != first.Memory.ID || restored.Revision != 2 {
		t.Fatalf("restored memory = %+v", restored)
	}
	composed := c.Compose("continue")
	for _, want := range []string{"Recovered archived memory", "build-contract", "Run the focused package tests"} {
		if !strings.Contains(composed, want) {
			t.Fatalf("recovery update missing %q:\n%s", want, composed)
		}
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

func TestRestoreMemoryQueuesAuditedRevisionForNextTurn(t *testing.T) {
	dir := t.TempDir()
	c := New(Options{Memory: memory.Load(memory.Options{CWD: dir, UserDir: t.TempDir()})})
	store := c.Memory().Store
	first, err := store.SaveWithOptions(memory.Memory{Name: "release-target", Description: "v1", Body: "main-v2"}, memory.SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveWithOptions(memory.Memory{ID: first.Memory.ID, Name: "release-target", Description: "v2", Body: "release-v2"}, memory.SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	c.memory.applyWrite(c.Memory(), "")

	restored, err := c.RestoreMemory(first.Memory.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Revision != 3 || restored.Body != "main-v2" {
		t.Fatalf("restored = %+v", restored)
	}
	if revisions := c.MemoryRevisions(first.Memory.ID); len(revisions) < 2 {
		t.Fatalf("revision history = %+v", revisions)
	}
	composed := c.Compose("continue")
	if !strings.Contains(composed, "Restored memory") || !strings.Contains(composed, "revision 3") {
		t.Fatalf("restore note did not ride next turn: %q", composed)
	}
}
