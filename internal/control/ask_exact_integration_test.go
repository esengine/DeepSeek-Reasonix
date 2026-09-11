package control

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
)

func TestAskExactResolutionDeliversOnceAfterReplay(t *testing.T) {
	dir := t.TempDir()
	asks := make(chan event.Event, 1)
	answers := make(chan []event.AskAnswer, 1)
	release := make(chan struct{})
	c := New(Options{
		SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl"),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				asks <- e
			}
		}),
	})
	t.Cleanup(func() { c.Cancel(); waitIdle(t, c); c.Close() })
	c.SetTurnEventRoutingMetadata("runtime-ask", "")
	c.runGuarded(func(ctx context.Context) error {
		got, err := c.Ask(ctx, askProbeQuestions())
		if err != nil {
			return err
		}
		answers <- got
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})
	var request event.Event
	select {
	case request = <-asks:
	case <-time.After(5 * time.Second):
		t.Fatal("Ask was not published")
	}
	identity := PromptIdentity{PromptID: request.Ask.ID, TurnID: request.TurnID, RuntimeEpoch: "runtime-ask", Kind: PromptAsk}
	if identity.TurnID == "" || identity.TurnID != c.RuntimeStatus().TurnID {
		t.Fatalf("Ask has no current turn identity: %+v", identity)
	}
	var replay event.Event
	c.ReplayPendingPromptsTo(event.FuncSink(func(e event.Event) { replay = e }))
	if replay.Ask.ID != identity.PromptID || replay.TurnID != identity.TurnID {
		t.Fatalf("replayed Ask changed identity: %+v", replay)
	}
	want := []event.AskAnswer{{QuestionID: request.Ask.Questions[0].ID, Selected: []string{"custom answer"}}}
	answer := PromptAnswer{Questions: want}
	stale := identity
	stale.RuntimeEpoch = "old-runtime"
	if err := c.ResolvePromptExact(stale, answer); !errors.Is(err, ErrPromptStaleRuntime) {
		t.Fatalf("stale runtime = %v", err)
	}
	stale = identity
	stale.TurnID = "old-turn"
	if err := c.ResolvePromptExact(stale, answer); !errors.Is(err, ErrPromptStaleTurn) {
		t.Fatalf("stale turn = %v", err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { <-start; results <- c.ResolvePromptExact(identity, answer) })
	}
	close(start)
	wg.Wait()
	close(results)
	var succeeded, duplicate int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrPromptAlreadyResolved):
			duplicate++
		default:
			t.Fatalf("current Ask answer rejected: %v", err)
		}
	}
	if succeeded != 1 || duplicate != 1 {
		t.Fatalf("answer results: %d successes, %d duplicates", succeeded, duplicate)
	}
	select {
	case got := <-answers:
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Ask returned %+v, want %+v", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("accepted answer did not unblock Ask")
	}
	if pending := c.PendingPromptIdentities(); len(pending) != 0 {
		t.Fatalf("resolved Ask remains pending: %+v", pending)
	}
	records, err := c.TurnEventsAfter(0)
	if err != nil {
		t.Fatal(err)
	}
	answered := 0
	for _, record := range records {
		if record.Kind == "prompt_answered" {
			answered++
		}
	}
	if answered != 1 {
		t.Fatalf("durable answers = %d, want exactly one", answered)
	}
	close(release)
	waitIdle(t, c)
}

func TestAskExactSkipCancelsTurn(t *testing.T) {
	dir := t.TempDir()
	asks := make(chan event.Event, 1)
	done := make(chan event.Event, 1)
	c := New(Options{
		SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl"),
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.AskRequest:
				asks <- e
			case event.TurnDone:
				done <- e
			}
		}),
	})
	t.Cleanup(func() { c.Cancel(); waitIdle(t, c); c.Close() })
	c.runner = &askBlockingRunner{c: c}
	c.SetTurnEventRoutingMetadata("runtime-skip", "")
	c.Send("ask user")
	var request event.Event
	select {
	case request = <-asks:
	case <-time.After(5 * time.Second):
		t.Fatal("Ask was not published")
	}
	identity := PromptIdentity{PromptID: request.Ask.ID, TurnID: request.TurnID, RuntimeEpoch: "runtime-skip", Kind: PromptAsk}
	if err := c.ResolvePromptExact(identity, PromptAnswer{}); err != nil {
		t.Fatalf("skip Ask: %v", err)
	}
	if terminal := waitTurnDoneEvent(t, done); !terminal.Cancelled {
		t.Fatalf("empty answer did not cancel the turn: %+v", terminal)
	}
	waitIdle(t, c)
	if c.PendingPrompt() || len(c.PendingPromptIdentities()) != 0 {
		t.Fatal("skipped Ask remains pending")
	}
}
