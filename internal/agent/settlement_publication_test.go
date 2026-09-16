package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// This recorder models the acceptance boundary, independently of the display
// sink. Tests observe it at publication time rather than inspecting the fully
// populated conversation after Run has returned.
type settlementRecorder struct {
	mu       sync.Mutex
	accepted map[string]provider.Message
	reject   error
}

func (*settlementRecorder) CheckpointSession(ctx context.Context, _ SessionCheckpointBoundary) error {
	return ctx.Err()
}

func (r *settlementRecorder) RecordSessionMessages(_ context.Context, _ string, messages []provider.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, message := range messages {
		if message.Role == provider.RoleAssistant && r.reject != nil {
			return r.reject
		}
	}
	for _, message := range messages {
		r.accepted[message.ID] = message
	}
	return nil
}

func (r *settlementRecorder) message(id string) (provider.Message, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	message, exists := r.accepted[id]
	return message, exists
}

func TestAssistantSettlementPublicationFollowsBusinessAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name  string
		turns []testutil.Turn
	}{
		{name: "answer", turns: []testutil.Turn{{Text: "final answer", Reasoning: "final reasoning"}}},
		{name: "reasoning-only", turns: []testutil.Turn{{Reasoning: "reasoning-only result"}}},
		{name: "tool-bearing", turns: []testutil.Turn{
			{Text: "checking", Reasoning: "tool reasoning", ToolCalls: []provider.ToolCall{{ID: "echo-1", Name: "echo", Arguments: `{"text":"one"}`}}},
			{Text: "done"},
		}},
		{name: "tool-only", turns: []testutil.Turn{
			{ToolCalls: []provider.ToolCall{{ID: "echo-1", Name: "echo", Arguments: `{"text":"one"}`}}},
			{Text: "done"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &settlementRecorder{accepted: make(map[string]provider.Message)}
			messages, commits := make(map[string]int), make(map[string]int)
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind != event.Message && !(e.Kind == event.StreamAttempt && e.StreamAttempt.Action == event.StreamAttemptCommit) {
					return
				}
				accepted, exists := recorder.message(e.MessageID)
				if !exists || accepted.Role != provider.RoleAssistant {
					t.Errorf("settlement kind=%v attempt=%q published before assistant business acceptance", e.Kind, e.AttemptID)
					return
				}
				if e.AttemptID != accepted.ID {
					t.Errorf("settlement identity changed: attempt=%q accepted=%q", e.AttemptID, accepted.ID)
				}
				if e.Kind == event.Message {
					messages[accepted.ID]++
					if e.Text != accepted.Content || e.Reasoning != accepted.ReasoningContent {
						t.Errorf("published message differs from accepted body: text=%q reasoning=%q", e.Text, e.Reasoning)
					}
				} else {
					commits[accepted.ID]++
				}
			})
			p := testutil.NewMock("test", tc.turns...)
			a := New(p, echoRegistry(), NewSession("system"), Options{SessionCheckpointer: recorder}, sink)
			if err := a.Run(withNoClosedLoop(t.Context()), "question"); err != nil {
				t.Fatal(err)
			}
			assistantCount := 0
			for _, message := range a.Session().Snapshot() {
				if message.Role != provider.RoleAssistant {
					continue
				}
				assistantCount++
				if commits[message.ID] != 1 {
					t.Errorf("assistant %q has %d commit publications, want one", message.ID, commits[message.ID])
				}
				wantMessages := 0
				if message.Content != "" || message.ReasoningContent != "" {
					wantMessages = 1
				}
				if messages[message.ID] != wantMessages {
					t.Errorf("assistant %q has %d complete-message publications, want %d", message.ID, messages[message.ID], wantMessages)
				}
			}
			if assistantCount != len(tc.turns) {
				t.Errorf("accepted assistants=%d, want %d", assistantCount, len(tc.turns))
			}
		})
	}
}

func TestAssistantAcceptanceFailureDoesNotPublishSuccessfulSettlement(t *testing.T) {
	rejected := errors.New("assistant commit unavailable")
	recorder := &settlementRecorder{accepted: make(map[string]provider.Message), reject: rejected}
	streamed := false
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Text {
			streamed = true
		}
		if e.Kind == event.Message || e.Kind == event.StreamAttempt && e.StreamAttempt.Action == event.StreamAttemptCommit {
			t.Errorf("rejected assistant published successful settlement kind=%v", e.Kind)
		}
	})
	a := New(testutil.NewMock("test", testutil.Turn{Text: "visible partial answer"}), echoRegistry(), NewSession("system"), Options{SessionCheckpointer: recorder}, sink)
	if err := a.Run(withNoClosedLoop(t.Context()), "question"); !errors.Is(err, rejected) {
		t.Fatalf("Run error=%v, want acceptance failure", err)
	}
	if !streamed {
		t.Fatal("fixture did not emit the partial answer before commit failed")
	}
}
