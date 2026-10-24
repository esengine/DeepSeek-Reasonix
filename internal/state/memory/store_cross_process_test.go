package memory

// Cross-process audit tests: a second process is modelled by running Save's own
// unexported steps at a fault ordinal, but only after trying the mutation lock,
// exactly as a real peer would. store_lock_test.go proves the lock itself.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/testenv"
)

// interleaveAt installs the fault seam so run() executes at the ordinal'th
// matching fault call of the first process, then never again. Fault calls the
// hook makes through its own filesystem work are ignored, so a model step that
// reindexes cannot re-enter.
func interleaveAt(t *testing.T, match func(op, path string) bool, ordinal int, run func()) {
	t.Helper()
	seen, fired, busy := 0, false, false
	withFault(t, func(op, path string) error {
		if busy || fired || !match(op, path) {
			return nil
		}
		seen++
		if seen != ordinal {
			return nil
		}
		fired, busy = true, true
		run()
		busy = false
		return nil
	})
}

// otherProcessMayWrite reports whether the second process can take the store's
// mutation lock for dir. A real peer would wait memoryLockWait and then fail
// with ErrStoreLocked; here the same lock file is simply already held, so the
// probe is non-blocking. It releases the lock when it could take it.
func otherProcessMayWrite(t *testing.T, dir string) bool {
	t.Helper()
	root := memoryLockRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("other process: create lock root: %v", err)
	}
	release, err := filelock.TryAcquire(memoryLockPath(root, canonicalLockDir(dir)))
	if err != nil {
		return false
	}
	release()
	return true
}

// otherProcessSave publishes one update in the other process: read the target
// (its findSaveTarget), snapshot the preimage under the revision it read, write
// its next revision, reindex. That is the sequence Store.SaveWithOptions runs.
// It reports whether the lock allowed it to run at all.
func otherProcessSave(t *testing.T, dir, name, body string) bool {
	t.Helper()
	if !otherProcessMayWrite(t, dir) {
		return false
	}
	path := filepath.Join(dir, name+".md")
	existing, ok, err := loadMemory(path)
	if err != nil {
		t.Fatalf("other process: %s: %v", path, err)
	}
	if !ok {
		t.Fatalf("other process: %s is not readable", path)
	}
	next := existing
	next.Revision = existing.Revision + 1
	next.Body = body
	next.UpdatedAt = time.Now().UTC()
	if err := snapshotMemoryRevisionInDir(dir, path, existing); err != nil {
		t.Fatalf("other process: snapshot: %v", err)
	}
	if err := writeMemoryAtomic(path, []byte(render(next, name)), 0o644); err != nil {
		t.Fatalf("other process: write: %v", err)
	}
	if err := reindexIn(dir, name, next); err != nil {
		t.Fatalf("other process: index: %v", err)
	}
	return true
}

// otherProcessCreate publishes a new fact in the other process, the way the
// create branch of SaveWithOptions does when no target exists.
func otherProcessCreate(t *testing.T, dir string, m Memory) bool {
	t.Helper()
	if !otherProcessMayWrite(t, dir) {
		return false
	}
	now := time.Now().UTC()
	m.Name = slug(m.Name)
	m.ID = newMemoryID(m.Name, now)
	m.Revision = 1
	m.CreatedAt, m.UpdatedAt = now, now
	if m.Scope == "" {
		m.Scope = FactScopeProject
	}
	path := filepath.Join(dir, m.Name+".md")
	if err := writeMemoryAtomic(path, []byte(render(m, m.Name)), 0o644); err != nil {
		t.Fatalf("other process: write: %v", err)
	}
	if err := reindexIn(dir, m.Name, m); err != nil {
		t.Fatalf("other process: index: %v", err)
	}
	return true
}

func crossProcDirRead(dir string) func(op, path string) bool {
	return func(op, path string) bool { return op == "read-dir" && path == dir }
}

