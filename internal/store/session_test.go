package store

import "testing"

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
		{"event-log", SessionEventLog(p), "/home/u/.reasonix/sessions/abc.events.jsonl"},
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

func TestSessionSidecarFiles(t *testing.T) {
	const p = "/home/u/.reasonix/sessions/abc.jsonl"
	got := SessionSidecarFiles(p)
	want := []string{
		p + ".meta",
		"/home/u/.reasonix/sessions/abc.goal-state.json",
		"/home/u/.reasonix/sessions/abc.events.jsonl",
		"/home/u/.reasonix/sessions/abc.event-index.json",
		"/home/u/.reasonix/sessions/abc.conflicts.jsonl",
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
