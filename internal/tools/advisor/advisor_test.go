package advisor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

type fixedTranscript []provider.Message

func (f fixedTranscript) Transcript() []provider.Message { return f }

type adviceStub struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (s *adviceStub) Name() string { return "advice-stub" }

func (s *adviceStub) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "Check the lock order in store.go first."}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 42}}
	close(ch)
	return ch, nil
}

type usageSink struct{ events []event.Event }

func (s *usageSink) Emit(e event.Event) { s.events = append(s.events, e) }

func TestRenderTranscriptKeepsTheTaskAndTheNewestWork(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "SYSTEM PROMPT"},
		{Role: provider.RoleUser, Content: "fix the deadlock"},
	}
	for i := range 40 {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c", Name: "read_file", Arguments: `{"path":"store.go"}`}}},
			provider.Message{Role: provider.RoleTool, Name: "read_file", Content: strings.Repeat("x", 1000) + string(rune('A'+i%26))},
		)
	}
	msgs = append(msgs, provider.Message{Role: provider.RoleAssistant, Content: "partial stream", LocalOnly: true})
	got := renderTranscript(msgs, 20*1024)
	if strings.Contains(got, "SYSTEM PROMPT") || strings.Contains(got, "partial stream") {
		t.Fatal("rendered a message the model was never sent")
	}
	if !strings.Contains(got, "## User\n\nfix the deadlock") {
		t.Fatal("the task statement was dropped")
	}
	if !strings.Contains(got, "earlier messages left out") || !strings.Contains(got, `→ read_file({"path":"store.go"})`) {
		t.Fatalf("expected an omission marker and the newest calls:\n%s", got[:min(len(got), 600)])
	}
	if !strings.HasSuffix(strings.TrimSpace(got), strings.Repeat("x", 1000)+"N") {
		t.Fatal("the newest result is not last")
	}
	if len(got) > 20*1024+200 {
		t.Fatalf("rendered %d bytes against a 20 KiB budget", len(got))
	}
}

func TestAdviseSendsTheConversationAndBillsTheAdvisor(t *testing.T) {
	stub, sink := &adviceStub{}, &usageSink{}
	adv := New(Spec{Provider: stub, ModelRef: "strong/pro", Sink: sink})
	ctx := tool.WithTranscriptReader(t.Context(), fixedTranscript{{Role: provider.RoleUser, Content: "fix the deadlock"}})
	out, err := adv.Execute(ctx, json.RawMessage(`{"question":"Which lock should go first?"}`))
	if err != nil || out != "Check the lock order in store.go first." {
		t.Fatalf("Execute = %q, %v", out, err)
	}
	if len(stub.reqs) != 1 || len(stub.reqs[0].Tools) != 0 {
		t.Fatalf("advisor requests = %d, want one request with no tools", len(stub.reqs))
	}
	evidence := stub.reqs[0].Messages[len(stub.reqs[0].Messages)-1].Content
	if !strings.Contains(evidence, "fix the deadlock") || !strings.Contains(evidence, "Which lock should go first?") {
		t.Fatalf("the advisor did not see the task and the question:\n%s", evidence)
	}
	if len(sink.events) != 1 || sink.events[0].UsageSource != event.UsageSourceAdvisor || sink.events[0].ModelRef != "strong/pro" {
		t.Fatalf("usage events = %+v, want one billed to the advisor model", sink.events)
	}
}

func TestAdviseRefusesWithoutABoundConversation(t *testing.T) {
	_, err := New(Spec{Provider: &adviceStub{}}).Execute(t.Context(), json.RawMessage(`{"question":"q"}`))
	if !errors.Is(err, ErrNoTranscript) {
		t.Fatalf("err = %v, want ErrNoTranscript", err)
	}
	if _, err := New(Spec{Provider: &adviceStub{}}).Execute(t.Context(), json.RawMessage(`{"question":"  "}`)); err == nil {
		t.Fatal("an empty question was sent")
	}
}
