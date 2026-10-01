package sessionstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

func writeIdleSession(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name+".jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello " + name})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func ageFiles(t *testing.T, path string, age time.Duration) {
	t.Helper()
	old := time.Now().Add(-age)
	for _, p := range []string{path, store.SessionEventLog(path)} {
		if _, err := os.Stat(p); err == nil {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func archivedOf(t *testing.T, path string) bool {
	t.Helper()
	m, _, err := LoadBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	return m.Archived
}

func TestArchiveInactiveSessionsSkipsWhatMustStay(t *testing.T) {
	dir := t.TempDir()
	idle := writeIdleSession(t, dir, "idle")
	pinned := writeIdleSession(t, dir, "pinned")
	held := writeIdleSession(t, dir, "held")
	leased := writeIdleSession(t, dir, "leased")
	midTurn := writeIdleSession(t, dir, "midturn")
	if err := SetSessionPinned(pinned, true); err != nil {
		t.Fatal(err)
	}
	if _, err := BeginSessionInFlightTurn(midTurn, 0, false); err != nil {
		t.Fatal(err)
	}
	lease, err := TryAcquireSessionLease(leased)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	now := time.Now().Add(10 * 24 * time.Hour)
	n, err := ArchiveInactiveSessions(dir, now, 3*24*time.Hour, func(p string) bool { return p == held })
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || !archivedOf(t, idle) {
		t.Fatalf("archived %d, idle=%v; want only the idle one", n, archivedOf(t, idle))
	}
	for name, p := range map[string]string{"pinned": pinned, "held": held, "leased": leased, "midturn": midTurn} {
		if archivedOf(t, p) {
			t.Errorf("%s was archived", name)
		}
	}
}

func TestArchiveInactiveSessionsLeavesRecentAndIsReversible(t *testing.T) {
	dir := t.TempDir()
	path := writeIdleSession(t, dir, "a")
	n, err := ArchiveInactiveSessions(dir, time.Now(), 3*24*time.Hour, nil)
	if err != nil || n != 0 || archivedOf(t, path) {
		t.Fatalf("a fresh session was archived: n=%d err=%v", n, err)
	}
	later := time.Now().Add(4 * 24 * time.Hour)
	if n, _ := ArchiveInactiveSessions(dir, later, 3*24*time.Hour, nil); n != 1 {
		t.Fatalf("archived %d, want 1", n)
	}
	if n, _ := ArchiveInactiveSessions(dir, later, 3*24*time.Hour, nil); n != 0 {
		t.Fatalf("a second pass archived %d, want 0", n)
	}
	if err := SetSessionArchived(path, false); err != nil {
		t.Fatal(err)
	}
	if n, _ := ArchiveInactiveSessions(dir, time.Now().Add(time.Hour), 3*24*time.Hour, nil); n != 0 || archivedOf(t, path) {
		t.Fatalf("a restored session was archived again straight away (n=%d)", n)
	}
}

func TestPinSessionsKeepsThemFromTheSweep(t *testing.T) {
	dir := t.TempDir()
	path := writeIdleSession(t, dir, "a")
	if err := PinSessions([]string{path}); err != nil {
		t.Fatal(err)
	}
	if n, _ := ArchiveInactiveSessions(dir, time.Now().Add(10*24*time.Hour), time.Hour, nil); n != 0 {
		t.Fatalf("a pinned session was archived (%d)", n)
	}
}

func TestArchiveInactiveSessionsGuards(t *testing.T) {
	dir := t.TempDir()
	later := time.Now().Add(10 * 24 * time.Hour)
	week := 3 * 24 * time.Hour

	superseded := writeIdleSession(t, dir, "superseded")
	if err := UpdateBranchMeta(superseded, false, func(m *BranchMeta) error { m.Superseded = true; return nil }); err != nil {
		t.Fatal(err)
	}
	subagent := writeIdleSession(t, dir, "subagent-abc")
	pending := writeIdleSession(t, dir, "pending")
	if err := MarkCleanupPending(pending, "test"); err != nil {
		t.Fatal(err)
	}
	if n, err := ArchiveInactiveSessions(dir, later, week, nil); err != nil || n != 0 {
		t.Fatalf("archived %d (err %v), want none", n, err)
	}
	for name, p := range map[string]string{"superseded": superseded, "subagent": subagent, "cleanup-pending": pending} {
		if archivedOf(t, p) {
			t.Errorf("%s was archived", name)
		}
	}
}

func TestArchiveInactiveSessionsTrustsTheSidecarClock(t *testing.T) {
	dir := t.TempDir()
	path := writeIdleSession(t, dir, "a")
	ageFiles(t, path, 30*24*time.Hour)
	if err := UpdateBranchMeta(path, false, func(m *BranchMeta) error {
		m.UpdatedAt = time.Now().Add(-time.Hour)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n, _ := ArchiveInactiveSessions(dir, time.Now(), 3*24*time.Hour, nil); n != 0 || archivedOf(t, path) {
		t.Fatalf("a session touched an hour ago was archived (%d)", n)
	}
}

func TestArchiveInactiveSessionsRechecksUnderTheLock(t *testing.T) {
	later := time.Now().Add(10 * 24 * time.Hour)
	for name, write := range map[string]func(path string) error{
		"pinned in between": func(p string) error { return SetSessionPinned(p, true) },
		"touched in between": func(p string) error {
			return UpdateBranchMeta(p, false, func(m *BranchMeta) error {
				m.UpdatedAt = later
				return nil
			})
		},
		"turn started in between": func(p string) error {
			_, err := BeginSessionInFlightTurn(p, 0, false)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeIdleSession(t, dir, "a")
			beforeArchiveLock = func(p string) {
				if err := write(p); err != nil {
					t.Error(err)
				}
			}
			t.Cleanup(func() { beforeArchiveLock = nil })
			if n, err := ArchiveInactiveSessions(dir, later, 3*24*time.Hour, nil); err != nil || n != 0 || archivedOf(t, path) {
				t.Fatalf("n=%d err=%v archived=%v, want the re-check to leave it", n, err, archivedOf(t, path))
			}
		})
	}
}

func TestAutoArchiveMarksAndRestoreClears(t *testing.T) {
	dir := t.TempDir()
	path := writeIdleSession(t, dir, "a")
	later := time.Now().Add(10 * 24 * time.Hour)
	if n, _ := ArchiveInactiveSessions(dir, later, time.Hour, nil); n != 1 {
		t.Fatal("not archived")
	}
	m, _, _ := LoadBranchMeta(path)
	if m.AutoArchivedAt.IsZero() {
		t.Fatal("the sweep left no mark for the notice")
	}
	if err := SetSessionArchived(path, false); err != nil {
		t.Fatal(err)
	}
	if m, _, _ = LoadBranchMeta(path); !m.AutoArchivedAt.IsZero() {
		t.Fatal("restoring kept the automatic-archive mark")
	}
}
