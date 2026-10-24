package agent

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessionstore"
)

type childHostProbe struct {
	escape                   sandbox.EscapeApprover
	config                   tool.ConfigWriteApprover
	background, paid, elicit bool
}

func (*childHostProbe) Name() string            { return "inspect_host_context" }
func (*childHostProbe) Description() string     { return "Inspect execution-local host collaborators." }
func (*childHostProbe) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*childHostProbe) ReadOnly() bool          { return true }
func (p *childHostProbe) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	p.escape, _ = sandbox.EscapeApproverFrom(ctx)
	p.config, _ = tool.ConfigWriteApproverFrom(ctx)
	_, p.background = tool.BackgroundStarterFrom(ctx)
	_, p.paid = tool.PaidSearchApproverFrom(ctx)
	_, p.elicit = tool.ElicitorFrom(ctx)
	return "inspected", nil
}

type childHostApprover struct{ identity string }

func (*childHostApprover) ApproveSandboxEscape(context.Context, sandbox.EscapeRequest) (bool, string, error) {
	return true, "test", nil
}
func (*childHostApprover) ApproveManagedConfigWrite(context.Context, tool.ConfigWriteRequest) (bool, string, error) {
	return true, "test", nil
}
func (*childHostApprover) ApprovePaidSearch(context.Context, tool.PaidSearchRequest) (bool, error) {
	return true, nil
}
func (*childHostApprover) Elicit(context.Context, tool.ElicitRequest) (tool.ElicitReply, error) {
	return tool.ElicitReply{}, nil
}

func TestChildHostCapabilitiesComeOnlyFromChildServices(t *testing.T) {
	for _, bindLocal := range []bool{false, true} {
		name := "none"
		if bindLocal {
			name = "local"
		}
		t.Run(name, func(t *testing.T) {
			parent, local := &childHostApprover{identity: "parent"}, &childHostApprover{identity: "local"}
			ctx := tool.WithBackgroundStarter(t.Context(), func(string, string, func(context.Context, io.Writer) (string, error)) string { return "parent-job" })
			ctx = sandbox.WithEscapeApprover(ctx, parent)
			ctx = tool.WithConfigWriteApprover(ctx, parent)
			ctx = tool.WithPaidSearchApprover(ctx, parent)
			ctx = tool.WithElicitor(ctx, parent)
			probe := &childHostProbe{}
			registry := tool.NewRegistry()
			registry.Add(probe)
			p := &scriptedProvider{name: "child-host", turns: [][]provider.Chunk{
				{toolCallChunk("probe", probe.Name(), `{}`), {Type: provider.ChunkDone}},
				{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
			}}
			opts := Options{MaxSteps: 5, SubagentDepth: 1}
			if bindLocal {
				opts.SandboxEscapeApprover, opts.ConfigWriteApprover = local, local
			}
			if _, err := RunSubAgentWithSession(ctx, p, registry, sessionstore.NewSession("sys"), "inspect", opts, event.Discard); err != nil {
				t.Fatal(err)
			}
			if probe.background || probe.paid || probe.elicit {
				t.Fatalf("child inherited parent services: %+v", probe)
			}
			if bindLocal {
				if probe.escape != local || probe.config != local {
					t.Fatal("child-local services were lost")
				}
			} else if probe.escape != nil || probe.config != nil {
				t.Fatal("child inherited parent approval grants")
			}
		})
	}
}
