package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionSidecarLayout(t *testing.T) {
	const p = "/home/u/.reasonix/sessions/abc.jsonl"
	cases := []struct {
		name string
		got  string
		want string
	}{
		// .meta appends to the full path (historical layout); the rest replace .jsonl.
		{"meta", SessionMeta(p), p + ".meta"},
		{"goal-state", SessionGoalState(p), "/home/u/.reasonix/sessions/abc.goal-state.json"},
		// scheduled-tasks lives in the project's .reasonix (per working directory),
		// one file shared by every session launched there.
		{"scheduled-tasks", SessionScheduledTasks("/home/u/proj", p), filepath.Join("/home/u/proj", ".reasonix", "scheduled-tasks.json")},
		{"event-log", SessionEventLog(p), "/home/u/.reasonix/sessions/abc.events.jsonl"},
		{"event-log-damaged", SessionEventLogDamaged(p), "/home/u/.reasonix/sessions/abc.events.jsonl.damaged"},
		{"event-index", SessionEventIndex(p), "/home/u/.reasonix/sessions/abc.event-index.json"},
		{"conflict-log", SessionConflictLog(p), "/home/u/.reasonix/sessions/abc.conflicts.jsonl"},
		{"lock", SessionLockFile(p), p + ".lock"},
		{"lease-lock", SessionLeaseLock(p), p + ".lease.lock"},
		{"lease-info", SessionLeaseInfo(p), p + ".lease.json"},
		{"checkpoint", SessionCheckpointDir(p), "/home/u/.reasonix/sessions/abc.ckpt"},
		{"jobs", SessionJobsDir(p), "/home/u/.reasonix/sessions/abc.jobs"},
		{"cleanup-pending", SessionCleanupPending(p), "/home/u/.reasonix/sessions/abc.cleanup-pending.json"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestSessionSidecarEmptyPath(t *testing.T) {
	for _, fn := range []struct {
		name string
		f    func(string) string
	}{
		{"meta", SessionMeta},
		{"goal-state", SessionGoalState},
		{"event-log", SessionEventLog},
		{"event-log-damaged", SessionEventLogDamaged},
		{"event-index", SessionEventIndex},
		{"conflict-log", SessionConflictLog},
		{"lock", SessionLockFile},
		{"lease-lock", SessionLeaseLock},
		{"lease-info", SessionLeaseInfo},
		{"checkpoint", SessionCheckpointDir},
		{"jobs", SessionJobsDir},
		{"cleanup-pending", SessionCleanupPending},
	} {
		if got := fn.f(""); got != "" {
			t.Errorf("%s(\"\") = %q, want empty", fn.name, got)
		}
	}
	// scheduled-tasks takes (workspaceRoot, sessionPath). Without a root there
	// is no per-directory anchor: persistence is disabled ("") rather than
	// leaking a beside-session file that session deletion no longer sweeps.
	for _, args := range [][2]string{{"", ""}, {"", "/x/a.jsonl"}, {"/proj", ""}} {
		if got := SessionScheduledTasks(args[0], args[1]); got != "" {
			t.Errorf("SessionScheduledTasks(%q, %q) = %q, want empty", args[0], args[1], got)
		}
	}
	if got := LegacyScheduledTasks("/x/a.jsonl"); got != "/x/a.scheduled-tasks.json" {
		t.Errorf("LegacyScheduledTasks = %q, want the historical beside-session path", got)
	}
	if LegacyScheduledTasks("") != "" {
		t.Error("LegacyScheduledTasks(\"\") should be empty")
	}
}

func TestIsSessionTranscriptName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"session.jsonl", true},
		{"session.events.jsonl", false},
		{"session.conflicts.jsonl", false},
		{"session.guardian.jsonl", false},
		{"session.guardian.events.jsonl", false},
		{"session.events.jsonl.damaged", false},
		{"session.jsonl.meta", false},
		{"notes.txt", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsSessionTranscriptName(c.name); got != c.want {
			t.Errorf("IsSessionTranscriptName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMigrateScheduledTasks(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "sessions", "abc.jsonl")
	legacy := LegacyScheduledTasks(sessionPath)
	root := filepath.Join(dir, "proj")
	newPath := SessionScheduledTasks(root, sessionPath)
	payload := []byte(`[{"id":"aa11","cron":"*/5 * * * *","prompt":"old loop"}]`)
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	if !MigrateScheduledTasks(root, sessionPath) {
		t.Fatal("migration should import the legacy sidecar")
	}
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("new store missing after migration: %v", err)
	}
	if string(data) != string(payload) {
		t.Errorf("new store = %q, want %q", data, payload)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy sidecar should be removed, stat err = %v", err)
	}

	// Second call: no legacy file left, no import.
	if MigrateScheduledTasks(root, sessionPath) {
		t.Error("second migration should be a no-op")
	}
	// New store exists: no import even if a legacy file reappears.
	if err := os.WriteFile(legacy, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if MigrateScheduledTasks(root, sessionPath) {
		t.Error("migration with an existing new store should be a no-op")
	}
	// No root: no per-directory anchor, no migration.
	if MigrateScheduledTasks("", sessionPath) {
		t.Error("migration without a root should be a no-op")
	}
}

func TestSessionSidecarFiles(t *testing.T) {
	const p = "/home/u/.reasonix/sessions/abc.jsonl"
	got := SessionSidecarFiles(p)
	want := []string{
		p + ".meta",
		"/home/u/.reasonix/sessions/abc.goal-state.json",
		"/home/u/.reasonix/sessions/abc.events.jsonl",
		"/home/u/.reasonix/sessions/abc.events.jsonl.damaged",
		"/home/u/.reasonix/sessions/abc.event-index.json",
		"/home/u/.reasonix/sessions/abc.conflicts.jsonl",
		"/home/u/.reasonix/sessions/abc.recovery.json",
	}
	// The /loop scheduled-task file must NOT be session-owned: crons belong to
	// the working directory and survive /new, /clear, and session deletion.
	for _, s := range got {
		if strings.Contains(s, "scheduled-tasks") {
			t.Errorf("SessionSidecarFiles must not include the cron file, got %q", s)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("SessionSidecarFiles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SessionSidecarFiles[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if SessionSidecarFiles("") != nil {
		t.Error("SessionSidecarFiles(\"\") should be nil")
	}
}

func TestSessionScheduledTasksPath(t *testing.T) {
	const p = "/home/u/.reasonix/sessions/abc.jsonl"
	want := filepath.Join("/home/u/proj", ".reasonix", "scheduled-tasks.json")
	if got := SessionScheduledTasks("/home/u/proj", p); got != want {
		t.Errorf("with root = %q, want the per-directory .reasonix path (no session stem)", got)
	}
	// Same directory must yield the same file regardless of the session.
	if got := SessionScheduledTasks("/home/u/proj", "/home/u/.reasonix/sessions/xyz.jsonl"); got != want {
		t.Errorf("another session in the same dir = %q, want the same per-directory file", got)
	}
	if got := SessionScheduledTasks("", p); got != "" {
		t.Errorf("without root = %q, want \"\" (no persistence anchor, no leak)", got)
	}
	if SessionScheduledTasks("/home/u/proj", "") != "" {
		t.Error("SessionScheduledTasks with empty session path should be \"\"")
	}
}
