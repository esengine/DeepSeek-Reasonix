package control

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func runTwoTurns(t *testing.T) (*Controller, *agent.Agent, *[]event.Event) {
	t.Helper()
	dir := t.TempDir()
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("first answer"),
		textTurn("second answer"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	var events []event.Event
	c := New(Options{
		Runner:     ag,
		Executor:   ag,
		SessionDir: dir,
		Label:      "test",
		Sink:       event.FuncSink(func(e event.Event) { events = append(events, e) }),
	})
	c.SetSessionPath(agent.NewSessionPath(dir, "test"))
	if err := c.runTurnWithRaw(context.Background(), "first prompt", "first prompt"); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if err := c.runTurnWithRaw(context.Background(), "second prompt", "second prompt"); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	return c, ag, &events
}

// TestRewindConversationFailsLoudlyAfterCompaction reproduces #3598: once
// compaction shrinks the message log below a turn's recorded boundary, a
// conversation/both rewind to that turn skipped the truncation but still emitted
// a success notice — code rolled back, conversation silently did not.
func TestRewindConversationFailsLoudlyAfterCompaction(t *testing.T) {
	c, ag, events := runTwoTurns(t)

	c.mu.Lock()
	lastTurn := c.cpTurn - 1
	boundary := c.cpBound[lastTurn]
	c.mu.Unlock()
	if boundary <= 1 {
		t.Fatalf("expected the latest turn's boundary above 1, got cpBound=%v", c.cpBound)
	}

	// Auto-compaction replaces the prefix with a summary, shrinking the log below
	// the recorded boundary; compaction does not rewrite checkpoint boundaries.
	sess := ag.Session()
	sess.Messages = []provider.Message{{Role: provider.RoleUser, Content: "summary"}}

	*events = nil
	err := c.Rewind(lastTurn, RewindBoth)
	if err == nil || !strings.Contains(err.Error(), "compacted") {
		t.Fatalf("Rewind after compaction error = %v, want a 'compacted past' failure", err)
	}
	for _, e := range *events {
		if e.Kind == event.Notice && strings.Contains(e.Text, "rewound conversation") {
			t.Fatalf("emitted a false conversation-rewind success after skipping truncation: %q", e.Text)
		}
	}
	if got := len(ag.Session().Messages); got != 1 {
		t.Fatalf("session messages = %d, want the compacted log left intact at 1", got)
	}
}

// TestRewindConversationSucceedsWithLiveBoundary is the companion happy path: a
// boundary still within the log truncates the conversation and reports success.
func TestRewindConversationSucceedsWithLiveBoundary(t *testing.T) {
	c, ag, events := runTwoTurns(t)

	c.mu.Lock()
	lastTurn := c.cpTurn - 1
	boundary := c.cpBound[lastTurn]
	c.mu.Unlock()

	*events = nil
	if err := c.Rewind(lastTurn, RewindConversation); err != nil {
		t.Fatalf("Rewind with a live boundary: %v", err)
	}
	if got := len(ag.Session().Messages); got != boundary {
		t.Fatalf("session truncated to %d messages, want boundary %d", got, boundary)
	}
	ok := false
	for _, e := range *events {
		if e.Kind == event.Notice && strings.Contains(e.Text, "rewound conversation") {
			ok = true
		}
	}
	if !ok {
		t.Fatal("expected a conversation-rewind success notice")
	}
}
