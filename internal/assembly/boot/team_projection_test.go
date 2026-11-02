package boot

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/session/team"
	"reasonix/internal/state/sessioninbox"
)

type fakeTeamSeat struct {
	mu sync.Mutex
	p  team.Projection
}

func (f *fakeTeamSeat) Projection() (team.Projection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.p, nil
}

func (f *fakeTeamSeat) Team() (*team.Service, team.SeatRef) { return nil, team.SeatRef{} }

func (f *fakeTeamSeat) set(p team.Projection) {
	f.mu.Lock()
	f.p = p
	f.mu.Unlock()
}

// A team seat is dynamic state: binding one, and every edit after, rides the
// turn under its own tags while the prefix stays where it was.
func TestProjectionTeamIdentityRidesTheTurnNotThePrefix(t *testing.T) {
	h := newProjectionHarness(t, "ctxproj-team", "", "")
	before := h.turn("turn-a")
	seat := &fakeTeamSeat{p: team.Projection{Identity: "You are Ada (agent a-1).", Roster: "Other agents: Ben."}}
	h.ctrl.BindTeamSeat(seat)

	first := h.turn("turn-b")
	if a, b := systemOf(before), systemOf(first); a != b {
		t.Fatalf("cache-boundary: binding a team seat moved the prefix:\nfirst diff site: %q", firstDivergence(a, b))
	}
	got := projectionOf(t, first, "turn-b")
	if !strings.Contains(blockOf(got, "agent-identity"), "You are Ada (agent a-1).") || !strings.Contains(blockOf(got, "team-roster"), "Other agents: Ben.") {
		t.Fatalf("freshness: the bound seat did not reach the turn:\n%s", got)
	}
	if blockOf(got, "group-roster") != "" {
		t.Fatalf("a seat with no group projected one:\n%s", got)
	}

	if again := projectionOf(t, h.turn("turn-c"), "turn-c"); blockOf(again, "agent-identity") != "" || blockOf(again, "team-roster") != "" {
		t.Fatalf("an unchanged seat was sent again:\n%s", again)
	}

	seat.set(team.Projection{Identity: "You are Ada (agent a-1), the team's Writer.", Roster: "Other agents: Ben."})
	edited := h.turn("turn-d")
	got = projectionOf(t, edited, "turn-d")
	if !strings.Contains(blockOf(got, "agent-identity"), "the team's Writer") || blockOf(got, "team-roster") != "" {
		t.Fatalf("freshness: an identity edit was not the next turn's projection, or the unchanged roster rode along:\n%s", got)
	}
	if a, b := systemOf(before), systemOf(edited); a != b {
		t.Fatalf("cache-boundary: an identity edit moved the prefix:\nfirst diff site: %q", firstDivergence(a, b))
	}
}

// An agent's first turn is often another agent's message, which runs as a turn
// the host started. It must carry who the agent is all the same.
func TestProjectionTeamIdentityReachesATurnTheHostStarted(t *testing.T) {
	h := newProjectionHarness(t, "ctxproj-team-host", "", "")
	h.ctrl.BindTeamSeat(&fakeTeamSeat{p: team.Projection{Identity: "You are Ben (agent a-2).", Roster: "Other agents: Ada."}})
	h.ctrl.EnsureSessionPath()
	start := len(h.rec.requests())
	if _, err := h.ctrl.TryEnqueueFollowup(control.InboxRequest{Origin: sessioninbox.OriginHost, Display: "message from Ada", Raw: "message from Ada", Submit: "host-turn-text from Ada"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for len(h.rec.requests()) <= start || !h.events.saw(event.TurnDone) {
		if time.Now().After(deadline) {
			t.Fatal("the host-started turn never reached the provider")
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := projectionOf(t, h.rec.requests()[start], "host-turn-text from Ada")
	if !strings.Contains(blockOf(got, "agent-identity"), "You are Ben") {
		t.Fatalf("a turn the host started carried no identity:\n%s", got)
	}
}

// A group block the model holds goes on reading as current until something
// replaces it, so a seat that left its group owes one retraction, then nothing.
func TestProjectionTeamGroupIsRetractedOnceWhenTheSeatLeaves(t *testing.T) {
	h := newProjectionHarness(t, "ctxproj-team-group", "", "")
	seat := &fakeTeamSeat{p: team.Projection{Identity: "You are Ada.", Roster: "Ben.", Group: "You are in the group \"Launch\"."}}
	h.ctrl.BindTeamSeat(seat)
	if got := projectionOf(t, h.turn("turn-a"), "turn-a"); !strings.Contains(blockOf(got, "group-roster"), "Launch") {
		t.Fatalf("no group block:\n%s", got)
	}
	seat.set(team.Projection{Identity: "You are Ada.", Roster: "Ben."})
	if got := projectionOf(t, h.turn("turn-b"), "turn-b"); !strings.Contains(blockOf(got, "group-roster"), "no longer a member") {
		t.Fatalf("the departed seat's group block was not retracted:\n%s", got)
	}
	if got := projectionOf(t, h.turn("turn-c"), "turn-c"); blockOf(got, "group-roster") != "" {
		t.Fatalf("the retraction was sent twice:\n%s", got)
	}
}

// A fold summarises the turns that carried the seat's blocks away, so the
// latest projection is owed again.
func TestProjectionTeamSeatIsReOwedAfterCompaction(t *testing.T) {
	h := newProjectionHarness(t, "ctxproj-team-compaction", "\ncompact_ratio = 0.4\n", "\ncontext_window = 12000\n")
	h.ctrl.BindTeamSeat(&fakeTeamSeat{p: team.Projection{Identity: "You are Ada (agent a-1).", Roster: "Other agents: Ben."}})
	filler := strings.Repeat("filler sentence about the ledger. ", 200)
	for _, prompt := range []string{"turn-1", "turn-2", "turn-3", "turn-4", "turn-5", "turn-6"} {
		h.turn(prompt + " " + filler)
	}
	if _, err := h.ctrl.Compact(context.Background(), agent.CompactRequest{}); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !h.events.saw(event.CompactionDone) {
		t.Fatalf("no fold completed, so this arm measures nothing: %v", h.events.kinds)
	}
	got := projectionOf(t, h.turn("turn-eta"), "turn-eta")
	if !strings.Contains(blockOf(got, "agent-identity"), "You are Ada") || !strings.Contains(blockOf(got, "team-roster"), "Ben") {
		t.Fatalf("freshness: the fold left the session without its identity:\n%s", got)
	}
}
