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

func TestSaveMemoryRefreshesBackgroundSnapshotWithoutLegacyUpdate(t *testing.T) {
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

	background := c.Memory().BackgroundDataBlock()
	if !strings.Contains(background, "response-language") || !strings.Contains(background, body) {
		t.Fatalf("saved memory missing from refreshed background snapshot:\n%s", background)
	}
	if composed := c.Compose("hello"); strings.Contains(composed, "<memory-update>") || composed != "hello" {
		t.Fatalf("background save generated legacy update: %q", composed)
	}
}

func TestForgetMemoryRefreshesBackgroundSnapshotWithoutLegacyUpdate(t *testing.T) {
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
	if strings.Contains(composed, "<memory-update>") || composed != "hello" {
		t.Fatalf("forget generated legacy update: %q", composed)
	}
}

func TestRestoreArchivedMemoryRefreshesBackgroundSnapshotWithoutLegacyUpdate(t *testing.T) {
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
	background := c.Memory().BackgroundDataBlock()
	if !strings.Contains(background, "build-contract") || !strings.Contains(background, "project build contract") {
		t.Fatalf("restored memory missing from refreshed background snapshot:\n%s", background)
	}
	if composed := c.Compose("continue"); strings.Contains(composed, "<memory-update>") || composed != "continue" {
		t.Fatalf("archived restore generated legacy update: %q", composed)
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
	readers.Go(func() {
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
	})

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

func TestRestoreMemoryRefreshesBackgroundSnapshotWithoutLegacyUpdate(t *testing.T) {
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
	background := c.Memory().BackgroundDataBlock()
	if !strings.Contains(background, "release-target") || !strings.Contains(background, "v1") {
		t.Fatalf("restored revision missing from refreshed background snapshot: %q", background)
	}
	if composed := c.Compose("continue"); strings.Contains(composed, "<memory-update>") || composed != "continue" {
		t.Fatalf("revision restore generated legacy update: %q", composed)
	}
}
