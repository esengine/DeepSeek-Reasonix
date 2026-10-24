package memory

// Cross-process tests for the revision snapshot: the "other process" runs the
// primitives Save runs through the memoryFault seam, at an exact point inside
// the first process's sequence — no sleeps, no second process.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// lockFreeProcessSave publishes one update the way a writer that does not take
// the store lock would, such as an older build: read the target, snapshot the
// preimage under the revision it read, write the next revision, reindex. The
// snapshot's own check is what protects the revision slots against it.
func lockFreeProcessSave(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name+".md")
	existing, ok, readErr := loadMemory(path)
	if readErr != nil || !ok {
		t.Fatalf("other process: %s: %v", path, readErr)
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
}

// otherProcessArchiveAndRecreate retires name into .archive and publishes a new
// fact under the same name with a different id: the archive-then-create case.
func otherProcessArchiveAndRecreate(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name+".md")
	if _, ok, readErr := loadMemory(path); readErr != nil || !ok {
		t.Fatalf("other process: %s is not readable: %v", path, readErr)
	}
	archive := filepath.Join(dir, ".archive")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(archive, "recreated-"+name+".md")); err != nil {
		t.Fatalf("other process: archive: %v", err)
	}
	now := time.Now().UTC()
	reborn := Memory{Name: name, Body: body, ID: newMemoryID(name, now), Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := writeMemoryAtomic(path, []byte(render(reborn, name)), 0o644); err != nil {
		t.Fatalf("other process: write: %v", err)
	}
	if err := reindexIn(dir, name, reborn); err != nil {
		t.Fatalf("other process: index: %v", err)
	}
}

// TestAuditCrossProcessSnapshotIsTheRevisionItNames covers the mislabel: the
// snapshot re-reads the fact file and stores whatever bytes are there under the
// revision the earlier findSaveTarget read produced, so a newer revision must
// be refused instead of filed as revision N.
func TestAuditCrossProcessSnapshotIsTheRevisionItNames(t *testing.T) {
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

	// The other process reads alpha (revision 2) at the same time this process
	// does, and publishes its own revision 3 before this process snapshots.
	interleaveAt(t, crossProcDirRead(dir), 2, func() {
		lockFreeProcessSave(t, dir, "alpha", "from the other process")
	})
	_, err = s.SaveWithOptions(Memory{ID: id, Name: "alpha", Body: "three"}, SaveOptions{})
	if err == nil || !strings.Contains(err.Error(), "memory revision conflict: expected 2, found 3") {
		t.Fatalf("stale save error = %v, want revision conflict expected 2, found 3", err)
	}

	// The refused save filed nothing: no revision 3 slot, and revision 2 still
	// holds the bytes the other process filed for it.
	if _, statErr := os.Stat(revisionSlot(dir, id, 3)); !os.IsNotExist(statErr) {
		t.Fatalf("refused save filed revision 3: stat error %v\n%s", statErr, revisionSlots(t, dir, id))
	}
	snapshot, ok, readErr := loadMemory(revisionSlot(dir, id, 2))
	if readErr != nil || !ok {
		t.Fatalf("revision 2 snapshot is missing at %s: %v", revisionSlot(dir, id, 2), readErr)
	}
	if snapshot.Revision != 2 || snapshot.Body != "two" {
		raw, _ := os.ReadFile(revisionSlot(dir, id, 2))
		t.Errorf("revision 2 snapshot holds revision %d, body %q\nfile bytes:\n%s\nwant revision 2, body %q",
			snapshot.Revision, snapshot.Body, raw, "two")
	}
	if restored, err := s.Restore(id, 2); err != nil {
		t.Errorf("revision 2 is no longer recoverable: %v", err)
	} else if restored.Memory.Body != "two" {
		t.Errorf("restore revision 2 returned body %q, want %q", restored.Memory.Body, "two")
	}
}

// TestAuditCrossProcessRestoreReturnsTheBytesItNames is the user-visible end of
// the same defect: revision 3's slot must hold the bytes revision 3 was written
// with, so a restore of revision 3 cannot return a body revision 3 never had.
func TestAuditCrossProcessRestoreReturnsTheBytesItNames(t *testing.T) {
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
	interleaveAt(t, crossProcDirRead(dir), 2, func() {
		lockFreeProcessSave(t, dir, "alpha", "from the other process")
	})
	if _, err := s.SaveWithOptions(Memory{ID: id, Name: "alpha", Body: "three"}, SaveOptions{}); err == nil ||
		!strings.Contains(err.Error(), "memory revision conflict: expected 2, found 3") {
		t.Fatalf("stale save error = %v, want revision conflict expected 2, found 3", err)
	}
	// A later, ordinary save: this is what snapshots revision 3.
	if _, err := s.SaveWithOptions(Memory{ID: id, Name: "alpha", Body: "four"}, SaveOptions{}); err != nil {
		t.Fatalf("later save failed: %v", err)
	}

	restored, err := s.Restore(id, 3)
	if err != nil {
		t.Fatalf("restore revision 3: %v\n%s", err, revisionSlots(t, dir, id))
	}
	if restored.Memory.Body != "from the other process" {
		t.Errorf("restore revision 3 returned body %q; revision 3 was written as %q\n%s",
			restored.Memory.Body, "from the other process", revisionSlots(t, dir, id))
	}
	assertSlotsHoldTheirRevision(t, dir, id)
}

