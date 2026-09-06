package control

import (
	"errors"
	"reasonix/internal/event"
	"sync"
	"testing"
)

func TestResolvePromptExactRejectsStaleTurnBeforeDispatch(t *testing.T) {
	c := New(Options{})
	t.Cleanup(c.Close)
	err := c.ResolvePromptExact(PromptIdentity{
		PromptID: "prompt-1", TurnID: "turn-stale", Kind: PromptAsk,
	}, PromptAnswer{})
	if !errors.Is(err, ErrPromptStaleTurn) {
		t.Fatalf("ResolvePromptExact error = %v, want ErrPromptStaleTurn", err)
	}
}

func TestResolvePromptExactRejectsIncompleteIdentity(t *testing.T) {
	c := New(Options{})
	t.Cleanup(c.Close)
	err := c.ResolvePromptExact(PromptIdentity{PromptID: "prompt-1", Kind: PromptAsk}, PromptAnswer{})
	if !errors.Is(err, ErrPromptNotPending) {
		t.Fatalf("ResolvePromptExact error = %v, want ErrPromptNotPending", err)
	}
}

func TestResolvePromptExactRejectsStaleRuntime(t *testing.T) {
	c := New(Options{})
	t.Cleanup(c.Close)
	c.SetTurnEventRoutingMetadata("runtime-current", "")
	err := c.ResolvePromptExact(PromptIdentity{
		PromptID: "prompt-1", TurnID: "turn-any", RuntimeEpoch: "runtime-old", Kind: PromptAsk,
	}, PromptAnswer{})
	if !errors.Is(err, ErrPromptStaleRuntime) {
		t.Fatalf("ResolvePromptExact error = %v, want ErrPromptStaleRuntime", err)
	}
}

func TestResolvePromptExactRejectsLegacyIdentityAfterRuntimeEpochIsSet(t *testing.T) {
	c := New(Options{})
	t.Cleanup(c.Close)
	c.SetTurnEventRoutingMetadata("runtime-current", "")
	err := c.ResolvePromptExact(PromptIdentity{PromptID: "legacy", TurnID: "turn-any", Kind: PromptAsk}, PromptAnswer{})
	if !errors.Is(err, ErrPromptStaleRuntime) {
		t.Fatalf("legacy exact resolve error = %v, want ErrPromptStaleRuntime", err)
	}
}

func TestResolvePromptExactRejectsClosedController(t *testing.T) {
	c := New(Options{})
	c.Close()
	err := c.ResolvePromptExact(PromptIdentity{PromptID: "p", TurnID: "t", Kind: PromptAsk}, PromptAnswer{})
	if !errors.Is(err, ErrPromptNotPending) {
		t.Fatalf("closed resolver error = %v", err)
	}
}

func TestPendingPromptOwnerTracksResolvedIdentity(t *testing.T) {
	var owner PendingPromptOwner
	id := PromptIdentity{PromptID: "p", TurnID: "t", Kind: PromptAsk}
	if err := owner.Register(id); err != nil {
		t.Fatal(err)
	}
	if got, ok := owner.Identity("p"); !ok || got != id {
		t.Fatalf("registered identity = %+v, %v", got, ok)
	}
	owner.MarkResolved(id)
	if _, ok := owner.Identity("p"); ok {
		t.Fatal("resolved prompt remains pending")
	}
	if !owner.WasResolved("p") {
		t.Fatal("resolved prompt was not recorded")
	}
}

func TestPendingPromptOwnerRejectsConcurrentResolveReservation(t *testing.T) {
	var owner PendingPromptOwner
	id := PromptIdentity{PromptID: "p", TurnID: "t", Kind: PromptMCP}
	if err := owner.Register(id); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results <- owner.BeginResolve(id) }()
	}
	close(start)
	wg.Wait()
	close(results)
	var success, already int
	for err := range results {
		if err == nil {
			success++
		}
		if errors.Is(err, ErrPromptAlreadyResolved) {
			already++
		}
	}
	if success != 1 || already != 1 {
		t.Fatalf("resolve reservations = success %d already %d", success, already)
	}
}

func TestPendingPromptOwnerResolveRestoresAfterFailure(t *testing.T) {
	var owner PendingPromptOwner
	id := PromptIdentity{PromptID: "p-fail", TurnID: "t", Kind: PromptAsk}
	if err := owner.RegisterPrompt(PendingPrompt{Identity: id, Resolve: func(PromptAnswer) error { return errors.New("persist failed") }}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Resolve(id, PromptAnswer{}); err == nil || err.Error() != "persist failed" {
		t.Fatalf("resolve error = %v", err)
	}
	pending, ok := owner.Identity(id.PromptID)
	if !ok || pending != id {
		t.Fatalf("failed resolve did not restore pending identity: %+v %v", pending, ok)
	}
}

func TestPendingPromptOwnerBindsMissingRoutingOnce(t *testing.T) {
	var owner PendingPromptOwner
	id := PromptIdentity{PromptID: "p-bind", Kind: PromptAsk}
	if err := owner.Register(id); err != nil {
		t.Fatal(err)
	}
	bound, ok := owner.BindRouting(id.PromptID, "turn-1", "runtime-1")
	if !ok || bound.TurnID != "turn-1" || bound.RuntimeEpoch != "runtime-1" {
		t.Fatalf("bound identity = %+v, %v", bound, ok)
	}
	again, ok := owner.BindRouting(id.PromptID, "turn-2", "runtime-2")
	if !ok || again != bound {
		t.Fatalf("routing identity was rewritten: first=%+v second=%+v ok=%v", bound, again, ok)
	}
}

func TestPromptAnsweredEventInheritsOwnerTurnID(t *testing.T) {
	var got event.Event
	c := New(Options{Sink: event.FuncSink(func(e event.Event) { got = e })})
	t.Cleanup(c.Close)
	c.promptOwner.Register(PromptIdentity{PromptID: "p-event", TurnID: "turn-event", Kind: PromptAsk})
	if err := c.emitTurnEventChecked(event.Event{Kind: event.PromptAnswered, ItemID: "p-event"}); err != nil {
		t.Fatal(err)
	}
	if got.TurnID != "turn-event" {
		t.Fatalf("PromptAnswered turn id = %q", got.TurnID)
	}
}
