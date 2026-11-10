package sessionstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeSessions(t testing.TB, dir string, n int) {
	t.Helper()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		name := fmt.Sprintf("20261001-%06d-deepseek-flash", i)
		path := filepath.Join(dir, name+".jsonl")
		if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		meta := BranchMeta{ID: name, CreatedAt: base.Add(time.Duration(i) * time.Minute), UpdatedAt: base.Add(time.Duration(i) * time.Minute), Turns: 3, Preview: "hello " + name}
		raw, _ := json.Marshal(meta)
		if err := os.WriteFile(path+".meta", raw, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".events.jsonl"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func sessionPath(dir string, i int) string {
	return filepath.Join(dir, fmt.Sprintf("20261001-%06d-deepseek-flash.jsonl", i))
}

// countSidecarReads reports how many sidecars a listing opened.
func countSidecarReads(t *testing.T) (reads func() int) {
	t.Helper()
	var mu sync.Mutex
	n := 0
	branchMetaReadHook = func(string) { mu.Lock(); n++; mu.Unlock() }
	t.Cleanup(func() { branchMetaReadHook = nil })
	return func() int { mu.Lock(); defer mu.Unlock(); v := n; n = 0; return v }
}

func list(t *testing.T, dir string) []SessionOrderInfo {
	t.Helper()
	got, err := ListSessionOrder(dir)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAnUnchangedFolderIsListedWithoutOpeningASession(t *testing.T) {
	dir := testenv.TempDir(t)
	writeSessions(t, dir, 300)
	reads := countSidecarReads(t)

	first := list(t, dir)
	if got := reads(); got != 300 {
		t.Fatalf("the first listing read %d sidecars, want all 300", got)
	}
	second := list(t, dir)
	if got := reads(); got != 0 {
		t.Fatalf("an unchanged folder read %d sidecars, want none", got)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("the remembered listing differs from the one read from the files")
	}
}

func TestARestartedProcessAnswersFromTheIndexOnDisk(t *testing.T) {
	dir := testenv.TempDir(t)
	writeSessions(t, dir, 150)
	want := list(t, dir)
	reads := countSidecarReads(t)
	if _, err := os.Stat(filepath.Join(dir, listIndexName)); err != nil {
		t.Fatalf("no index was kept: %v", err)
	}
	got := list(t, dir)
	if n := reads(); n != 0 || !reflect.DeepEqual(want, got) {
		t.Fatalf("reads=%d equal=%v after a restart, want 0 reads and the same rows", n, reflect.DeepEqual(want, got))
	}
}

func TestOnlyWhatChangedIsReadAgain(t *testing.T) {
	dir := testenv.TempDir(t)
	writeSessions(t, dir, 200)
	list(t, dir)
	reads := countSidecarReads(t)

	// A rename is a sidecar write; a new turn is a transcript write; both change
	// one session's files and nothing else.
	path := sessionPath(dir, 7)
	meta, _, _ := LoadBranchMeta(path)
	reads()
	meta.CustomTitle = "renamed"
	raw, _ := json.Marshal(meta)
	if err := os.WriteFile(path+".meta", raw, 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path+".meta", later, later); err != nil {
		t.Fatal(err)
	}
	got := list(t, dir)
	if n := reads(); n != 1 {
		t.Fatalf("a rename re-read %d sidecars, want 1", n)
	}
	for _, s := range got {
		if s.Path == path && s.CustomTitle != "renamed" {
			t.Fatalf("the rename is not in the listing: %q", s.CustomTitle)
		}
	}
}

func TestANewAndARemovedSessionAreSeenAtOnce(t *testing.T) {
	dir := testenv.TempDir(t)
	writeSessions(t, dir, 120)
	list(t, dir)
	reads := countSidecarReads(t)
	if err := os.Remove(sessionPath(dir, 3)); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(dir, "20261002-000000-new.jsonl")
	if err := os.WriteFile(extra, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := list(t, dir)
	if n := reads(); n != 1 {
		t.Fatalf("one new session read %d sidecars, want 1", n)
	}
	seen := map[string]bool{}
	for _, s := range got {
		seen[s.Path] = true
	}
	if seen[sessionPath(dir, 3)] || !seen[extra] || len(got) != 120 {
		t.Fatalf("removed present=%v new present=%v rows=%d", seen[sessionPath(dir, 3)], seen[extra], len(got))
	}
}

func TestAPendingRemovalHidesASessionWithoutReadingIt(t *testing.T) {
	dir := testenv.TempDir(t)
	writeSessions(t, dir, 120)
	list(t, dir)
	reads := countSidecarReads(t)
	if err := MarkCleanupPending(sessionPath(dir, 9), "test"); err != nil {
		t.Fatal(err)
	}
	got := list(t, dir)
	for _, s := range got {
		if s.Path == sessionPath(dir, 9) {
			t.Fatal("a session with a removal pending is still listed")
		}
	}
	if n := reads(); n != 0 || len(got) != 119 {
		t.Fatalf("reads=%d rows=%d, want 0 and 119", n, len(got))
	}
}

func TestAnUnusableIndexIsRebuiltNotTrusted(t *testing.T) {
	dir := testenv.TempDir(t)
	writeSessions(t, dir, 120)
	want := list(t, dir)
	for _, bad := range []string{"not json", `{"format":999,"entries":{}}`, ""} {
		if err := os.WriteFile(filepath.Join(dir, listIndexName), []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		reads := countSidecarReads(t)
		got := list(t, dir)
		if n := reads(); n != 120 || !reflect.DeepEqual(want, got) {
			t.Fatalf("index %q: reads=%d equal=%v, want a full read with the same rows", bad, n, reflect.DeepEqual(want, got))
		}
	}
}

func TestTheIndexIsNotASessionAndASmallFolderKeepsNone(t *testing.T) {
	small := testenv.TempDir(t)
	writeSessions(t, small, 10)
	list(t, small)
	if _, err := os.Stat(filepath.Join(small, listIndexName)); err == nil {
		t.Fatal("a folder of ten sessions kept an index")
	}
	big := testenv.TempDir(t)
	writeSessions(t, big, 110)
	if got := list(t, big); len(got) != 110 {
		t.Fatalf("rows = %d", len(got))
	}
	if got := list(t, big); len(got) != 110 {
		t.Fatalf("rows with the index present = %d, the index was read as a session", len(got))
	}
}

func TestListingsRunningTogetherAgree(t *testing.T) {
	dir := testenv.TempDir(t)
	writeSessions(t, dir, 150)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if got, err := ListSessionOrder(dir); err != nil || len(got) != 150 {
				t.Errorf("rows=%d err=%v", len(got), err)
			}
		})
	}
	wg.Wait()
}

// An index written by another build must not be trusted once the row it holds
// has changed shape, so changing SessionOrderInfo means saying so by bumping
// listIndexFormat.
func TestListIndexFormatTracksTheRowShape(t *testing.T) {
	var shape strings.Builder
	for f := range reflect.TypeFor[SessionOrderInfo]().Fields() {
		shape.WriteString(f.Name + " " + f.Type.String() + ";")
	}
	const known = "Path string;CreatedAt time.Time;LastActivityAt time.Time;ModTime time.Time;Scope string;WorkspaceRoot string;TopicID string;TopicTitle string;CustomTitle string;Archived bool;Pinned bool;ManualRank float64;Unread bool;ViewedAt time.Time;Recovered bool;RecoveryReason string;RecoveryDigest string;ParentID string;RecoveryRootID string;Turns int;Preview string;SchemaVersion int;Revision int64;ContentDigest string;"
	if shape.String() != known {
		t.Fatalf("SessionOrderInfo changed shape: bump listIndexFormat (now %d) and update this table.\nnow:   %s\nknown: %s", listIndexFormat, shape.String(), known)
	}
}

// The picker's listing is built on the same order, so it gets the same saving.
func TestTheSessionListingItselfOpensNoSidecarsOnAnUnchangedFolder(t *testing.T) {
	dir := testenv.TempDir(t)
	writeSessions(t, dir, 130)
	first, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	reads := countSidecarReads(t)
	second, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n := reads(); n != 0 || !reflect.DeepEqual(first, second) {
		t.Fatalf("reads=%d equal=%v on an unchanged folder, want none and the same rows", n, reflect.DeepEqual(first, second))
	}
}
