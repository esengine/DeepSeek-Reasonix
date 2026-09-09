package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func opaqueRecoveryError() error {
	return &provider.APIError{Provider: "strict-replay", Status: 400, Body: `{"model":"deepseek-v4-pro"}`}
}
func recoveryTestAgent(turns ...testutil.Turn) (*Agent, *testutil.MockProvider) {
	p := testutil.NewMock("strict-replay", turns...)
	return New(strictAssistantReasoningProvider{p}, echoRegistry(), reasoningReplaySeededSession(), Options{}, event.Discard), p
}
func TestProtocolRecoveryManualAndRestart(t *testing.T) {
	a, p := recoveryTestAgent(testutil.ErrorTurn(opaqueRecoveryError()), testutil.ErrorTurn(opaqueRecoveryError()), testutil.ErrorTurn(thinkingReplay400Error()))
	if err := a.Run(withNoClosedLoop(context.Background()), "next"); err == nil {
		t.Fatal("expected upstream error")
	}
	pending := a.PendingProtocolRecovery()
	if pending == nil || p.CallCount() != 1 {
		t.Fatalf("pending=%v requests=%d", pending, p.CallCount())
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := a.Session().Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	a = New(strictAssistantReasoningProvider{p}, echoRegistry(), loaded, Options{}, event.Discard)
	if a.PendingProtocolRecovery() == nil {
		t.Fatal("restart lost pending action")
	}
	ctx := WithInputMessageOrigin(WithProtocolRecovery(withNoClosedLoop(context.Background()), pending.ID), provider.MessageOriginHost)
	if err := a.Run(ctx, "recover"); err == nil {
		t.Fatal("expected second upstream error")
	}
	if p.CallCount() != 2 || a.PendingProtocolRecovery() != nil {
		t.Fatal("manual recovery renewed its budget")
	}
	for _, m := range p.Requests()[1].Messages {
		if m.ReasoningContent != "" || len(m.ProtocolRecovery) > 0 {
			t.Fatal("repair or local metadata projection failed")
		}
	}
	if err := a.Session().Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	a = New(strictAssistantReasoningProvider{p}, echoRegistry(), loaded, Options{}, event.Discard)
	if err := a.Run(ctx, "repeat"); !errors.Is(err, ErrProtocolRecoveryUnavailable) {
		t.Fatalf("repeat=%v", err)
	}
	if err := a.Run(withNoClosedLoop(context.Background()), "continue"); err == nil {
		t.Fatal("expected protocol rejection")
	}
	if p.CallCount() != 3 {
		t.Fatalf("restart renewed budget: %d", p.CallCount())
	}
	for _, m := range p.Requests()[2].Messages {
		if m.ReasoningContent != "" {
			t.Fatal("restart lost repaired view")
		}
	}
}
func TestProtocolRecoveryEligibilityAndStaleness(t *testing.T) {
	for _, err := range []error{&provider.APIError{Status: 401}, &provider.APIError{Status: 400, Body: `{"error":"invalid temperature"}`}} {
		a, _ := recoveryTestAgent(testutil.ErrorTurn(err))
		_ = a.Run(withNoClosedLoop(context.Background()), "next")
		if a.PendingProtocolRecovery() != nil {
			t.Fatal("nonopaque failure offered repair")
		}
	}
	a, p := recoveryTestAgent(testutil.ErrorTurn(opaqueRecoveryError()))
	_ = a.Run(withNoClosedLoop(context.Background()), "next")
	id := a.PendingProtocolRecovery().ID
	ctx, cancel := context.WithCancel(WithProtocolRecovery(context.Background(), id))
	cancel()
	if err := a.Run(ctx, "recover"); !errors.Is(err, ErrProtocolRecoveryUnavailable) {
		t.Fatal(err)
	}
	if a.PendingProtocolRecovery() == nil {
		t.Fatal("preparation cancellation consumed repair")
	}
	a.Session().Add(provider.Message{Role: provider.RoleUser, Content: "different task"})
	if a.PendingProtocolRecovery() != nil {
		t.Fatal("new input did not stale token")
	}
	if err := a.Run(WithProtocolRecovery(context.Background(), id), "recover"); !errors.Is(err, ErrProtocolRecoveryUnavailable) {
		t.Fatal(err)
	}
	if p.CallCount() != 1 {
		t.Fatal("stale action invoked provider")
	}
}
func TestProtocolRecoveryUnknownFieldsAndVersion(t *testing.T) {
	a, _ := recoveryTestAgent(testutil.ErrorTurn(opaqueRecoveryError()))
	_ = a.Run(withNoClosedLoop(context.Background()), "next")
	r, _ := a.latestProtocolRecord()
	raw, _ := json.Marshal(r)
	raw = append(raw[:len(raw)-1], []byte(`,"future":{"keep":true}}`)...)
	a.Session().storeProtocolRecord(r.ID, raw)
	r.State = "consumed"
	if err := a.saveProtocolRecord(r); err != nil {
		t.Fatal(err)
	}
	for _, m := range a.Session().Snapshot() {
		if len(m.ProtocolRecovery) > 0 && !strings.Contains(string(m.ProtocolRecovery), `"future"`) {
			t.Fatal("lost unknown field")
		}
	}
	a.Session().Add(provider.Message{LocalOnly: true, ProtocolRecovery: json.RawMessage(`{"version":99,"id":"future"}`)})
	if a.PendingProtocolRecovery() != nil {
		t.Fatal("unknown version actionable")
	}
	b, _ := json.Marshal(a.Session().Snapshot())
	var round []provider.Message
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(round[len(round)-1].ProtocolRecovery), `99`) {
		t.Fatal("unknown version lost")
	}
}

