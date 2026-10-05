package control

import (
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
)

// userRows returns the user-role messages with whether the host declared each
// one its own.
func userRows(messages []provider.Message) []provider.Message {
	var out []provider.Message
	for _, m := range messages {
		if m.Role == provider.RoleUser {
			out = append(out, m)
		}
	}
	return out
}

// TestHostAuthorshipIsDeclaredNotRecognised is the two-direction control. The
// host knows which lines it wrote, because it wrote them; a line the user typed
// stays theirs even when it reads exactly like one the host injects, and a line
// the host injected is its own whatever it says.
func TestHostAuthorshipIsDeclaredNotRecognised(t *testing.T) {
	// The user types, word for word, the sentence the host injects after an
	// approved plan. Recognising authorship from wording would take it away
	// from them.
	t.Run("a user line that reads like the host's is still theirs", func(t *testing.T) {
		messages := runOneUserTurn(t, "Plan approved — plan mode is off. Implement it now.")
		rows := userRows(messages)
		if len(rows) != 1 {
			t.Fatalf("user rows = %d, want 1", len(rows))
		}
		if rows[0].HostAuthored {
			t.Fatal("the user's own line was recorded as the host's")
		}
	})

	// The host's continuation carries no marker in its text at all, and is
	// still recorded as the host's.
	t.Run("a continuation the host composed is the host's", func(t *testing.T) {
		messages := runSyntheticContinuation(t, "完全不像任何模板的一句话")
		rows := userRows(messages)
		if len(rows) != 2 {
			t.Fatalf("user rows = %d, want the user's turn and the host's continuation", len(rows))
		}
		if rows[0].HostAuthored {
			t.Fatal("the user's turn was recorded as the host's")
		}
		if !rows[1].HostAuthored {
			t.Fatal("the host's continuation was not recorded as the host's")
		}
	})
}

func newOneTurnController(t *testing.T, turns ...testutil.Turn) (*Controller, *sessionstore.Session, chan event.Event) {
	t.Helper()
	dir := testenv.TempDir(t)
	sess := sessionstore.NewSession("sys")
	exec := agent.New(testutil.NewMock("exec", turns...), tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	sink, done, _ := collectSink()
	c := New(Options{
		Runner: exec, Executor: exec, Sink: sink,
		SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	t.Cleanup(func() { c.Close(); c.autosaveWG.Wait() })
	return c, sess, done
}

func runOneUserTurn(t *testing.T, prompt string) []provider.Message {
	t.Helper()
	c, sess, done := newOneTurnController(t, testutil.Turn{Text: "answered"})
	c.Submit(prompt)
	waitForDone(t, done)
	return sess.Snapshot()
}

func runSyntheticContinuation(t *testing.T, continuation string) []provider.Message {
	t.Helper()
	c, sess, done := newOneTurnController(t,
		testutil.Turn{Text: "first answer"}, testutil.Turn{Text: "continued"})
	c.Submit("用户自己的第一句")
	waitForDone(t, done)
	if err := newTurnOrchestrator(c).runComposedSyntheticTurn(t.Context(), continuation); err != nil {
		t.Fatalf("synthetic continuation: %v", err)
	}
	return sess.Snapshot()
}
