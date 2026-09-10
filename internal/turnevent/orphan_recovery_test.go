package turnevent

import (
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestOrphanRecoveryDistinguishesPendingAndStartedAndIsIdempotent(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "started"}[started], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			l, err := Open(path, "session")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = l.Begin(); err != nil {
				t.Fatal(err)
			}
			if _, _, err = l.Append(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "call", Name: "write", RunState: provider.ToolRunPending}}, event.TurnInProgress); err != nil {
				t.Fatal(err)
			}
			if started {
				if _, _, err = l.Append(event.Event{Kind: event.ToolStarted, Tool: event.Tool{ID: "call", Name: "write", RunState: provider.ToolRunStarted, AttemptID: "attempt"}}, event.TurnInProgress); err != nil {
					t.Fatal(err)
				}
			}
			if err = l.Close(); err != nil {
				t.Fatal(err)
			}
			l, err = Open(path, "session")
			if err != nil {
				t.Fatal(err)
			}
			records, err := l.EventsAfter(0)
			if err != nil {
				t.Fatal(err)
			}
			var state provider.ToolRunState
			for _, r := range records {
				if r.Kind == "tool_result" {
					state = r.Event.Tool.RunState
				}
			}
			want := provider.ToolRunCancelled
			if started {
				want = provider.ToolRunUnknown
			}
			if state != want {
				t.Fatalf("recovered state=%s want=%s", state, want)
			}
			count := len(records)
			_ = l.Close()
			l, err = Open(path, "session")
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			again, err := l.EventsAfter(0)
			if err != nil || len(again) != count {
				t.Fatalf("second reopen appended recovery events: %d -> %d err=%v", count, len(again), err)
			}
		})
	}
}
