package agent

// Crash-injection pins for each durable save boundary. A refactor that
// changes an outcome must update the matching test in the same commit.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// crashAt installs a fileutil.CrashPoint that panics the first time op is
// about to touch path. The returned restore must be deferred; the returned
// channel receives the op-path pair when the crash fires.
func crashAt(t *testing.T, op, path string) (fired <-chan struct{}, restore func()) {
	t.Helper()
	firedCh := make(chan struct{}, 1)
	prev := fileutil.CrashPoint
	fileutil.CrashPoint = func(firedOp, firedPath string) {
		if firedOp != op || firedPath != path {
			return
		}
		select {
		case firedCh <- struct{}{}:
		default:
		}
		panic(crashInjected{op: firedOp, path: firedPath})
	}
	return firedCh, func() { fileutil.CrashPoint = prev }
}

type crashInjected struct {
	op   string
	path string
}

func (c crashInjected) Error() string { return "crash injected at " + c.op + " (" + c.path + ")" }

// saveCrashing runs fn and converts an injected crash panic into a
// crashInjected error, so callers can assert the crash fired without the
// panic unwinding the test.
func saveCrashing(fn func()) (crash error) {
	defer func() {
		if r := recover(); r != nil {
			if ci, ok := r.(crashInjected); ok {
				crash = ci
				return
			}
			panic(r)
		}
	}()
	fn()
	return nil
}

func messageCount(t *testing.T, s *Session) int {
	t.Helper()
	return len(s.Snapshot())
}

// TestCrashAtWALAppendKeepsPreviousCheckpointUsable pins the first boundary:
// a crash before the WAL record lands leaves the previous checkpoint as the
// only forward progress. A reload observes the old transcript, and the next
// save replays the append without duplicating or losing messages.
// (Message counts include the leading system message.)
func TestCrashAtWALAppendKeepsPreviousCheckpointUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("system")
	s.Add(userMessage("first"))
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	s.Add(userMessage("second"))
	_, restore := crashAt(t, "wal-append", store.SessionEventLog(path))
	crash := saveCrashing(func() { _ = s.SaveSnapshot(path) })
	restore()
	if crash == nil {
		t.Fatal("save must crash at the wal-append boundary")
	}

	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reload after wal-append crash: %v", err)
	}
	if got := messageCount(t, reloaded); got != 2 {
		t.Fatalf("reload after wal-append crash = %d messages, want 2 (previous checkpoint)", got)
	}

	// The retry save must heal forward without duplicating events.
	reloaded.Add(userMessage("second"))
	if err := reloaded.SaveSnapshot(path); err != nil {
		t.Fatalf("retry save: %v", err)
	}
	final, err := LoadSession(path)
	if err != nil {
		t.Fatalf("final reload: %v", err)
	}
	if got := messageCount(t, final); got != 3 {
		t.Fatalf("final reload = %d messages, want 3", got)
	}
	if got := countEventLogRecords(t, path); got != 2 {
		t.Fatalf("event log = %d records after retry, want 2 (replace + append)", got)
	}
}

// TestCrashAtCheckpointWriteLeavesEventLogAuthoritative pins the WAL-first
// ordering on the full-rewrite path (first save, repairs, compactions): a
// crash after the WAL replace record landed but before the .jsonl checkpoint
// rename still exposes the full transcript on reload, because the event log
// is authoritative. The compatibility checkpoint never comes to exist.
func TestCrashAtCheckpointWriteLeavesEventLogAuthoritative(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("system")
	s.Add(userMessage("first"))
	s.Add(userMessage("second"))
	_, restore := crashAt(t, "session-checkpoint", path)
	crash := saveCrashing(func() { _ = s.SaveSnapshot(path) })
	restore()
	if crash == nil {
		t.Fatal("save must crash at the session-checkpoint boundary")
	}

	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("checkpoint must not exist after crash before its rename (err=%v)", err)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reload after checkpoint crash: %v", err)
	}
	if got := messageCount(t, reloaded); got != 3 {
		t.Fatalf("reload = %d messages, want 3 (event log authoritative)", got)
	}
	// The follow-up save publishes the missing checkpoint without duplicating
	// WAL history.
	if err := reloaded.SaveSnapshot(path); err != nil {
		t.Fatalf("follow-up save: %v", err)
	}
	if got := countEventLogRecords(t, path); got != 1 {
		t.Fatalf("event log = %d records, want 1 (no duplicate replace)", got)
	}
	checkpointBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if got := strings.Count(string(checkpointBytes), "\n"); got != 3 {
		t.Fatalf("checkpoint = %d lines, want 3", got)
	}
}

