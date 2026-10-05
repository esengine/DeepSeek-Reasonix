package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

type turnStartProvider struct{}

func (p *turnStartProvider) Name() string { return "turn-start" }

func (p *turnStartProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "answered"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// runTurns runs each input as its own turn and returns the TurnStarted events,
// in order. The agent is built with no host around it at all: this layer cannot
// import control, so nothing a checkpoint knows can reach these numbers.
func runTurns(t *testing.T, session *sessionstore.Session, inputs ...string) []event.Event {
	t.Helper()
	var started []event.Event
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnStarted {
			started = append(started, e)
		}
	})
	a := New(&turnStartProvider{}, tool.NewRegistry(), session, Options{}, sink)
	for _, in := range inputs {
		if err := a.Run(context.Background(), in); err != nil {
			t.Fatalf("Run(%q): %v", in, err)
		}
	}
	return started
}

func requireNamed(t *testing.T, e event.Event, wantTurn int) int {
	t.Helper()
	if e.AuthoredTurn == nil || e.MsgIndex == nil {
		t.Fatalf("turn_started named no message: authored=%v index=%v", e.AuthoredTurn, e.MsgIndex)
	}
	if *e.AuthoredTurn != wantTurn {
		t.Fatalf("authored turn = %d, want %d", *e.AuthoredTurn, wantTurn)
	}
	return *e.MsgIndex
}

func TestTurnStartedNamesTheAuthoredMessageItIsAbout(t *testing.T) {
	session := sessionstore.NewSession("system")
	started := runTurns(t, session, "first", "second")
	if len(started) != 2 {
		t.Fatalf("turn_started events = %d, want 2", len(started))
	}
	// The system message holds index 0, so the first authored turn is 1 and the
	// message it opens is 1 — two numbers that are equal once and never again.
	if index := requireNamed(t, started[0], 1); index != 1 {
		t.Fatalf("first turn message index = %d, want 1", index)
	}
	if index := requireNamed(t, started[1], 2); index <= *started[0].MsgIndex {
		t.Fatalf("second turn message index = %d, want past the first turn's", index)
	}
}

func TestLiveTurnStartAndDisplayIndexNameTheSameMessage(t *testing.T) {
	session := sessionstore.NewSession("system")
	started := runTurns(t, session, "first", "second")
	msgs := session.Snapshot()
	digest, err := sessionstore.DigestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("DigestSessionMessages: %v", err)
	}
	idx := sessionstore.BuildSessionDisplayIndex(msgs, 1, true, digest)
	if idx == nil {
		t.Fatal("BuildSessionDisplayIndex returned nil")
	}
	for turn, e := range started {
		index := requireNamed(t, e, turn+1)
		if index < 0 || index >= len(idx.Entries) {
			t.Fatalf("turn %d named index %d, outside a transcript of %d", turn+1, index, len(idx.Entries))
		}
		entry := idx.Entries[index]
		if entry.Role != provider.RoleUser || !entry.StartsTurn || entry.AuthoredTurn != *e.AuthoredTurn {
			t.Fatalf("live turn %d named index %d; the record there is %+v", *e.AuthoredTurn, index, entry)
		}
	}
}

func TestMidTurnSteerMintsNoAuthoredTurn(t *testing.T) {
	session := sessionstore.NewSession("system")
	runTurns(t, session, "first")
	session.Add(provider.Message{Role: provider.RoleUser, Content: sessionstore.MidTurnSteerMessage("keep going", false)})
	started := runTurns(t, session, "second")
	if len(started) != 1 {
		t.Fatalf("turn_started events = %d, want 1", len(started))
	}
	requireNamed(t, started[0], 2)
}

func TestSyntheticUserTurnMintsNoAuthoredTurn(t *testing.T) {
	session := sessionstore.NewSession("system")
	runTurns(t, session, "first")
	injected := runTurns(t, session, "Continue pursuing the active goal: finish the migration.")
	if len(injected) != 1 {
		t.Fatalf("turn_started events = %d, want 1", len(injected))
	}
	if injected[0].AuthoredTurn != nil || injected[0].MsgIndex != nil {
		t.Fatalf("host-injected turn named a message: authored=%v index=%v",
			*injected[0].AuthoredTurn, *injected[0].MsgIndex)
	}
	// The number it did not take is still there for the next real turn.
	next := runTurns(t, session, "second")
	requireNamed(t, next[0], 2)
}

// TestLandingHoldsTheMessageToTheAnnouncedIdentity checks the seam holding a
// published name to the message that lands. The mismatch is constructed —
// control measures the index one statement before handing over the identity —
// so this proves the check fires, not that anything reaches it.
func TestLandingHoldsTheMessageToTheAnnouncedIdentity(t *testing.T) {
	land := func(id sessionstore.AuthoredTurnIdentity, text string) []event.Event {
		var notices []event.Event
		sink := event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice && e.Code == event.NoticeCodeTurnIdentityMismatch {
				notices = append(notices, e)
			}
		})
		session := sessionstore.NewSession("system")
		a := New(&turnStartProvider{}, tool.NewRegistry(), session, Options{}, sink)
		ctx := WithHostTurnBoundary(context.Background(), HostTurnBoundary{Authored: &id})
		a.LandAuthoredUserMessage(ctx, provider.Message{Role: provider.RoleUser, Content: text})
		if got := session.Snapshot()[1].RawContent; got != id.Raw {
			t.Fatalf("landed RawContent = %q, want the announced identity's %q", got, id.Raw)
		}
		return notices
	}

	if notices := land(sessionstore.AuthoredTurnIdentity{AuthoredTurn: 1, MsgIndex: 1, Raw: "第一句"}, "第一句"); len(notices) != 0 {
		t.Fatalf("a message that landed where it was named reported %d mismatches", len(notices))
	}
	notices := land(sessionstore.AuthoredTurnIdentity{AuthoredTurn: 1, MsgIndex: 7, Raw: "第一句"}, "第一句")
	if len(notices) != 1 {
		t.Fatalf("a message that landed elsewhere reported %d mismatches, want 1", len(notices))
	}
	if notices[0].Audience != event.NoticeAudienceOperator {
		t.Fatalf("mismatch audience = %q, want operator: this is about the machine, not the conversation", notices[0].Audience)
	}
}
