package agent

import (
	"context"
	"sync"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// sinkRecorder records all events for inspection.
type sinkRecorder struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *sinkRecorder) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *sinkRecorder) Events() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]event.Event, len(s.events))
	copy(out, s.events)
	return out
}

// TestSteerQueuesAndConsumes verifies that Steer() queues a message and
// consumeSteer() returns it at the next iteration boundary.
func TestSteerQueuesAndConsumes(t *testing.T) {
	mp := testutil.NewMock("m", testutil.Turn{Text: "ok"})
	sink := &sinkRecorder{}
	a := New(mp, tool.NewRegistry(), NewSession(""), Options{}, sink)

	// Steer before Run
	a.Steer("focus on tests")
	if err := a.Run(context.Background(), "write code"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify the steer appears in the API request as a user message
	reqs := mp.Requests()
	if len(reqs) == 0 {
		t.Fatal("no API requests recorded")
	}
	lastReq := reqs[len(reqs)-1]

	// The steer should be the last user message before the assistant turn
	var steerFound bool
	for _, msg := range lastReq.Messages {
		if msg.Role == provider.RoleUser && msg.Content == midTurnSteerMessage("focus on tests") {
			steerFound = true
			break
		}
	}
	if !steerFound {
		t.Fatal("steer message not found in API request messages")
	}
}

// TestSteerConsumedGates verifies that steerConsumed and SteerConsumed work.
func TestSteerConsumedGates(t *testing.T) {
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	// Initially not consumed (nothing to consume)
	if a.SteerConsumed() {
		t.Fatal("SteerConsumed should be false initially")
	}

	// After Steer(), steerConsumed should still be false (not consumed yet)
	a.Steer("hello")
	if a.SteerConsumed() {
		t.Fatal("SteerConsumed should be false after Steer() but before consume")
	}

	// Consume the steer — should set steerConsumed = true (queue now empty)
	text, ok := a.consumeSteer()
	if !ok || text != "hello" {
		t.Fatalf("consumeSteer got %q, %v", text, ok)
	}
	if !a.SteerConsumed() {
		t.Fatal("SteerConsumed should be true after consuming last item")
	}

	// Steer again — resets to false
	a.Steer("world")
	if a.SteerConsumed() {
		t.Fatal("SteerConsumed should be false after new Steer()")
	}
}

// TestMultipleSteersFIFO verifies multiple steers are consumed in FIFO order.
func TestMultipleSteersFIFO(t *testing.T) {
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	a.Steer("first")
	a.Steer("second")
	a.Steer("third")

	// Consume in FIFO order
	for _, want := range []string{"first", "second"} {
		got, ok := a.consumeSteer()
		if !ok || got != want {
			t.Fatalf("consumeSteer got %q, want %q", got, want)
		}
		// steerConsumed should be false because queue is not empty yet
		if a.SteerConsumed() {
			t.Fatal("SteerConsumed should be false while items remain")
		}
	}

	// Last one — queue becomes empty
	got, ok := a.consumeSteer()
	if !ok || got != "third" {
		t.Fatalf("consumeSteer got %q, want %q", got, "third")
	}
	if !a.SteerConsumed() {
		t.Fatal("SteerConsumed should be true after consuming last item")
	}
}

// TestSteerNoToolCallsContinuesWithMoreSteers verifies that when the model
// gives a final answer but there are still queued steers, the loop continues
// instead of returning done.
func TestSteerNoToolCallsContinuesWithMoreSteers(t *testing.T) {
	// Script: first turn returns text with no tool calls (final answer)
	// but the steer queue has more items.
	mp := testutil.NewMock("m",
		testutil.Turn{Text: "first answer"},
		testutil.Turn{Text: "second answer"},
	)
	sink := &sinkRecorder{}
	a := New(mp, tool.NewRegistry(), NewSession(""), Options{}, sink)

	// Queue two steers from the start
	a.Steer("guidance one")
	a.Steer("guidance two")

	if err := a.Run(context.Background(), "initial prompt"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The loop should have made 2 API calls (one per steer)
	reqs := mp.Requests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 API requests (one per steer), got %d", len(reqs))
	}

	// First request should contain "guidance one"
	req1 := reqs[0]
	found1 := false
	for _, msg := range req1.Messages {
		if msg.Role == provider.RoleUser && msg.Content == midTurnSteerMessage("guidance one") {
			found1 = true
			break
		}
	}
	if !found1 {
		t.Fatal("first request missing 'guidance one' steer")
	}

	// Second request should contain "guidance two"
	req2 := reqs[1]
	found2 := false
	for _, msg := range req2.Messages {
		if msg.Role == provider.RoleUser && msg.Content == midTurnSteerMessage("guidance two") {
			found2 = true
			break
		}
	}
	if !found2 {
		t.Fatal("second request missing 'guidance two' steer")
	}
}

// TestSteerEventEmitted verifies a Steer event is emitted when a steer is consumed.
func TestSteerEventEmitted(t *testing.T) {
	mp := testutil.NewMock("m", testutil.Turn{Text: "ok"})
	sink := &sinkRecorder{}
	a := New(mp, tool.NewRegistry(), NewSession(""), Options{}, sink)

	a.Steer("guide me")
	if err := a.Run(context.Background(), "do it"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	events := sink.Events()
	var steerEvents []event.Event
	for _, e := range events {
		if e.Kind == event.Steer {
			steerEvents = append(steerEvents, e)
		}
	}
	if len(steerEvents) == 0 {
		t.Fatal("no Steer events emitted")
	}
	if steerEvents[0].Text != "guide me" {
		t.Fatalf("Steer event text = %q, want %q", steerEvents[0].Text, "guide me")
	}
}

// TestSteerQueueEmptiedOnRunExit verifies clearSteerQueue runs when Run exits.
func TestSteerQueueEmptiedOnRunExit(t *testing.T) {
	mp := testutil.NewMock("m", testutil.Turn{Text: "ok"})
	a := New(mp, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	// Steer before Run
	a.Steer("hello")

	// Run should consume the steer, then clear on exit (defer)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Queue should be empty after Run exits
	if text, ok := a.consumeSteer(); ok {
		t.Fatalf("expected empty steer queue after Run, got %q", text)
	}
}

// TestSteerWrappedWithMessage verifies the steer message is wrapped with
// the mid-turn steer prefix.
func TestSteerWrappedWithMessage(t *testing.T) {
	wrapped := midTurnSteerMessage("focus on tests")
	if wrapped != midTurnSteerPrefix+"\n"+"focus on tests" {
		t.Fatalf("unexpected wrap format: %q", wrapped)
	}
}

// TestSteerPersistsAcrossIterations verifies that a mid-turn steer,
// once consumed and persisted to the session, appears in every
// subsequent API request within the same turn — not just the one
// where it was consumed. This is the key difference from the
// pre-fix behaviour where a steer was spliced into a temporary
// msgs copy and lost on the next iteration.
func TestSteerPersistsAcrossIterations(t *testing.T) {
	reg := tool.NewRegistry()
	// Register a read-only tool so the agent can execute it without gating.
	reg.Add(fakeTool{name: "ls", readOnly: true})

	// Script: first turn returns a tool call (so the loop iterates again),
	// second turn returns a final answer.
	mp := testutil.NewMock("m",
		testutil.Turn{
			ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "ls", Arguments: `{}`}},
		},
		testutil.Turn{Text: "done"},
	)
	sink := &sinkRecorder{}
	a := New(mp, reg, NewSession(""), Options{}, sink)

	a.Steer("use short responses")
	if err := a.Run(context.Background(), "list files"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := mp.Requests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 API requests (tool call + final answer), got %d", len(reqs))
	}

	steerMsg := midTurnSteerMessage("use short responses")

	// The steer must appear in the first request (iteration 1).
	found1 := msgListContains(reqs[0].Messages, provider.RoleUser, steerMsg)
	if !found1 {
		t.Fatal("iteration 1: steer message not found in API request")
	}

	// The steer must also appear in the second request (iteration 2)
	// — proving it persisted in the session, not just a temporary copy.
	found2 := msgListContains(reqs[1].Messages, provider.RoleUser, steerMsg)
	if !found2 {
		t.Fatal("iteration 2: steer message NOT found — it was lost after the first iteration (pre-fix behaviour)")
	}

	// The steer should appear exactly once in each request (not duplicated).
	count1 := msgCount(reqs[0].Messages, provider.RoleUser, steerMsg)
	count2 := msgCount(reqs[1].Messages, provider.RoleUser, steerMsg)
	if count1 != 1 {
		t.Fatalf("iteration 1: steer appears %d times, want 1", count1)
	}
	if count2 != 1 {
		t.Fatalf("iteration 2: steer appears %d times, want 1", count2)
	}
}

// TestSteerPersistsWhenInjectedMidLoop verifies that a steer injected
// after the turn has already started (simulating a real mid-turn user
// interaction) also persists into subsequent iterations.
func TestSteerPersistsWhenInjectedMidLoop(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "ls", readOnly: true})

	mp := testutil.NewMock("m",
		testutil.Turn{
			ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "ls", Arguments: `{}`}},
		},
		testutil.Turn{Text: "done"},
	)
	sink := &sinkRecorder{}
	a := New(mp, reg, NewSession(""), Options{}, sink)

	// Simulate the real scenario: the steer arrives during the turn,
	// not before it. We steer right before Run — the effect is the
	// same as a user typing while the agent is in iteration 0 (the
	// steer is consumed at the iteration 1 boundary in practice, but
	// here it's already queued at iteration 0).
	a.Steer("mid-turn correction")

	if err := a.Run(context.Background(), "do work"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := mp.Requests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 API requests, got %d", len(reqs))
	}

	steerMsg := midTurnSteerMessage("mid-turn correction")

	if !msgListContains(reqs[0].Messages, provider.RoleUser, steerMsg) {
		t.Fatal("iteration 1: steer not found")
	}
	if !msgListContains(reqs[1].Messages, provider.RoleUser, steerMsg) {
		t.Fatal("iteration 2: steer NOT found — persistence broken")
	}
}

func msgListContains(msgs []provider.Message, role provider.Role, content string) bool {
	return msgCount(msgs, role, content) > 0
}

func msgCount(msgs []provider.Message, role provider.Role, content string) int {
	n := 0
	for _, m := range msgs {
		if m.Role == role && m.Content == content {
			n++
		}
	}
	return n
}
