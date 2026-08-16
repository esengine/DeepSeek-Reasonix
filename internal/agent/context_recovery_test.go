package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type budgetTestTool struct{}

func (budgetTestTool) Name() string        { return "budget_fixture" }
func (budgetTestTool) Description() string { return "Budget recovery request fixture." }
func (budgetTestTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
}
func (budgetTestTool) ReadOnly() bool { return true }
func (budgetTestTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}

func sameProviderRequestExceptMaxTokens(a, b provider.Request) bool {
	a.MaxTokens = 0
	b.MaxTokens = 0
	return reflect.DeepEqual(a, b)
}

type scriptedBudgetProvider struct {
	mu     sync.Mutex
	policy provider.ContextBudgetPolicy
	errs   []error
	reqs   []provider.Request
	texts  []string
}

func (p *scriptedBudgetProvider) Name() string { return "scripted-budget" }
func (p *scriptedBudgetProvider) ContextBudgetPolicy() provider.ContextBudgetPolicy {
	return p.policy
}
func (p *scriptedBudgetProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	idx := len(p.reqs) - 1
	var err error
	if idx < len(p.errs) {
		err = p.errs[idx]
	}
	text := "ok"
	if idx < len(p.texts) && p.texts[idx] != "" {
		text = p.texts[idx]
	}
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: text}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func issue8909Limit() *provider.ContextLimitError {
	return &provider.ContextLimitError{
		APIError:         &provider.APIError{Provider: "p", Status: 400, Body: "context"},
		WindowTokens:     1_048_576,
		RequestedTokens:  1_165_351,
		PromptTokens:     810_882,
		CompletionTokens: 354_469,
	}
}

func newBudgetAgent(t *testing.T, p provider.Provider) *Agent {
	t.Helper()
	sess := NewSession("")
	sess.Replace([]provider.Message{{Role: provider.RoleUser, Content: "continue"}})
	registry := tool.NewRegistry()
	registry.Add(budgetTestTool{})
	return New(p, registry, sess, Options{ContextWindow: 1_048_576, CompactRatio: 2, MaxOutputTokens: 0, Temperature: 0.25}, event.Discard)
}

