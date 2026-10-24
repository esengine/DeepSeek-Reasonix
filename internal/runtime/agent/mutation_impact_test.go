package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/builtin"
)

type impactProbe struct {
	name         string
	held, called bool
}

func TestReadOnlyBashPermissionDoesNotEraseMutationImpact(t *testing.T) {
	for _, child := range []bool{false, true} {
		t.Run(fmt.Sprintf("child=%v", child), func(t *testing.T) { runBashPermissionMutationCase(t, child) })
	}
}

func runBashPermissionMutationCase(t *testing.T, child bool) {
	root, metadata := t.TempDir(), t.TempDir()
	waiting := make(chan struct{}, 1)
	c, err := mutation.New(root, t.TempDir(), func(n mutation.Notice) {
		if n.State == "waiting" {
			waiting <- struct{}{}
		}
	}, mutation.WithWorkspaceResources(metadata))
	if err != nil {
		t.Fatal(err)
	}
	held, err := c.Acquire(t.Context(), mutation.Intent{Effect: mutation.GitMetadata, Paths: []string{metadata}})
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(mutation.NotExecuted)
	ws := builtin.Workspace{Dir: root}
	reg := tool.NewRegistry()
	for _, target := range ws.Tools("bash") {
		if child {
			target = foregroundOnlyBash{inner: target}
		}
		reg.Add(target)
	}
	a := New(nil, reg, sessionstore.NewSession(""), Options{MutationCoordinator: c, WriteWorkspaceRoot: root, CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan toolOutcome, 1)
	go func() {
		done <- a.executeOne(ctx, &a.turn, provider.ToolCall{ID: "status", Name: "bash", Arguments: `{"command":"git status --short"}`})
	}()
	select {
	case <-waiting:
	case out := <-done:
		t.Fatalf("permission classification bypassed metadata scope: %+v", out)
	case <-time.After(5 * time.Second):
		t.Fatal("missing opaque scope wait")
	}
	cancel()
	if out := <-done; !out.blocked {
		t.Fatalf("cancelled read-classified Bash ran: %+v", out)
	}
}

func (p *impactProbe) Name() string          { return p.name }
func (*impactProbe) Description() string     { return "Inspect the concrete execution transaction." }
func (*impactProbe) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*impactProbe) ReadOnly() bool          { return false }
func (p *impactProbe) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	p.called, p.held = true, mutation.HoldingLease(ctx)
	return "done", nil
}

func TestMutationCoordinationDoesNotUseEvidenceToolNames(t *testing.T) {
	for _, name := range []string{"bash", "task", "run_script", "custom_writer"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			c, err := mutation.New(root, t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			probe := &impactProbe{name: name}
			reg := tool.NewRegistry()
			reg.Add(probe)
			a := New(nil, reg, sessionstore.NewSession(""), Options{MutationCoordinator: c, WriteWorkspaceRoot: root}, event.Discard)
			out := a.executeOne(t.Context(), &a.turn, provider.ToolCall{ID: "probe", Name: name, Arguments: `{"command":"go test ./..."}`})
			if out.errMsg != "" || !probe.called || !probe.held {
				t.Fatalf("concrete writer bypassed coordination: probe=%+v outcome=%+v", probe, out)
			}
		})
	}
}

type describedImpactProbe struct {
	*impactProbe
	impact tool.MutationImpact
}

func (p describedImpactProbe) DescribeMutation(context.Context, json.RawMessage) (tool.MutationImpact, error) {
	return p.impact, nil
}

func TestNonFileImpactPreservesWriteAuthorizationWithoutFileLease(t *testing.T) {
	root := t.TempDir()
	c, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []tool.MutationKind{tool.MutationReadOnly, tool.MutationExternal} {
		t.Run(string(kind), func(t *testing.T) {
			probe := &impactProbe{name: "delegate_effect"}
			reg := tool.NewRegistry()
			reg.Add(describedImpactProbe{probe, tool.MutationImpact{Kind: kind}})
			checked := false
			gate := mutationGateFunc(func(_ context.Context, _ string, _ json.RawMessage, readOnly bool) (bool, string, error) {
				checked = true
				if readOnly {
					return false, "", fmt.Errorf("coordination classification widened permission")
				}
				return true, "", nil
			})
			a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate, MutationCoordinator: c, WriteWorkspaceRoot: root}, event.Discard)
			out := a.executeOne(t.Context(), &a.turn, provider.ToolCall{ID: "probe", Name: probe.name, Arguments: `{}`})
			if out.errMsg != "" || !checked || !probe.called || probe.held {
				t.Fatalf("scope and authority coupled: checked=%v probe=%+v out=%+v", checked, probe, out)
			}
		})
	}
}
