package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"reasonix/internal/workspacelease"
)

type workspaceLeaseTestTool struct {
	name     string
	readOnly bool
	calls    atomic.Int32
}

type workspaceLeaseTestHooks struct{ preCalls atomic.Int32 }
type workspaceLeaseDenyGate struct{}

func (workspaceLeaseDenyGate) Check(context.Context, string, json.RawMessage, bool) (bool, string, error) {
	return false, "test denial", nil
}

func (h *workspaceLeaseTestHooks) PreToolUse(context.Context, string, json.RawMessage) (bool, string) {
	h.preCalls.Add(1)
	return false, ""
}
func (*workspaceLeaseTestHooks) PostToolUse(context.Context, string, json.RawMessage, string) {}
func (*workspaceLeaseTestHooks) PostToolUseFailure(context.Context, string, json.RawMessage, string, error) {
}
func (*workspaceLeaseTestHooks) PostLLMCall(_ context.Context, reasoning string, _ int) string {
	return reasoning
}
func (*workspaceLeaseTestHooks) HasPostLLMCall() bool                      { return false }
func (*workspaceLeaseTestHooks) SubagentStop(context.Context, string)      {}
func (*workspaceLeaseTestHooks) PreCompact(context.Context, string) string { return "" }

func (t *workspaceLeaseTestTool) Name() string        { return t.name }
func (t *workspaceLeaseTestTool) Description() string { return t.name }
func (t *workspaceLeaseTestTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *workspaceLeaseTestTool) ReadOnly() bool { return t.readOnly }
func (t *workspaceLeaseTestTool) Execute(context.Context, json.RawMessage) (string, error) {
	t.calls.Add(1)
	return "ok", nil
}

func deliveryLeaseTestAgent(t *testing.T, owner *workspacelease.Owner, tools ...tool.Tool) *Agent {
	t.Helper()
	reg := tool.NewRegistry()
	for _, candidate := range tools {
		reg.Add(candidate)
	}
	a := New(nil, reg, NewSession(""), Options{DeliveryProfile: true, WorkspaceLease: owner}, event.Discard)
	a.deliveryCriteriaEstablished = true
	a.setTodoState([]evidence.TodoItem{{Content: "mutate", Status: "in_progress"}})
	return a
}

func TestDeliveryWriterWaitsBeforeToolExecutionButReaderDoesNot(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	first, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.BeginRun()
	if err := first.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer first.EndRun()

	reader := &workspaceLeaseTestTool{name: "lease_reader", readOnly: true}
	writer := &workspaceLeaseTestTool{name: "lease_writer", readOnly: false}
	a := deliveryLeaseTestAgent(t, second, reader, writer)
	second.BeginRun()
	defer second.EndRun()

	if outcome := a.executeOne(context.Background(), providerToolCall("read", reader.Name())); outcome.errMsg != "" {
		t.Fatalf("reader was blocked by another Delivery writer: %+v", outcome)
	}
	if got := reader.calls.Load(); got != 1 {
		t.Fatalf("reader calls = %d, want 1", got)
	}

	hooks := &workspaceLeaseTestHooks{}
	a.hooks = hooks
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	outcome := a.executeOne(ctx, providerToolCall("write", writer.Name()))
	if !outcome.blocked || outcome.errMsg != "blocked: workspace write lease unavailable" {
		t.Fatalf("writer outcome = %+v, want lease block", outcome)
	}
	if got := writer.calls.Load(); got != 0 {
		t.Fatalf("writer executed %d times before lease acquisition", got)
	}
	if got := hooks.preCalls.Load(); got != 0 {
		t.Fatalf("PreToolUse ran %d times before lease acquisition", got)
	}
}

func TestDeniedDeliveryWriterDoesNotAcquireWorkspaceLease(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	deniedOwner, _ := workspacelease.New(root, locks, nil)
	probeOwner, _ := workspacelease.New(root, locks, nil)
	writer := &workspaceLeaseTestTool{name: "denied_writer"}
	a := deliveryLeaseTestAgent(t, deniedOwner, writer)
	a.gate = workspaceLeaseDenyGate{}
	deniedOwner.BeginRun()
	outcome := a.executeOne(context.Background(), providerToolCall("write", writer.Name()))
	deniedOwner.EndRun()
	if !outcome.blocked || outcome.errMsg != "blocked by permission policy" {
		t.Fatalf("denied outcome = %+v", outcome)
	}
	if writer.calls.Load() != 0 {
		t.Fatal("denied writer executed")
	}
	probeOwner.BeginRun()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := probeOwner.AcquireWrite(ctx); err != nil {
		t.Fatalf("permission denial leaked workspace lease: %v", err)
	}
	probeOwner.EndRun()
}

func providerToolCall(id, name string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: name, Arguments: `{}`}
}