func TestContextLimitRecoveryChangesOnlyOutputField(t *testing.T) {
	prov := &scriptedBudgetProvider{
		policy: provider.ContextBudgetPolicy{
			WindowMode: provider.ContextWindowShared, AutoOutputTokens: 384_000,
			MaxOutputTokens: 384_000, LimitMode: provider.OutputLimitOmitWhenSafe,
		},
		errs: []error{issue8909Limit(), nil},
	}
	a := newBudgetAgent(t, prov)
	a.sess.conversation.Replace([]provider.Message{
		{
			Role: provider.RoleAssistant, Content: "tool preface", ReasoningContent: "provider reasoning",
			ReasoningSignature: "reasoning-signature", ReasoningID: "reasoning-id", ReasoningStatus: "completed",
			ToolCalls:      []provider.ToolCall{{ID: "call-1", Name: "budget_fixture", Arguments: `{"q":"status"}`, ThoughtSignature: "thought-signature"}},
			ResponsesItems: []json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"item-1"}`)},
			ServerSearch: []provider.ServerSearchCall{{
				ID: "search-1", Query: "context budgets",
				Results: []provider.ServerSearchHit{{Title: "Result", URL: "https://example.test"}},
				Raw:     json.RawMessage(`{"query":"context budgets"}`),
			}},
		},
		{Role: provider.RoleTool, Name: "budget_fixture", ToolCallID: "call-1", Content: "done"},
		{Role: provider.RoleUser, Content: "continue", Images: []string{"data:image/png;base64,AA=="}},
	})
	beforeMessages := a.sess.conversation.Snapshot()
	got := a.streamWithSamplingRecovery(WithResponseFormat(context.Background(), "json_object"), 1)
	if got.err != nil {
		t.Fatalf("recovery failed: %v", got.err)
	}
	prov.mu.Lock()
	defer prov.mu.Unlock()
	if len(prov.reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(prov.reqs))
	}
	if !sameProviderRequestExceptMaxTokens(prov.reqs[0], prov.reqs[1]) {
		t.Fatalf("provider request changed outside MaxTokens:\nfirst=%+v\nretry=%+v", prov.reqs[0], prov.reqs[1])
	}
	if prov.reqs[1].MaxTokens != 229_502 {
		t.Fatalf("retry MaxTokens = %d, want 229502", prov.reqs[1].MaxTokens)
	}
	if a.lastAdmission().LastRecovery != contextRecoveryLearnedRetry {
		t.Fatalf("last recovery = %s", a.lastAdmission().LastRecovery)
	}
	budget := a.ContextMaintenanceSnapshot().ContextBudget
	if budget == nil {
		t.Fatal("missing context budget snapshot after learned retry")
	}
	if budget.Source != provider.ContextBudgetSourceLearned || budget.WindowMode != provider.ContextWindowShared.String() {
		t.Fatalf("retry source/window = %s/%s, want learned/shared", budget.Source, budget.WindowMode)
	}
	if budget.RequestedOutputTokens != 384_000 || budget.EffectiveOutputTokens != 229_502 || budget.PhysicalRemaining != 229_502 || !budget.Clipped {
		t.Fatalf("retry budget = %+v, want requested=384000 effective=physical=229502 clipped", budget)
	}
	if budget.ObservedWindow != 1_048_576 || budget.ObservedPrompt != 810_882 || budget.ObservedCompletion != 354_469 {
		t.Fatalf("retry observations = %+v", budget)
	}
	if after := a.sess.conversation.Snapshot(); !reflect.DeepEqual(after, beforeMessages) {
		t.Fatalf("recovery mutated the transcript:\nbefore=%+v\nafter=%+v", beforeMessages, after)
	}
}

func TestContextLimitRecoveryPublishesUnknownGatewayBudget(t *testing.T) {
	limit := &provider.ContextLimitError{
		APIError:         &provider.APIError{Provider: "compatible", Status: 400, Body: "context"},
		WindowTokens:     20_000,
		RequestedTokens:  25_000,
		PromptTokens:     10_000,
		CompletionTokens: 15_000,
	}
	prov := &scriptedBudgetProvider{
		policy: provider.ContextBudgetPolicy{WindowMode: provider.ContextWindowUnknown, LimitMode: provider.OutputLimitOmitWhenSafe},
		errs:   []error{limit, nil},
	}
	a := newBudgetAgent(t, prov)
	got := a.streamWithSamplingRecovery(context.Background(), 1)
	if got.err != nil {
		t.Fatalf("unknown gateway recovery failed: %v", got.err)
	}
	prov.mu.Lock()
	if len(prov.reqs) != 2 || prov.reqs[0].MaxTokens != 0 || prov.reqs[1].MaxTokens != 1_808 {
		t.Fatalf("unknown gateway requests = %+v, want omitted then 1808", prov.reqs)
	}
	prov.mu.Unlock()
	budget := a.ContextMaintenanceSnapshot().ContextBudget
	if budget == nil {
		t.Fatal("missing learned unknown-gateway budget")
	}
	if budget.Source != provider.ContextBudgetSourceLearned || budget.WindowMode != provider.ContextWindowShared.String() ||
		budget.AutoOutputTokens != 15_000 || budget.RequestedOutputTokens != 15_000 ||
		budget.EffectiveOutputTokens != 1_808 || budget.PhysicalRemaining != 1_808 || !budget.Clipped ||
		budget.LastRecovery != contextRecoveryLearnedRetry {
		t.Fatalf("unknown gateway retry budget = %+v", budget)
	}
}

func TestContextLimitRecoveryStopsAfterBudgetAndCompact(t *testing.T) {
	limit := issue8909Limit()
	limit.PromptTokens = 1_040_000
	limit.CompletionTokens = 20_000
	limit.RequestedTokens = 1_060_000
	prov := &scriptedBudgetProvider{
		policy: provider.ContextBudgetPolicy{
			WindowMode: provider.ContextWindowShared, AutoOutputTokens: 384_000,
			LimitMode: provider.OutputLimitOmitWhenSafe,
		},
		errs: []error{limit, limit, limit},
	}
	a := newBudgetAgent(t, prov)
	got := a.streamWithSamplingRecovery(context.Background(), 1)
	if got.err == nil {
		t.Fatal("expected terminal context overflow")
	}
	if a.lastAdmission().LastRecovery != contextRecoveryFailed {
		t.Fatalf("last recovery = %s, want failed", a.lastAdmission().LastRecovery)
	}
	if provider.AsContextLimitError(got.err) == nil && !errors.Is(got.err, ErrCompactionRequired) {
		t.Fatalf("terminal err = %v", got.err)
	}
}

func TestContextBudgetLearnAndSnapshotRace(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}, sess: sessionRuntime{conversation: NewSession("")}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 200 {
			a.learnContextBudget(1_000_000-i, 1000+i, true)
			a.setLastRecovery(contextRecoveryLearnedRetry)
			_ = a.ContextMaintenanceSnapshot()
			_ = a.effectiveContextWindow()
		}
	}()
	for range 200 {
		a.learnContextBudget(900_000, 2000, true)
		_ = a.ContextMaintenanceSnapshot()
		_ = a.lastAdmission()
	}
	<-done
}

func TestThreeStateMaxOutputTokens(t *testing.T) {
	prov := &policyWindowProvider{policy: provider.ContextBudgetPolicy{
		WindowMode: provider.ContextWindowShared, AutoOutputTokens: 384_000,
		MaxOutputTokens: 384_000, LimitMode: provider.OutputLimitOmitWhenSafe,
	}}
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}, svc: agentServices{prov: prov}}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	pos := provider.Request{Messages: msgs, MaxTokens: 8192}
	if err := a.applyAdmissionToRequest(&pos); err != nil || pos.MaxTokens != 8192 {
		t.Fatalf("positive cap = %d err=%v", pos.MaxTokens, err)
	}
	zero := provider.Request{Messages: msgs, MaxTokens: 0}
	if err := a.applyAdmissionToRequest(&zero); err != nil || zero.MaxTokens != 0 {
		t.Fatalf("auto omit = %d err=%v", zero.MaxTokens, err)
	}
	neg := provider.Request{Messages: msgs, MaxTokens: -1}
	if err := a.applyAdmissionToRequest(&neg); err != nil || neg.MaxTokens != -1 {
		t.Fatalf("explicit omit = %d err=%v", neg.MaxTokens, err)
	}
}
