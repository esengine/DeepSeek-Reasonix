package memory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// denyFactFile makes exactly one fact file unreadable, the fault a permission
// change, sandbox denial, or storage problem produces. The read is verified so
// a root user (or a platform where the mode does not deny) skips instead of
// asserting a state it did not create.
func denyFactFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, info.Mode().Perm()) })
	if _, err := os.ReadFile(path); !errors.Is(err, os.ErrPermission) {
		t.Skip("file permissions do not deny reads on this platform or for this user")
	}
}

// requireFactReadError asserts the typed identity, so callers can tell this
// state from absence without reading the sentence.
func requireFactReadError(t *testing.T, err error, path string) *FactReadError {
	t.Helper()
	var fact *FactReadError
	if !errors.As(err, &fact) || fact.Path != path || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("error = %v, want FactReadError for %q wrapping permission denied", err, path)
	}
	return fact
}

// unreadableFixture saves one readable and one unreadable fact in the same
// directory and returns the store plus both paths.
func unreadableFixture(t *testing.T) (Store, string, string) {
	t.Helper()
	s := Store{Dir: t.TempDir()}
	good, err := s.Save(Memory{Name: "good", Title: "Compiler flags", Description: "compiler flags are documented here", Body: "compiler flags: use -O2"})
	if err != nil {
		t.Fatal(err)
	}
	locked, err := s.Save(Memory{Name: "locked", Title: "Locked fact", Description: "locked compiler notes", Body: "locked compiler notes that cannot be read"})
	if err != nil {
		t.Fatal(err)
	}
	denyFactFile(t, locked)
	return s, good, locked
}

// TestAnUnreadableFactIsListedNotAbsent is the listing half of the defect: a
// fact file that exists but cannot be read must be named beside the readable
// facts, not dropped as if it had never been saved.
func TestAnUnreadableFactIsListedNotAbsent(t *testing.T) {
	s, good, locked := unreadableFixture(t)

	report, err := s.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(report.Facts) != 1 || report.Facts[0].Name != "good" || report.Facts[0].Body != "compiler flags: use -O2" {
		t.Fatalf("readable facts = %+v, want only %q", report.Facts, good)
	}
	if len(report.Unreadable) != 1 {
		t.Fatalf("unreadable = %+v, want the one file at %s", report.Unreadable, locked)
	}
	requireFactReadError(t, report.Unreadable[0], locked)
	if report.Unreadable[0].Name() != "locked" {
		t.Fatalf("unreadable name = %q, want the filename stem %q", report.Unreadable[0].Name(), "locked")
	}

	// ListAll keeps its old shape for callers that only need the facts, but it
	// must still answer "one readable fact, no error": one bad file may not
	// hide the rest.
	if list, err := s.ListAll(); err != nil || len(list) != 1 || list[0].Name != "good" {
		t.Fatalf("ListAll = %+v, %v; want the readable fact only and no error", list, err)
	}

	// The read-by-name path must not answer "not found" for a file that is
	// right there.
	if _, ok, err := s.Read("locked"); ok || err == nil {
		t.Fatalf("Read(locked) = ok %v, error %v; want the read failure", ok, err)
	} else {
		requireFactReadError(t, err, locked)
	}
	// Reading another fact is unaffected.
	if got, ok, err := s.Read("good"); err != nil || !ok || got.Body != "compiler flags: use -O2" {
		t.Fatalf("Read(good) = %+v, %v, %v", got, ok, err)
	}
}