func crossProcIndexPublish(dir string) func(op, path string) bool {
	index := filepath.Join(dir, indexFile)
	return func(op, path string) bool { return op == "publish-index" && path == index }
}

func crossProcIndexRead(dir string) func(op, path string) bool {
	index := filepath.Join(dir, indexFile)
	return func(op, path string) bool { return op == "read-index" && path == index }
}

// TestAuditCrossProcessSubjectKeyStaysUnique is candidate DUPLICATE IDENTITY:
// validateSubjectKey promises one active value per (scope, subject), and the
// remember tool tells the model a second save is rejected. The check is a
// directory scan, so without exclusion two processes that each scan before
// either writes both pass; the mutation lock keeps the peer out of that window.
func TestAuditCrossProcessSubjectKeyStaysUnique(t *testing.T) {
	dir := testenv.TempDir(t)
	s := Store{Dir: dir}
	// The other process tries after this process's subject scan (which found no
	// holder) and before this process publishes.
	allowed := true
	interleaveAt(t, crossProcDirRead(dir), 3, func() {
		allowed = otherProcessCreate(t, dir, Memory{
			Name: "runner", SubjectKey: "project.package_manager", Body: "we use npm",
		})
	})
	if _, err := s.SaveWithOptions(Memory{
		Name: "pm", SubjectKey: "project.package_manager", Body: "we use pnpm",
	}, SaveOptions{}); err != nil {
		t.Fatalf("this process's save was refused: %v", err)
	}
	if allowed {
		t.Errorf("the other process wrote inside this process's subject scan; the mutation lock did not exclude it")
	}

	var holders []string
	all, err := s.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if NormalizeSubjectKey(m.SubjectKey) == "project.package_manager" {
			fact := m
			holders = append(holders, fact.ID+" "+fact.Name)
		}
	}
	if len(holders) != 1 {
		t.Errorf("%d active facts hold subject project.package_manager: %v; validateSubjectKey's contract is one", len(holders), holders)
	}
}