// TestSnapshotRefusesARecreatedName covers the same window when the other
// process archived the fact and created a different one under the same file
// name: the bytes there no longer carry this id at this revision, so filing
// them as revision 2 would invent history for the old fact.
func TestSnapshotRefusesARecreatedName(t *testing.T) {
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

	alpha := filepath.Join(dir, "alpha.md")
	interleaveAt(t, func(op, path string) bool { return op == "read-file" && path == alpha }, 1, func() {
		otherProcessArchiveAndRecreate(t, dir, "alpha", "reborn")
	})
	_, err = s.SaveWithOptions(Memory{ID: id, Name: "alpha", Body: "three"}, SaveOptions{})
	if err == nil || !strings.Contains(err.Error(), "memory revision conflict: expected 2, found 1") {
		t.Fatalf("save over a recreated name error = %v, want revision conflict expected 2, found 1", err)
	}

	// Nothing was filed for the old id at revision 2, and the new fact stands.
	if _, statErr := os.Stat(revisionSlot(dir, id, 2)); !os.IsNotExist(statErr) {
		t.Fatalf("refused save filed revision 2: stat error %v\n%s", statErr, revisionSlots(t, dir, id))
	}
	active, ok, readErr := loadMemory(alpha)
	if readErr != nil || !ok || active.ID == id || active.Body != "reborn" {
		t.Fatalf("recreated fact = %+v (ok %v), want body %q under a new id", active, ok, "reborn")
	}
}

// TestSnapshotsHoldTheRevisionTheyName is the control: an ordinary save and
// restore sequence over several revisions keeps every snapshot exactly at the
// revision its file name claims.
func TestSnapshotsHoldTheRevisionTheyName(t *testing.T) {
	dir := testenv.TempDir(t)
	s := Store{Dir: dir}
	bodies := []string{"one", "two", "three"}
	first, err := s.SaveWithOptions(Memory{Name: "alpha", Body: bodies[0]}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	id := first.Memory.ID
	for _, body := range bodies[1:] {
		if _, err := s.SaveWithOptions(Memory{ID: id, Name: "alpha", Body: body}, SaveOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	for i, body := range bodies[:len(bodies)-1] {
		m, ok, readErr := loadMemory(revisionSlot(dir, id, i+1))
		if readErr != nil || !ok {
			t.Fatalf("revision %d snapshot is missing: %v", i+1, readErr)
		}
		if m.ID != id || m.Revision != i+1 || m.Body != body {
			t.Errorf("revision %d snapshot = id %q revision %d body %q, want id %q revision %d body %q",
				i+1, m.ID, m.Revision, m.Body, id, i+1, body)
		}
	}
	for i, body := range bodies {
		restored, err := s.Restore(id, i+1)
		if err != nil {
			t.Fatalf("restore revision %d: %v", i+1, err)
		}
		if restored.Memory.Body != body {
			t.Errorf("restore revision %d returned body %q, want %q", i+1, restored.Memory.Body, body)
		}
	}
	assertSlotsHoldTheirRevision(t, dir, id)
}

// revisionSlot is the one file a revision is snapshotted at.
func revisionSlot(dir, id string, revision int) string {
	return filepath.Join(dir, ".revisions", id, fmt.Sprintf("%09d.md", revision))
}

// assertSlotsHoldTheirRevision guards the invariant the mislabel broke: every
// snapshot in .revisions/<id> carries the revision its file name claims.
func assertSlotsHoldTheirRevision(t *testing.T, dir, id string) {
	t.Helper()
	revisionDir := filepath.Join(dir, ".revisions", id)
	entries, err := os.ReadDir(revisionDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		m, ok, readErr := loadMemory(filepath.Join(revisionDir, e.Name()))
		if readErr != nil || !ok {
			t.Errorf("%s is not readable: %v", e.Name(), readErr)
			continue
		}
		var revision int
		if _, err := fmt.Sscanf(e.Name(), "%09d.md", &revision); err != nil {
			t.Errorf("unexpected snapshot name %q: %v", e.Name(), err)
			continue
		}
		if m.Revision != revision {
			t.Errorf("%s holds revision %d\n%s", e.Name(), m.Revision, revisionSlots(t, dir, id))
		}
	}
}

// revisionSlots lists what .revisions/<id> holds, for failure messages.
func revisionSlots(t *testing.T, dir, id string) string {
	t.Helper()
	dir = filepath.Join(dir, ".revisions", id)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "revision dir: " + err.Error()
	}
	var b strings.Builder
	for _, e := range entries {
		m, ok, readErr := loadMemory(filepath.Join(dir, e.Name()))
		if readErr != nil || !ok {
			continue
		}
		b.WriteString("  " + e.Name() + ": revision " + strconv.Itoa(m.Revision) + " body " + strconv.Quote(m.Body) + "\n")
	}
	return "revision dir:\n" + b.String()
}
