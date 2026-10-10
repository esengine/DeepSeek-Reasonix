package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	sessionstore "reasonix/internal/state/sessionstore"
)

type delegatingTarget struct {
	readOnlyBoundaryTarget
	atExecute func()
}

func (delegatingTarget) ResolveProfile(json.RawMessage) *event.Profile {
	return &event.Profile{Name: "explore", Count: 1}
}

func (d delegatingTarget) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	d.atExecute()
	return d.readOnlyBoundaryTarget.Execute(ctx, args)
}

func TestProxiedDelegationIsMarkedBeforeItRuns(t *testing.T) {
	var events []event.Event
	var profiledAtExecute bool
	target := delegatingTarget{
		readOnlyBoundaryTarget: readOnlyBoundaryTarget{name: "explore", readOnly: true},
		atExecute: func() {
			for _, e := range events {
				if e.Kind == event.ToolDispatch && e.Tool.Profile != nil {
					profiledAtExecute = true
				}
			}
		},
	}
	reg := tool.NewRegistry()
	reg.Add(readOnlyBoundaryProxy{resolved: tool.ResolvedCall{
		ProxyAction: "call", CapabilityID: "subagent:explore", TargetName: target.Name(),
		Target: target, ReadOnly: true, Args: json.RawMessage(`{"task":"x"}`),
	}})
	call := provider.ToolCall{ID: "d1", Name: "use_capability", Arguments: `{"action":"call","capability_id":"subagent:explore"}`}
	session := sessionstore.NewSession("sys")
	session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{call}})
	a := New(nil, reg, session, Options{}, event.FuncSink(func(e event.Event) { events = append(events, e) }))

	a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{call})

	if !profiledAtExecute {
		t.Fatal("no dispatch carried the delegation profile by the time the sub-agent started running")
	}
}
