package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	a := New(nil, reg, NewSession(""), Options{WorkspaceLease: owner}, event.Discard)
	a.turn.deliveryCriteriaEstablished = true
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

	if outcome := a.executeOne(context.Background(), &a.turn, providerToolCall("read", reader.Name())); outcome.errMsg != "" {
		t.Fatalf("reader was blocked by another Delivery writer: %+v", outcome)
	}
	if got := reader.calls.Load(); got != 1 {
		t.Fatalf("reader calls = %d, want 1", got)
	}

	hooks := &workspaceLeaseTestHooks{}
	a.svc.hooks = hooks
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	outcome := a.executeOne(ctx, &a.turn, providerToolCall("write", writer.Name()))
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
	a.svc.setGate(workspaceLeaseDenyGate{})
	deniedOwner.BeginRun()
	outcome := a.executeOne(context.Background(), &a.turn, providerToolCall("write", writer.Name()))
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

func TestReadOnlyBashDoesNotTakeWorkspaceLease(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	holder, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	readerOwner, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	holder.BeginRun()
	if err := holder.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer holder.EndRun()
	bash := &workspaceLeaseTestTool{name: "bash"}
	a := deliveryLeaseTestAgent(t, readerOwner, bash)
	readerOwner.BeginRun()
	defer readerOwner.EndRun()
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		ID:        "st",
		Name:      "bash",
		Arguments: `{"command":"git status"}`,
	})
	if out.blocked || out.errMsg != "" {
		t.Fatalf("read-only bash was blocked by a writer: %+v", out)
	}
}

func TestPathWriteReleasesLeaseAfterToolReturns(t *testing.T) {
	repo, locks := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := workspacelease.New(repo, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := workspacelease.New(repo, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	w1 := &recordingWriter{name: "write_file"}
	w2 := &recordingWriter{name: "write_file"}
	reg1 := tool.NewRegistry()
	reg1.Add(w1)
	a1 := New(nil, reg1, NewSession(""), Options{WorkspaceLease: first, WriteWorkspaceRoot: repo}, event.Discard)
	a1.turn.deliveryCriteriaEstablished = true
	a1.setTodoState([]evidence.TodoItem{{Content: "mutate", Status: "in_progress"}})
	reg2 := tool.NewRegistry()
	reg2.Add(w2)
	a2 := New(nil, reg2, NewSession(""), Options{WorkspaceLease: second, WriteWorkspaceRoot: repo}, event.Discard)
	a2.turn.deliveryCriteriaEstablished = true
	a2.setTodoState([]evidence.TodoItem{{Content: "mutate", Status: "in_progress"}})
	first.BeginRun()
	second.BeginRun()
	defer first.EndRun()
	defer second.EndRun()
	out1 := a1.executeOne(context.Background(), &a1.turn, provider.ToolCall{
		ID: "w1", Name: "write_file",
		Arguments: string(mustJSON(t, map[string]string{"path": filepath.Join(repo, "a.go"), "content": "a"})),
	})
	if out1.blocked || out1.errMsg != "" {
		t.Fatalf("first write: %+v", out1)
	}
	out2 := a2.executeOne(context.Background(), &a2.turn, provider.ToolCall{
		ID: "w2", Name: "write_file",
		Arguments: string(mustJSON(t, map[string]string{"path": filepath.Join(repo, "b.go"), "content": "b"})),
	})
	if out2.blocked || out2.errMsg != "" {
		t.Fatalf("second write after first tool returned: %+v", out2)
	}
}

func providerToolCall(id, name string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: name, Arguments: `{}`}
}

func TestNestedRepoWriteFileLeasesDoNotBlock(t *testing.T) {
	parent, locks := t.TempDir(), t.TempDir()
	repoA := filepath.Join(parent, "A")
	repoB := filepath.Join(parent, "B")
	for _, repo := range []string{repoA, repoB} {
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	first, err := workspacelease.New(parent, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := workspacelease.New(parent, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	w1 := &recordingWriter{name: "write_file"}
	w2 := &recordingWriter{name: "write_file"}
	reg1 := tool.NewRegistry()
	reg1.Add(w1)
	a1 := New(nil, reg1, NewSession(""), Options{
		WorkspaceLease: first, WriteWorkspaceRoot: parent,
	}, event.Discard)
	a1.turn.deliveryCriteriaEstablished = true
	a1.setTodoState([]evidence.TodoItem{{Content: "mutate", Status: "in_progress"}})
	reg2 := tool.NewRegistry()
	reg2.Add(w2)
	a2 := New(nil, reg2, NewSession(""), Options{
		WorkspaceLease: second, WriteWorkspaceRoot: parent,
	}, event.Discard)
	a2.turn.deliveryCriteriaEstablished = true
	a2.setTodoState([]evidence.TodoItem{{Content: "mutate", Status: "in_progress"}})
	first.BeginRun()
	second.BeginRun()
	defer first.EndRun()
	defer second.EndRun()

	out1 := a1.executeOne(context.Background(), &a1.turn, provider.ToolCall{
		ID:   "w1",
		Name: "write_file",
		Arguments: string(mustJSON(t, map[string]string{
			"path": filepath.Join(repoA, "a.go"), "content": "a",
		})),
	})
	if out1.blocked || out1.errMsg != "" {
		t.Fatalf("first nested write: %+v", out1)
	}
	out2 := a2.executeOne(context.Background(), &a2.turn, provider.ToolCall{
		ID:   "w2",
		Name: "write_file",
		Arguments: string(mustJSON(t, map[string]string{
			"path": filepath.Join(repoB, "b.go"), "content": "b",
		})),
	})
	if out2.blocked || out2.errMsg != "" {
		t.Fatalf("second nested write should not wait on the first: %+v", out2)
	}
}
