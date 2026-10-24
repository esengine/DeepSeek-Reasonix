package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/hook"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

type lifecycleMutationHooks struct {
	stubHooks
	startHeld, stopHeld bool
}

func (*lifecycleMutationHooks) ToolMutationHooksEnabled() bool     { return false }
func (*lifecycleMutationHooks) SubagentMutationHooksEnabled() bool { return true }
func (h *lifecycleMutationHooks) SubagentStart(ctx context.Context, _ string, _ json.RawMessage) {
	h.startHeld = mutation.HoldingLease(ctx)
}
func (h *lifecycleMutationHooks) SubagentStop(ctx context.Context, _, _ string, _ error) {
	h.stopHeld = mutation.HoldingLease(ctx)
}

func TestSubagentLifecycleHooksUseSeparateMutationTransactions(t *testing.T) {
	root := t.TempDir()
	c, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	probe := &impactProbe{name: "task"}
	reg := tool.NewRegistry()
	reg.Add(describedImpactProbe{probe, tool.MutationImpact{Kind: tool.MutationReadOnly}})
	h := &lifecycleMutationHooks{}
	a := New(nil, reg, sessionstore.NewSession(""), Options{MutationCoordinator: c, WriteWorkspaceRoot: root, Hooks: h}, event.Discard)
	out := a.executeOne(t.Context(), &a.turn, provider.ToolCall{ID: "task", Name: "task", Arguments: `{"prompt":"work"}`})
	if out.errMsg != "" || !probe.called || probe.held || !h.startHeld || !h.stopHeld {
		t.Fatalf("lifecycle scope: outcome=%+v probe=%+v start=%v stop=%v", out, probe, h.startHeld, h.stopHeld)
	}
}

func TestHookRunnerDeclaresLifecycleEffectsSeparately(t *testing.T) {
	for _, event := range []hook.Event{hook.SubagentStart, hook.SubagentStop} {
		r := hook.NewRunner([]hook.ResolvedHook{{Event: event}}, t.TempDir(), nil, nil)
		if !r.SubagentMutationHooksEnabled() || r.ToolMutationHooksEnabled() {
			t.Fatalf("hook effect mixed with whole tool lifetime: %s", event)
		}
	}
}
