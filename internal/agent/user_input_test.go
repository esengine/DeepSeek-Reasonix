package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type userInputCaptureProvider struct {
	request provider.Request
}

func (p *userInputCaptureProvider) Name() string { return "capture" }

func (p *userInputCaptureProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.request = req
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	close(ch)
	return ch, nil
}

func TestRunPersistsRawUserInputSeparatelyFromProviderContext(t *testing.T) {
	prov := &userInputCaptureProvider{}
	sess := NewSession("system")
	a := New(prov, tool.NewRegistry(), sess, Options{}, event.Discard)

	const raw = "fix the bug"
	const composed = "<capability-route version=\"1\">\nuse review\n</capability-route>\n\nfix the bug"
	ctx := WithRawUserInput(context.Background(), raw)
	if err := a.Run(ctx, composed); err != nil {
		t.Fatalf("Run: %v", err)
	}

	stored := sess.Snapshot()
	if len(stored) < 2 {
		t.Fatalf("stored messages = %d, want system and user", len(stored))
	}
	if got := stored[1].Content; !strings.HasPrefix(got, composed) || !strings.Contains(got, "<execution-policy") {
		t.Fatalf("stored provider content = %q, want composed %q plus execution-policy", got, composed)
	}
	if got := stored[1].RawContent; got != raw {
		t.Fatalf("stored raw content = %q, want raw %q", got, raw)
	}
	if stored[1].ProviderContent != "" {
		t.Fatalf("stored transitional provider content was not cleared: %+v", stored[1])
	}
	if len(prov.request.Messages) < 2 || !strings.HasPrefix(prov.request.Messages[1].Content, composed) {
		t.Fatalf("provider request did not receive composed context: %+v", prov.request.Messages)
	}
	if prov.request.Messages[1].RawContent != "" || prov.request.Messages[1].ProviderContent != "" {
		t.Fatalf("provider request leaked display metadata: %+v", prov.request.Messages[1])
	}

	encoded, err := json.Marshal(stored[1])
	if err != nil {
		t.Fatalf("marshal stored user turn: %v", err)
	}
	var legacy struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(encoded, &legacy); err != nil {
		t.Fatalf("decode with previous-release shape: %v", err)
	}
	if !strings.HasPrefix(legacy.Content, composed) || !strings.Contains(legacy.Content, "<execution-policy") {
		t.Fatalf("previous-release reader sees %q, want provider-visible composed prefix plus execution-policy", legacy.Content)
	}

	receipt := a.CompletionReceipt()
	if receipt == nil {
		t.Fatal("completion receipt is nil")
	}
	if len(receipt.Gaps) != 2 {
		t.Fatalf("completion gaps = %+v, want the atomic requirement and mutation check", receipt.Gaps)
	}
	if got := receipt.Gaps[0].Detail; got != "r1: "+raw {
		t.Fatalf("atomic criterion = %q, want raw user input %q", got, raw)
	}
	if strings.Contains(receipt.Gaps[0].Detail, "capability-route") {
		t.Fatalf("completion receipt leaked transient provider context: %+v", receipt.Gaps)
	}
}

func TestTransientCapabilityRouteCannotTurnConversationIntoDeliveryReceipt(t *testing.T) {
	prov := &userInputCaptureProvider{}
	a := New(prov, tool.NewRegistry(), NewSession("system"), Options{}, event.Discard)

	const raw = "请解释这个项目目前的进度"
	const composed = `<capability-route version="1">
Relevant capabilities for this turn:
- skill:minimax-docx prefer: the skill trigger matches the user request
Policy: prefer means use the skill for the required change
</capability-route>

` + raw
	if err := a.Run(WithRawUserInput(context.Background(), raw), composed); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if a.CompletionReceipt() != nil {
		t.Fatalf("an advisory turn received a delivery receipt from transient routing: %+v", a.CompletionReceipt())
	}
	if len(prov.request.Messages) < 2 ||
		!strings.Contains(prov.request.Messages[1].Content, `<capability-route version="1">`) ||
		!strings.Contains(prov.request.Messages[1].Content, raw) {
		t.Fatalf("provider lost the capability route: %+v", prov.request.Messages)
	}
	if got := a.turn.turnInput; got != raw {
		t.Fatalf("contract input = %q, want authenticated raw input %q", got, raw)
	}
	c := a.LiveContract()
	if c == nil || len(c.Requirements) != 0 || len(c.Checks) != 0 {
		t.Fatalf("transient route created delivery requirements: %+v", c)
	}
}

func TestCompletionContractUsesGoalScopeTaskText(t *testing.T) {
	prov := &userInputCaptureProvider{}
	a := New(prov, tool.NewRegistry(), NewSession("system"), Options{}, event.Discard)
	ctx := WithRawUserInput(context.Background(), "Continue working.")
	ctx = WithDeliveryExecutionScope(ctx, DeliveryExecutionScope{ID: "goal-1", TaskText: "fix the parser"})

	if err := a.Run(ctx, "<goal-context>continue</goal-context>"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertAtomicCriterion(t, a, "fix the parser")
}

func TestCompletionContractUsesPristineSubagentTaskText(t *testing.T) {
	prov := &userInputCaptureProvider{}
	a := New(prov, tool.NewRegistry(), NewSession("system"), Options{
		ClassifierTaskText: "fix the parser",
	}, event.Discard)
	const wrapped = "<workspace-context>private host framing</workspace-context>\n\nfix the parser"

	if err := a.Run(context.Background(), wrapped); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertAtomicCriterion(t, a, "fix the parser")
}

func assertAtomicCriterion(t *testing.T, a *Agent, want string) {
	t.Helper()
	receipt := a.CompletionReceipt()
	if receipt == nil || len(receipt.Gaps) == 0 {
		t.Fatalf("completion receipt = %+v, want atomic criterion %q", receipt, want)
	}
	if got := receipt.Gaps[0].Detail; got != "r1: "+want {
		t.Fatalf("atomic criterion = %q, want %q", got, want)
	}
}

func TestSubagentImageCandidatesAreCopiedAndIsolated(t *testing.T) {
	images := []string{"data:image/png;base64,AAAA"}
	ctx := WithSubagentImageCandidates(context.Background(), images)
	images[0] = "mutated"

	got := SubagentImageCandidates(ctx)
	if len(got) != 1 || got[0] != "data:image/png;base64,AAAA" {
		t.Fatalf("candidates = %v, want an isolated copy of the original image", got)
	}
	got[0] = "mutated again"
	if again := SubagentImageCandidates(ctx); again[0] != "data:image/png;base64,AAAA" {
		t.Fatalf("candidate accessor exposed mutable context state: %v", again)
	}
}