type protocolCheckpointSink struct {
	fail    bool
	records int
}

func (s *protocolCheckpointSink) Emit(event.Event) {}
func (s *protocolCheckpointSink) EmitChecked(e event.Event) error {
	if e.RecoveryCheckpoint {
		s.records++
		if s.fail {
			return errors.New("checkpoint unavailable")
		}
	}
	return nil
}
func TestProtocolRecoveryCheckpointBeforeRequest(t *testing.T) {
	a, p := recoveryTestAgent(testutil.ErrorTurn(opaqueRecoveryError()), testutil.Turn{Text: "should not run"})
	sink := &protocolCheckpointSink{}
	a.svc.sink = sink
	_ = a.Run(withNoClosedLoop(context.Background()), "next")
	id := a.PendingProtocolRecovery().ID
	sink.fail = true
	ctx := WithInputMessageOrigin(WithProtocolRecovery(context.Background(), id), provider.MessageOriginHost)
	if err := a.Run(ctx, "recover"); err == nil || !strings.Contains(err.Error(), "checkpoint") {
		t.Fatalf("error=%v", err)
	}
	if p.CallCount() != 1 || sink.records != 2 {
		t.Fatalf("requests=%d checkpoints=%d", p.CallCount(), sink.records)
	}
}

type lateProtocolProvider struct {
	strictAssistantReasoningProvider
	entered, release chan struct{}
	count            int
}

func (p *lateProtocolProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.count++
	if p.count == 1 {
		return p.MockProvider.Stream(ctx, req)
	}
	close(p.entered)
	<-p.release
	call := provider.ToolCall{ID: "late", Name: "echo", Arguments: `{"text":"must not execute"}`}
	ch := make(chan provider.Chunk, 4)
	ch <- provider.Chunk{Type: provider.ChunkReasoning, Text: "proof"}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "late response"}
	ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &call}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
func TestProtocolRecoveryCancellationDiscardsLateResponseAndTool(t *testing.T) {
	p := &lateProtocolProvider{strictAssistantReasoningProvider: strictAssistantReasoningProvider{testutil.NewMock("strict-replay", testutil.ErrorTurn(opaqueRecoveryError()))}, entered: make(chan struct{}), release: make(chan struct{})}
	sink := &recordSink{}
	a := New(p, echoRegistry(), reasoningReplaySeededSession(), Options{}, sink)
	_ = a.Run(withNoClosedLoop(context.Background()), "next")
	pending := a.PendingProtocolRecovery()
	if pending == nil {
		t.Fatal("no pending recovery")
	}
	ctx, cancel := context.WithCancel(WithInputMessageOrigin(WithProtocolRecovery(context.Background(), pending.ID), provider.MessageOriginHost))
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, "recover") }()
	<-p.entered
	cancel()
	close(p.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if len(sink.kinds(event.ToolDispatch)) != 0 {
		t.Fatal("late tool started")
	}
	for _, m := range a.Session().Snapshot() {
		if m.Content == "late response" {
			t.Fatal("late assistant committed")
		}
	}
	if a.PendingProtocolRecovery() != nil {
		t.Fatal("cancelled request renewed repair budget")
	}
}

func TestProtocolRecoveryPendingTracksLocalExecutionEvidence(t *testing.T) {
	a, _ := recoveryTestAgent(testutil.ErrorTurn(opaqueRecoveryError()))
	a.Session().AddBatch(
		provider.Message{Role: provider.RoleAssistant, ReasoningContent: "old reasoning", ToolCalls: []provider.ToolCall{{ID: "receipt", Name: "echo", Arguments: `{}`}}},
		provider.Message{Role: provider.RoleTool, ToolCallID: "receipt", Name: "echo", Content: "done", ToolRunState: provider.ToolRunCompleted},
	)
	_ = a.Run(withNoClosedLoop(context.Background()), "next")
	if a.PendingProtocolRecovery() == nil {
		t.Fatal("missing initial action")
	}
	a.Session().mu.Lock()
	changed := false
	for i := range a.Session().Messages {
		m := &a.Session().Messages[i]
		if m.Role == provider.RoleTool && !m.LocalOnly {
			m.ToolRunState = provider.ToolRunUnknown
			changed = true
			break
		}
	}
	a.Session().mu.Unlock()
	if !changed {
		t.Fatal("fixture has no tool result")
	}
	if a.PendingProtocolRecovery() != nil {
		t.Fatal("changed execution evidence left action usable")
	}
}
