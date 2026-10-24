package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/builtin"
	"reasonix/internal/tools/script"
)

type mutationGateFunc func(context.Context, string, json.RawMessage, bool) (bool, string, error)

func (f mutationGateFunc) Check(ctx context.Context, name string, args json.RawMessage, readOnly bool) (bool, string, error) {
	return f(ctx, name, args, readOnly)
}

type transactionProbeHooks struct {
	stubHooks
	unprotected bool
	calls       int
}

func (h *transactionProbeHooks) PreToolUse(ctx context.Context, _ string, _ json.RawMessage) (bool, string) {
	h.calls++
	h.unprotected = h.unprotected || !mutation.HoldingLease(ctx)
	return false, ""
}
func (h *transactionProbeHooks) PostToolUse(ctx context.Context, _ string, _ json.RawMessage, _ string) {
	h.calls++
	h.unprotected = h.unprotected || !mutation.HoldingLease(ctx)
}

func TestScriptNestedApprovalDoesNotHoldWorkspaceLease(t *testing.T) {
	root := t.TempDir()
	coordinator, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ws := builtin.Workspace{Dir: root}
	reg := tool.NewRegistry()
	reg.Add(script.New())
	reg.Add(ws.Tools("write_file")[0])
	checked := false
	gate := mutationGateFunc(func(ctx context.Context, name string, _ json.RawMessage, _ bool) (bool, string, error) {
		if name != "write_file" {
			return true, "", nil
		}
		checked = true
		if mutation.HoldingLease(ctx) {
			return false, "", fmt.Errorf("approval inherited a script lease")
		}
		wait, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		lease, err := coordinator.Acquire(wait, mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{filepath.Join(root, "file.txt")}})
		if err != nil {
			return false, "", err
		}
		lease.End(mutation.NotExecuted)
		return true, "", nil
	})
	hooks := &transactionProbeHooks{}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate, Hooks: hooks, MutationCoordinator: coordinator, WriteWorkspaceRoot: root, CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
	args, _ := json.Marshal(map[string]string{"script": `print(call("write_file",path="file.txt",content="created"))`})
	out := a.executeOne(t.Context(), &a.turn, provider.ToolCall{ID: "script", Name: "run_script", Arguments: string(args)})
	if !checked || out.errMsg != "" {
		t.Fatalf("nested approval: checked=%v outcome=%+v", checked, out)
	}
	if hooks.calls != 4 || hooks.unprotected {
		t.Fatalf("hook transactions: %+v", hooks)
	}
}
