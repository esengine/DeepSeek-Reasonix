package control

import (
	"context"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/mcpinteraction"
)

type interactionProbeSink struct {
	mu           sync.Mutex
	interactions []event.MCPInteraction
	answered     []string
}

func (s *interactionProbeSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e.Kind {
	case event.MCPInteractionRequest:
		s.interactions = append(s.interactions, e.MCPInteraction)
	case event.PromptAnswered:
		s.answered = append(s.answered, e.ItemID)
	}
}

func (s *interactionProbeSink) snapshot() (interactions []event.MCPInteraction, answered []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.MCPInteraction(nil), s.interactions...), append([]string(nil), s.answered...)
}

func sampleInteractionRequest() mcpinteraction.Request {
	return mcpinteraction.Request{
		Server: "github", Mode: mcpinteraction.ModeForm, Message: "approve the OAuth device code",
		RequestedSchema: []byte(`{"type":"object","properties":{"code":{"type":"string"}},"required":["code"]}`),
	}
}

func TestInteractEmitsEventAndAnswerResolves(t *testing.T) {
	sink := &interactionProbeSink{}
	c := New(Options{Sink: sink, SessionDir: t.TempDir()})

	type reply struct {
		res mcpinteraction.Result
		err error
	}
	done := make(chan reply, 1)
	go func() {
		res, err := c.Interact(t.Context(), sampleInteractionRequest())
		done <- reply{res, err}
	}()

	var id string
	deadline := time.After(2 * time.Second)
	for {
		interactions, _ := sink.snapshot()
		if len(interactions) == 1 {
			id = interactions[0].ID
			break
		}
		select {
		case <-deadline:
			t.Fatal("MCPInteractionRequest never emitted")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	interactions, _ := sink.snapshot()
	if interactions[0].Server != "github" || interactions[0].Mode != "form" {
		t.Fatalf("event payload = %+v", interactions[0])
	}

	if err := c.AnswerMCPInteractionChecked(id, mcpinteraction.ActionAccept, map[string]any{"code": "123-456"}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Interact error: %v", r.err)
		}
		if r.res.Action != mcpinteraction.ActionAccept || r.res.Content["code"] != "123-456" {
			t.Fatalf("result = %+v", r.res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Interact never returned after answer")
	}
	// The durable answer transition was persisted before the waiter released.
	if _, answered := sink.snapshot(); len(answered) == 0 {
		t.Fatal("PromptAnswered not emitted for the elicitation")
	}
}

func TestInteractDeclineClearsContent(t *testing.T) {
	sink := &interactionProbeSink{}
	c := New(Options{Sink: sink, SessionDir: t.TempDir()})

	done := make(chan mcpinteraction.Result, 1)
	go func() {
		res, _ := c.Interact(t.Context(), sampleInteractionRequest())
		done <- res
	}()
	var id string
	deadline := time.After(2 * time.Second)
	for {
		interactions, _ := sink.snapshot()
		if len(interactions) == 1 {
			id = interactions[0].ID
			break
		}
		select {
		case <-deadline:
			t.Fatal("event never emitted")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if err := c.AnswerMCPInteractionChecked(id, mcpinteraction.ActionDecline, map[string]any{"code": "ignored"}); err != nil {
		t.Fatalf("decline: %v", err)
	}
	select {
	case res := <-done:
		if res.Action != mcpinteraction.ActionDecline || res.Content != nil {
			t.Fatalf("decline result = %+v, want no content", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Interact never returned")
	}
}

func TestInteractInvalidActionRejected(t *testing.T) {
	c := New(Options{Sink: &interactionProbeSink{}, SessionDir: t.TempDir()})
	if err := c.AnswerMCPInteractionChecked("1", "guess", nil); err == nil {
		t.Fatal("invalid action accepted")
	}
}

func TestInteractCancelledContextCancels(t *testing.T) {
	sink := &interactionProbeSink{}
	c := New(Options{Sink: sink, SessionDir: t.TempDir()})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan mcpinteraction.Result, 1)
	go func() {
		res, _ := c.Interact(ctx, sampleInteractionRequest())
		done <- res
	}()
	deadline := time.After(2 * time.Second)
	for {
		if interactions, _ := sink.snapshot(); len(interactions) == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("event never emitted")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	select {
	case res := <-done:
		if res.Action != mcpinteraction.ActionCancel {
			t.Fatalf("cancelled action = %q, want cancel", res.Action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Interact never returned after cancellation")
	}
}

func TestInteractReplaysAfterFrontendReconnect(t *testing.T) {
	sink := &interactionProbeSink{}
	c := New(Options{Sink: sink, SessionDir: t.TempDir()})
	done := make(chan mcpinteraction.Result, 1)
	go func() {
		res, _ := c.Interact(t.Context(), sampleInteractionRequest())
		done <- res
	}()
	deadline := time.After(2 * time.Second)
	var id string
	for {
		interactions, _ := sink.snapshot()
		if len(interactions) == 1 {
			id = interactions[0].ID
			break
		}
		select {
		case <-deadline:
			t.Fatal("event never emitted")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	replay := &interactionProbeSink{}
	c.ReplayPendingPromptsTo(replay)
	interactions, _ := replay.snapshot()
	if len(interactions) != 1 || interactions[0].ID != id {
		t.Fatalf("replay = %+v, want the pending elicitation", interactions)
	}
	_ = c.AnswerMCPInteractionChecked(id, mcpinteraction.ActionCancel, nil)
	<-done
}
