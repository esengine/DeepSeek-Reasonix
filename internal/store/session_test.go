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
		{"checkpoint", SessionCheckpointDir},
		{"jobs", SessionJobsDir},
		{"cleanup-pending", SessionCleanupPending},
	} {
		if got := fn.f(""); got != "" {
			t.Errorf("%s(\"\") = %q, want empty", fn.name, got)
		}
	}
}
