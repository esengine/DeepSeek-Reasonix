package control

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

type scopedTool struct{ name, scope string }

func (s scopedTool) Name() string                                           { return s.name }
func (scopedTool) Description() string                                      { return "" }
func (scopedTool) Schema() json.RawMessage                                  { return json.RawMessage(`{}`) }
func (scopedTool) ReadOnly() bool                                           { return false }
func (s scopedTool) ApprovalScope() string                                  { return s.scope }
func (scopedTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }

// A tool whose approval covers more than the call names that on the request,
// with the arguments it covers; a plain tool's request carries neither, and
// the wire form sends the arguments only with a scope.
func TestApprovalRequestCarriesTheToolsDeclaredScope(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(scopedTool{name: "fan_out", scope: tool.ApprovalScopeUnattendedAttempts})
	reg.Add(scopedTool{name: "plain"})
	approvals := make(chan event.Approval, 2)
	c := New(Options{Registry: reg, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			approvals <- e.Approval
		}
	})})
	args := json.RawMessage(`{"prompt":"add Median","n":2}`)
	for _, name := range []string{"fan_out", "plain"} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _, _ = gateApprover{c}.Approve(context.Background(), name, "", args)
		}()
		var a event.Approval
		select {
		case a = <-approvals:
		case <-time.After(30 * time.Second):
			t.Fatalf("%s: no approval request", name)
		}
		c.Approve(a.ID, false, false, false)
		<-done
		want := ""
		if name == "fan_out" {
			want = tool.ApprovalScopeUnattendedAttempts
		}
		if a.Scope != want {
			t.Fatalf("%s: scope = %q, want %q", name, a.Scope, want)
		}
	}
}