// TestCrashAtRevisionLedgerHealsOnNextSave pins the ledger-lag window: a
// crash after the transcript landed but before the revision ledger recorded
// the new digest leaves a stale ledger. The next same-content save must heal
// the ledger via the ledgerStale path instead of appending new events.
func TestCrashAtRevisionLedgerHealsOnNextSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("system")
	s.Add(userMessage("first"))
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	recordsBefore := countEventLogRecords(t, path)

	s.Add(userMessage("second"))
	_, restore := crashAt(t, "branch-meta", BranchMetaPath(path))
	crash := saveCrashing(func() { _ = s.SaveSnapshot(path) })
	restore()
	if crash == nil {
		t.Fatal("save must crash at the branch-meta boundary")
	}

	// Transcript and ledger have diverged: the event log replay observes three
	// messages while the ledger still stamps the two-message digest.
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reload after ledger crash: %v", err)
	}
	if got := messageCount(t, reloaded); got != 3 {
		t.Fatalf("reload = %d messages, want 3", got)
	}

	// A same-content save must heal the ledger without new WAL records.
	if err := reloaded.SaveSnapshot(path); err != nil {
		t.Fatalf("healing save: %v", err)
	}
	if got := countEventLogRecords(t, path); got != recordsBefore+1 {
		t.Fatalf("event log = %d records after healing save, want %d (no extra records)", got, recordsBefore+1)
	}
	healed, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reload after healing: %v", err)
	}
	if got := messageCount(t, healed); got != 3 {
		t.Fatalf("healed reload = %d messages, want 3", got)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("load healed branch meta: ok=%v err=%v", ok, err)
	}
	if meta.ContentDigest == "" {
		t.Fatal("healed ledger must stamp the current content digest")
	}
}

// TestCrashAtEventIndexKeepsSaveDurable pins the derived-index ordering: the
// event index is a pure accelerator, so a crash at its boundary loses nothing
// authoritative. A reload observes the new transcript and the next save
// succeeds without event-log duplication.
func TestCrashAtEventIndexKeepsSaveDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("system")
	s.Add(userMessage("first"))
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	s.Add(userMessage("second"))
	_, restore := crashAt(t, "event-index", store.SessionEventIndex(path))
	crash := saveCrashing(func() { _ = s.SaveSnapshot(path) })
	restore()
	if crash == nil {
		t.Fatal("save must crash at the event-index boundary")
	}

	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reload after event-index crash: %v", err)
	}
	if got := messageCount(t, reloaded); got != 3 {
		t.Fatalf("reload = %d messages, want 3 (index is derived)", got)
	}
	if err := reloaded.SaveSnapshot(path); err != nil {
		t.Fatalf("follow-up save: %v", err)
	}
	if got := countEventLogRecords(t, path); got != 2 {
		t.Fatalf("event log = %d records, want 2 (no duplication from index loss)", got)
	}
}

// TestSavedSessionSidecarSetBaseline pins the sidecar inventory a healthy
// first save produces. Every sidecar listed here is load-bearing for some
// reader; the refactor may shrink this set (with a migration story for each
// removed file) but must never grow it silently.
func TestSavedSessionSidecarSetBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("system")
	s.Add(userMessage("first"))
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	want := []string{
		filepath.Base(path),                        // compatibility checkpoint
		filepath.Base(store.SessionEventLog(path)), // authoritative event log
		filepath.Base(store.SessionEventIndex(path)),
		filepath.Base(store.SessionDisplayIndex(path)),
		filepath.Base(BranchMetaPath(path)),
	}
	found := map[string]bool{}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read session dir: %v", err)
	}
	for _, e := range entries {
		found[e.Name()] = true
	}
	for _, name := range want {
		if !found[name] {
			t.Errorf("missing expected sidecar %q after first save", name)
		}
	}
	for name := range found {
		if !slices.Contains(want, name) {
			// Legacy .lock sidecars still outlive a save; the refactor removes them.
			if strings.HasSuffix(name, ".lock") {
				continue
			}
			t.Errorf("unexpected extra sidecar %q after first save", name)
		}
	}
}

func userMessage(content string) provider.Message {
	return provider.Message{Role: provider.RoleUser, Content: content}
}

func countEventLogRecords(t *testing.T, sessionPath string) int {
	t.Helper()
	b, err := os.ReadFile(store.SessionEventLog(sessionPath))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read event log: %v", err)
	}
	count := 0
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
