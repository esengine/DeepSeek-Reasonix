package sessionstore

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
)

func saveUnreadFixture(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func listedUnread(t *testing.T, dir, path string) bool {
	t.Helper()
	listed, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, si := range listed {
		if si.Path == path {
			return si.Unread
		}
	}
	t.Fatalf("%s is not listed", path)
	return false
}

func TestSessionUnreadFollowsFinishedAndViewed(t *testing.T) {
	dir := t.TempDir()
	path := saveUnreadFixture(t, dir, "20260101-000001-m.jsonl")
	if listedUnread(t, dir, path) {
		t.Fatal("a session nobody finished a turn in is unread")
	}
	base := time.Now().UTC()
	if err := RecordSessionFinished(path, base, true); err != nil {
		t.Fatal(err)
	}
	if !listedUnread(t, dir, path) {
		t.Fatal("a finished turn nobody looked at is not unread")
	}
	if err := MarkSessionViewed(path, base.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if listedUnread(t, dir, path) {
		t.Fatal("a viewed session is still unread")
	}
	if err := RecordSessionFinished(path, base.Add(2*time.Second), true); err != nil {
		t.Fatal(err)
	}
	if !listedUnread(t, dir, path) {
		t.Fatal("a turn finishing after the last view did not mark it unread again")
	}
}

func TestSessionUnreadAbsentFieldsMeanRead(t *testing.T) {
	dir := t.TempDir()
	path := saveUnreadFixture(t, dir, "20260101-000001-m.jsonl")
	if listedUnread(t, dir, path) {
		t.Fatal("a session without the fields must read as seen")
	}
	b, err := os.ReadFile(BranchMetaPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "finished_at") || strings.Contains(string(b), "viewed_at") {
		t.Fatalf("untouched metadata grew the new fields:\n%s", b)
	}
	legacy := `{"id":"20260101-000001-m","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","turns":1,"preview":"hello","schema_version":2,"future_field":7}`
	if err := os.WriteFile(BranchMetaPath(path), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if listedUnread(t, dir, path) {
		t.Fatal("an older sidecar must read as seen")
	}
}

func TestSessionViewedWithoutUnreadWritesNothing(t *testing.T) {
	dir := t.TempDir()
	path := saveUnreadFixture(t, dir, "20260101-000001-m.jsonl")
	before, err := os.ReadFile(BranchMetaPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkSessionViewed(path, time.Now()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(BranchMetaPath(path))
	if err != nil || string(before) != string(after) {
		t.Fatalf("opening a read session rewrote its sidecar (err=%v)", err)
	}
	bare := filepath.Join(dir, "20260101-000002-m.jsonl")
	if err := MarkSessionViewed(bare, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(BranchMetaPath(bare)); !os.IsNotExist(err) {
		t.Fatalf("marking a session with no sidecar created one (err=%v)", err)
	}
}

func TestSessionFinishedSkipsATranscriptThatWasNeverWritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000009-m.jsonl")
	if err := RecordSessionFinished(path, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(BranchMetaPath(path)); !os.IsNotExist(err) {
		t.Fatalf("a sidecar appeared for a session with no transcript (err=%v)", err)
	}
}

func TestSessionFinishedByThePersonDoesNotLeaveItUnread(t *testing.T) {
	dir := t.TempDir()
	path := saveUnreadFixture(t, dir, "20260101-000001-m.jsonl")
	if err := RecordSessionFinished(path, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	if listedUnread(t, dir, path) {
		t.Fatal("a turn the person ended themselves was left unread")
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || meta.FinishedAt.IsZero() {
		t.Fatalf("the end was not recorded: %+v ok=%v err=%v", meta, ok, err)
	}
}

func TestSessionUnreadSurvivesAClockThatRanBackwards(t *testing.T) {
	dir := t.TempDir()
	path := saveUnreadFixture(t, dir, "20260101-000001-m.jsonl")
	now := time.Now().UTC()
	if err := RecordSessionFinished(path, now, true); err != nil {
		t.Fatal(err)
	}
	if err := MarkSessionViewed(path, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if listedUnread(t, dir, path) {
		t.Fatal("a view stamped before the finish left the session unread")
	}
	if err := RecordSessionFinished(path, now.Add(-2*time.Hour), true); err != nil {
		t.Fatal(err)
	}
	if !listedUnread(t, dir, path) {
		t.Fatal("a finish stamped before the last view was lost")
	}
}

func TestSessionUnreadDoesNotReorderTheList(t *testing.T) {
	dir := t.TempDir()
	older := saveUnreadFixture(t, dir, "20260101-000001-m.jsonl")
	newer := saveUnreadFixture(t, dir, "20260101-000002-m.jsonl")
	before, err := ListSessions(dir)
	if err != nil || len(before) != 2 {
		t.Fatalf("list: %v %d", err, len(before))
	}
	if err := RecordSessionFinished(older, time.Now().Add(time.Hour), true); err != nil {
		t.Fatal(err)
	}
	if err := MarkSessionViewed(older, time.Now().Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	after, err := ListSessions(dir)
	if err != nil || len(after) != 2 {
		t.Fatalf("list: %v %d", err, len(after))
	}
	for i := range before {
		if before[i].Path != after[i].Path || !before[i].LastActivityAt.Equal(after[i].LastActivityAt) {
			t.Fatalf("recording unread moved a session: before=%v after=%v", before[i], after[i])
		}
	}
	_ = newer
}

func TestSessionUnreadSurvivesArchiveRenameAndAutosave(t *testing.T) {
	dir := t.TempDir()
	path := saveUnreadFixture(t, dir, "20260101-000001-m.jsonl")
	if err := RecordSessionFinished(path, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if err := RenameSession(path, "named"); err != nil {
		t.Fatal(err)
	}
	if err := SetSessionArchived(path, true); err != nil {
		t.Fatal(err)
	}
	if err := SetSessionArchived(path, false); err != nil {
		t.Fatal(err)
	}
	if err := UpdateSessionMeta(path, "m", "hello", 2, true); err != nil {
		t.Fatal(err)
	}
	if !listedUnread(t, dir, path) {
		t.Fatal("rename, archive or autosave dropped the unread mark")
	}
}

func TestSessionViewedAndFinishedRaceLeaveOneConsistentAnswer(t *testing.T) {
	dir := t.TempDir()
	path := saveUnreadFixture(t, dir, "20260101-000001-m.jsonl")
	if err := RecordSessionFinished(path, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range 8 {
		wg.Go(func() { errs <- MarkSessionViewed(path, time.Now()) })
		wg.Go(func() { errs <- UpdateSessionMeta(path, "m", "hello", 2, true) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if listedUnread(t, dir, path) {
		t.Fatal("concurrent views left the session unread")
	}
}

func TestSessionViewedWorksWhileAnotherWindowHoldsTheSession(t *testing.T) {
	dir := t.TempDir()
	path := saveUnreadFixture(t, dir, "20260101-000001-m.jsonl")
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if err := RecordSessionFinished(path, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if err := MarkSessionViewed(path, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if listedUnread(t, dir, path) {
		t.Fatal("a leased session could not be marked viewed")
	}
}

func TestSessionViewedToleratesASidecarCaughtMidReplace(t *testing.T) {
	dir := t.TempDir()
	path := saveUnreadFixture(t, dir, "20260101-000001-m.jsonl")
	if err := RecordSessionFinished(path, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(BranchMetaPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BranchMetaPath(path), good[:len(good)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	healed := make(chan struct{})
	go func() {
		defer close(healed)
		time.Sleep(30 * time.Millisecond)
		_ = os.WriteFile(BranchMetaPath(path), good, 0o600)
	}()
	err = MarkSessionViewed(path, time.Now().Add(time.Second))
	<-healed
	if err != nil {
		t.Fatalf("a view that met a torn sidecar failed instead of waiting it out: %v", err)
	}
	if listedUnread(t, dir, path) {
		t.Fatal("the session is still unread after the view")
	}
}