// TestAuditCrossProcessRenamePublishesTheNameItReports is candidate LOST RENAME:
// publishRename retires the old file, then publishes the staged one. A peer
// starting inside that window finds the id absent, assumes an interrupted
// rename, and deletes the staged copy; without a lock the first process then
// reports a save it never published. The lock keeps the recovery out.
func TestAuditCrossProcessRenamePublishesTheNameItReports(t *testing.T) {
	dir := testenv.TempDir(t)
	s := Store{Dir: dir}
	first, err := s.SaveWithOptions(Memory{Name: "alpha", Body: "one"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	id := first.Memory.ID
	if _, err := s.SaveWithOptions(Memory{ID: id, Name: "alpha", Body: "two"}, SaveOptions{}); err != nil {
		t.Fatal(err)
	}

	// Fault ordinal 1 of a rename: the retire has happened (alpha is a hidden
	// .retired-* now), the staged copy exists, and the index has not been
	// rewritten yet — exactly the window another process's findSaveTarget reads.
	allowed := true
	interleaveAt(t, crossProcIndexRead(dir), 1, func() {
		if !otherProcessMayWrite(t, dir) {
			allowed = false
			return
		}
		recovered, _, found, err := s.recoverInterruptedRename(id)
		if err != nil {
			t.Fatalf("other process: recovery: %v", err)
		}
		if !found {
			t.Fatal("other process: expected an interrupted rename to recover")
		}
		_ = recovered
	})
	result, err := s.SaveWithOptions(Memory{ID: id, Name: "beta", Body: "three"}, SaveOptions{})
	if err != nil {
		t.Fatalf("this process's rename failed: %v", err)
	}
	if allowed {
		t.Errorf("the other process recovered the interrupted rename while this process held the mutation lock")
	}

	if _, statErr := os.Stat(result.Path); statErr != nil {
		t.Errorf("rename reported success for %s but nothing is there: %v", result.Path, statErr)
	}
	active, ok, err := s.Read("beta")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || active.Body != "three" {
		t.Errorf("the save reported body %q under name beta; store has %+v", "three", active)
	}
}

// TestAuditCrossProcessIndexKeepsTheOtherProcessLine is candidate INDEX
// DIVERGENCE: reindexIn maps MEMORY.md through a read-then-rewrite, and
// flushIndexFrom drops every managed line the map does not carry. With the lock
// the peer cannot publish between the read and the flush, so every active fact
// still has its line afterwards.
func TestAuditCrossProcessIndexKeepsTheOtherProcessLine(t *testing.T) {
	dir := testenv.TempDir(t)
	s := Store{Dir: dir}
	first, err := s.SaveWithOptions(Memory{Name: "alpha", Body: "one"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	allowed := true
	interleaveAt(t, crossProcIndexPublish(dir), 1, func() {
		allowed = otherProcessCreate(t, dir, Memory{Name: "zeta", Description: "the other process's fact", Body: "z"})
	})
	if _, err := s.SaveWithOptions(Memory{ID: first.Memory.ID, Name: "alpha", Body: "two"}, SaveOptions{}); err != nil {
		t.Fatalf("this process's save failed: %v", err)
	}
	if allowed {
		t.Errorf("the other process published a fact inside this process's index rewrite")
	}

	index, err := os.ReadFile(filepath.Join(dir, indexFile))
	if err != nil {
		t.Fatal(err)
	}
	// The index lists every active fact, or the write failed: that is the
	// contract a stale read-then-rewrite breaks.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || entry.Name() == indexFile {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		if !strings.Contains(string(index), "("+name+".md)") {
			t.Errorf("MEMORY.md lost the active fact %q:\n%s", name, index)
		}
	}
}

// TestAuditCrossProcessUndoWithdrawsTheOtherProcessRevision is candidate
// WITHDRAWAL OVER A NEWER REVISION: undoPlainWrite restores the preimage this
// process read when its own index publication fails. Without exclusion the peer
// commits a revision just before that, which the withdrawal deletes and the
// counter rolls back. The lock keeps the peer out of that window.
func TestAuditCrossProcessUndoWithdrawsTheOtherProcessRevision(t *testing.T) {
	dir := testenv.TempDir(t)
	index := filepath.Join(dir, indexFile)
	s := Store{Dir: dir}
	first, err := s.SaveWithOptions(Memory{Name: "alpha", Body: "one"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	id := first.Memory.ID
	if _, err := s.SaveWithOptions(Memory{ID: id, Name: "alpha", Body: "two"}, SaveOptions{}); err != nil {
		t.Fatal(err)
	}

	// This process's index publication fails. The other process tries to commit
	// its own save just before it does; the lock must keep that revision out of
	// the withdrawal's preimage window.
	allowed := true
	injected, busy := false, false
	withFault(t, func(op, path string) error {
		if path != index || busy || op != "publish-index" {
			return nil
		}
		if !injected {
			injected = true
			busy = true
			allowed = otherProcessSave(t, dir, "alpha", "from the other process")
			busy = false
		}
		return &os.PathError{Op: "rename", Path: path, Err: os.ErrPermission}
	})
	if _, err := s.SaveWithOptions(Memory{ID: id, Name: "alpha", Body: "three"}, SaveOptions{}); err == nil {
		t.Fatal("expected this process's index publication to fail")
	}
	memoryFault = nil

	if allowed {
		t.Errorf("the other process committed a revision while this process held the mutation lock; the withdrawal can now roll it back")
	}
	active, ok, err := s.Read("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("alpha disappeared entirely")
	}
	if active.Body != "two" || active.Revision != 2 {
		t.Errorf("the withdrawal left revision %d body %q; the preimage it read was revision 2 body %q",
			active.Revision, active.Body, "two")
	}
}
