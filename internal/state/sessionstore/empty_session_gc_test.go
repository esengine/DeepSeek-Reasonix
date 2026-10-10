package sessionstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func writeAgedSession(t *testing.T, dir, name, body string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-age)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	return path
}

const (
	toolOnlyTranscript = "{\"role\":\"assistant\"}\n{\"role\":\"tool\",\"content\":\"ls\",\"tool_call_id\":\"tc-1\"}\n"
	userTranscript     = "{\"role\":\"user\",\"content\":\"hi\"}\n{\"role\":\"assistant\",\"content\":\"hello\"}\n"
)

func TestReclaimableEmptySessions(t *testing.T) {
	dir := testenv.TempDir(t)
	week := 7 * 24 * time.Hour

	// The v1 /new rotation and aborted forks: no user message ever reached them.
	empty := writeAgedSession(t, dir, "code-Reasonix__archive_202605201123.jsonl", toolOnlyTranscript, week)
	blank := writeAgedSession(t, dir, "20260101-000000.000000000-model.jsonl", "", week)
	writeAgedSession(t, dir, "kept-user.jsonl", userTranscript, week)
	writeAgedSession(t, dir, "subagent-sub-k-202605171512.jsonl", toolOnlyTranscript, week)
	writeAgedSession(t, dir, "torn.jsonl", "{\"role\":\"assistant\"", week)
	writeAgedSession(t, dir, "fresh.jsonl", toolOnlyTranscript, time.Minute)

	got, err := ReclaimableEmptySessions(dir, time.Now(), EmptySessionGracePeriod)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{blank, empty}
	if len(got) != len(want) {
		t.Fatalf("reclaimable = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reclaimable[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// A sidecar count may spare a transcript but never authorizes deleting one, so
// a stale "0 turns" must still be re-proven against the content.
func TestReclaimableEmptySessionsHonorsMetaTurns(t *testing.T) {
	dir := testenv.TempDir(t)
	path := writeAgedSession(t, dir, "counted.jsonl", toolOnlyTranscript, 7*24*time.Hour)
	if err := saveBranchMeta(path, BranchMeta{Turns: 3}, false); err != nil {
		t.Fatal(err)
	}
	got, err := ReclaimableEmptySessions(dir, time.Now(), EmptySessionGracePeriod)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("reclaimable = %v, want none", got)
	}
}

func TestReconcileCleanupPendingReclaimsEmptySessions(t *testing.T) {
	dir := testenv.TempDir(t)
	empty := writeAgedSession(t, dir, "empty.jsonl", toolOnlyTranscript, 7*24*time.Hour)
	writeAgedSession(t, dir, "kept.jsonl", userTranscript, 7*24*time.Hour)

	var disposed []CleanupPendingInfo
	if err := ReconcileCleanupPending(dir, func(item CleanupPendingInfo) error {
		disposed = append(disposed, item)
		return os.Remove(item.SessionPath)
	}); err != nil {
		t.Fatal(err)
	}
	if len(disposed) != 1 || disposed[0].SessionPath != empty {
		t.Fatalf("disposed = %v, want %s", disposed, empty)
	}
	// Hosts route on the operation: desktop trashes a delete instead of unlinking.
	if disposed[0].Meta.Operation != "delete" {
		t.Errorf("operation = %q, want delete", disposed[0].Meta.Operation)
	}
	if _, err := os.Stat(filepath.Join(dir, "kept.jsonl")); err != nil {
		t.Errorf("session with a user turn was removed: %v", err)
	}
}

func TestReconcileSessionDirDeletesUnderTheRemovalGuard(t *testing.T) {
	dir := testenv.TempDir(t)
	path := writeAgedSession(t, dir, "empty.jsonl", toolOnlyTranscript, 7*24*time.Hour)
	var heldDuring bool
	err := reconcileSessionDir(dir, func(info CleanupPendingInfo) error {
		if lease, err := TryAcquireSessionLease(info.SessionPath); err == nil {
			lease.Release()
		} else {
			heldDuring = true
		}
		return os.Remove(info.SessionPath)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !heldDuring {
		t.Fatal("cleanup ran without the removal guard, so a session opened meanwhile could be deleted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty session survived: %v", err)
	}
}

func TestReclaimEmptySessionSkipsOneOpenedSinceTheListing(t *testing.T) {
	dir := testenv.TempDir(t)
	path := writeAgedSession(t, dir, "empty.jsonl", toolOnlyTranscript, 7*24*time.Hour)
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	called := false
	err = reclaimEmptySession(path, time.Now(), func(CleanupPendingInfo) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("a busy session is skipped, not an error: %v", err)
	}
	if called {
		t.Fatal("deleted a session whose lease is held")
	}
}