// TestSaveByNameRefusesAnUnreadableFact is the write half: a save by name must
// stop before it rewrites a file it could not read, because the new id and
// revision 1 would erase the old identity and its history.
func TestSaveByNameRefusesAnUnreadableFact(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	first, err := s.SaveWithOptions(Memory{Name: "fact", Body: "v1"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	path := first.Path
	before := mustReadString(t, path)
	indexBefore := mustReadString(t, filepath.Join(s.Dir, indexFile))
	denyFactFile(t, path)

	_, err = s.Save(Memory{Name: "fact", Body: "v2"})
	requireFactReadError(t, err, path)

	// The failure left the file byte-identical: same id, same revision, same
	// body. Restore the mode so the assertion can read the bytes back.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mustReadString(t, path); got != before {
		t.Fatalf("save rewrote the unreadable fact:\n%s\nwant:\n%s", got, before)
	}
	if got := mustReadString(t, filepath.Join(s.Dir, indexFile)); got != indexBefore {
		t.Fatalf("save changed the index:\n%s\nwant:\n%s", got, indexBefore)
	}
	if _, statErr := os.Stat(filepath.Join(s.Dir, ".revisions", first.Memory.ID, "000000002.md")); !os.IsNotExist(statErr) {
		t.Fatalf("save filed a revision snapshot over an unreadable fact: %v", statErr)
	}

	// Once the file can be read again, the same save is an ordinary revision.
	if _, err := s.Save(Memory{Name: "fact", Body: "v2"}); err != nil {
		t.Fatalf("save after the read fault was repaired: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, ".revisions", first.Memory.ID, "000000001.md")); err != nil {
		t.Fatalf("repaired save filed no snapshot of the revision it replaced: %v", err)
	}
}

// TestMissingFactStillReadsAsAbsent is the control beside the fix: a file that
// is genuinely gone stays absent, with no error and no unreadable entry.
func TestMissingFactStillReadsAsAbsent(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Save(Memory{Name: "present", Body: "present body"}); err != nil {
		t.Fatal(err)
	}
	report, err := s.ListActive()
	if err != nil || len(report.Facts) != 1 || len(report.Unreadable) != 0 {
		t.Fatalf("ListActive = %+v, %v", report, err)
	}
	if _, ok, err := s.Read("missing"); ok || err != nil {
		t.Fatalf("Read(missing) = ok %v, error %v; want absent with no error", ok, err)
	}
	if _, ok, err := s.Read("present"); !ok || err != nil {
		t.Fatalf("Read(present) = ok %v, error %v", ok, err)
	}
}

// TestAnUnrelatedUnreadableFactDoesNotBlockAName is the fail-closed boundary:
// only the file the caller named is reported; another unreadable fact must not
// make a different fact unreadable or a save impossible.
func TestAnUnrelatedUnreadableFactDoesNotBlockAName(t *testing.T) {
	s, _, _ := unreadableFixture(t)
	if _, ok, err := s.Read("absent"); ok || err != nil {
		t.Fatalf("Read(absent) = ok %v, error %v; the unreadable sibling must not answer for it", ok, err)
	}
	if _, err := s.Save(Memory{Name: "fresh", Body: "fresh body"}); err != nil {
		t.Fatalf("save beside an unreadable unrelated fact: %v", err)
	}
	if got, ok, err := s.Read("fresh"); err != nil || !ok || got.Body != "fresh body" {
		t.Fatalf("Read(fresh) = %+v, %v, %v", got, ok, err)
	}
}

// TestRecallKeepsItsBlockAndNamesTheUnreadableFact is the recall control: one
// unreadable fact must not degrade the turn, and the audit must name it as
// unreadable instead of leaving it looking absent.
func TestRecallKeepsItsBlockAndNamesTheUnreadableFact(t *testing.T) {
	opts := Options{CWD: t.TempDir(), UserDir: t.TempDir()}
	s := StoreFor(opts.UserDir, opts.CWD)
	if _, err := s.Save(Memory{Name: "good", Title: "Compiler flags", Description: "compiler flags are documented here", Body: "compiler flags: use -O2"}); err != nil {
		t.Fatal(err)
	}
	locked, err := s.Save(Memory{Name: "locked", Title: "Locked compiler notes", Description: "compiler flags for the locked toolchain", Body: "compiler flags for the locked toolchain"})
	if err != nil {
		t.Fatal(err)
	}
	denyFactFile(t, locked)

	const query = "compiler flags"
	set := Load(opts)
	if set.StoreError != nil {
		t.Fatalf("one unreadable fact degraded the whole store: %v", set.StoreError)
	}
	for _, result := range []RecallResult{set.AutoRecall(query, RecallOptions{}), AutoRecall(s, query, RecallOptions{})} {
		if result.StoreError != nil {
			t.Fatalf("recall failed the turn for one unreadable fact: %v", result.StoreError)
		}
		if strings.Contains(result.Block(), "could not be read") {
			t.Fatalf("recall used the whole-store failure sentence for one fact:\n%s", result.Block())
		}
		if len(result.Hits) == 0 || !strings.Contains(result.Block(), "compiler flags: use -O2") {
			t.Fatalf("recall produced no block for the readable fact:\n%s", result.Block())
		}
		if len(result.Unreadable) != 1 {
			t.Fatalf("recall unreadable = %+v, want the one file at %s", result.Unreadable, locked)
		}
		requireFactReadError(t, result.Unreadable[0], locked)
		if result.Unreadable[0].Name() != "locked" {
			t.Fatalf("recall unreadable name = %q, want %q", result.Unreadable[0].Name(), "locked")
		}
	}
}

// TestRecallWithOnlyUnreadableFactsSaysUnreadable is the edge beside the
// control: nothing readable is left, so the existing whole-store sentence is
// the honest answer — "the store is empty" would be the absence this fix rules
// out.
func TestRecallWithOnlyUnreadableFactsSaysUnreadable(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	locked, err := s.Save(Memory{Name: "only", Body: "the only fact"})
	if err != nil {
		t.Fatal(err)
	}
	denyFactFile(t, locked)
	result := AutoRecall(s, "the only fact", RecallOptions{})
	requireFactReadError(t, result.StoreError, locked)
	if result.SuppressionCode != SuppressUnreadable {
		t.Fatalf("suppression = %q, want %q (not empty-store)", result.SuppressionCode, SuppressUnreadable)
	}
	if got, want := result.Block(), "Background memory could not be read. Saved facts may exist; recall is unavailable for this turn."; got != want {
		t.Fatalf("block = %q, want the existing sentence %q", got, want)
	}
}

// TestRecallDirectoryFailureKeepsItsSentence is the stated control: a whole
// directory that cannot be listed still degrades with its existing sentence,
// and that condition is not reported as a per-fact unreadable file.
func TestRecallDirectoryFailureKeepsItsSentence(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Save(Memory{Name: "fact", Body: "compiler flags"}); err != nil {
		t.Fatal(err)
	}
	denyMemoryDirectory(t, s.Dir)
	result := AutoRecall(s, "compiler flags", RecallOptions{})
	var directory *DirectoryReadError
	if !errors.As(result.StoreError, &directory) || directory.Path != s.Dir {
		t.Fatalf("store error = %v, want DirectoryReadError for %q", result.StoreError, s.Dir)
	}
	if result.SuppressionCode != SuppressUnreadable {
		t.Fatalf("suppression = %q, want %q", result.SuppressionCode, SuppressUnreadable)
	}
	if got, want := result.Block(), "Background memory could not be read. Saved facts may exist; recall is unavailable for this turn."; got != want {
		t.Fatalf("block = %q, want the existing sentence %q", got, want)
	}
	if len(result.Unreadable) != 0 {
		t.Fatalf("a directory failure reported per-fact unreadable state: %+v", result.Unreadable)
	}
}

// TestTheMemoryToolNamesUnreadableFacts covers the model-facing surface: list
// and search must carry the unreadable file beside their answer, and read must
// not answer "not found" for it.
func TestTheMemoryToolNamesUnreadableFacts(t *testing.T) {
	s, _, locked := unreadableFixture(t)
	tool := NewRecallTool(s)
	run := func(args string) (string, error) {
		return tool.Execute(context.Background(), json.RawMessage(args))
	}

	if out, err := run(`{"operation":"list"}`); err != nil {
		t.Fatalf("list: %v", err)
	} else if !strings.Contains(out, "good") || !strings.Contains(out, "Unreadable saved memories") || !strings.Contains(out, locked) {
		t.Fatalf("list did not name the unreadable fact:\n%s", out)
	}

	if out, err := run(`{"operation":"search","query":"compiler flags"}`); err != nil {
		t.Fatalf("search: %v", err)
	} else if !strings.Contains(out, "good") || !strings.Contains(out, "Unreadable saved memories") || !strings.Contains(out, locked) {
		t.Fatalf("search did not name the unreadable fact:\n%s", out)
	}

	out, err := run(`{"operation":"read","name":"locked"}`)
	requireFactReadError(t, err, locked)
	if strings.Contains(out, "not found") || strings.Contains(err.Error(), "not found") {
		t.Fatalf("read answered %q / %v, want the read failure", out, err)
	}
	if !strings.Contains(err.Error(), "read memory fact") {
		t.Fatalf("read error = %v, want the store's fact read error", err)
	}

	// A missing fact keeps its old answer.
	if _, err := run(`{"operation":"read","name":"missing"}`); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("read(missing) error = %v, want not found", err)
	}
}
