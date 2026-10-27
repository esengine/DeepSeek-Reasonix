package guardian

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/state/sessionstore"
)

type boundaryTarget struct {
	name                   string
	readOnly, hostMutation bool
	calls                  atomic.Int64
}

func (p *boundaryTarget) Name() string                        { return p.name }
func (*boundaryTarget) Description() string                   { return "Guardian boundary probe" }
func (*boundaryTarget) Schema() json.RawMessage               { return json.RawMessage(`{"type":"object"}`) }
func (p *boundaryTarget) ReadOnly() bool                      { return p.readOnly }
func (p *boundaryTarget) ReadOnlyExecutionHostMutation() bool { return p.hostMutation }
func (p *boundaryTarget) Execute(context.Context, json.RawMessage) (string, error) {
	p.calls.Add(1)
	return "probe executed", nil
}

type boundaryProvider struct {
	calls    []provider.ToolCall
	requests []provider.Request
}

func (*boundaryProvider) Name() string { return "guardian-boundary" }
func (p *boundaryProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	n := len(p.requests)
	p.requests = append(p.requests, req)
	ch := make(chan provider.Chunk, 2)
	if n < len(p.calls) {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &p.calls[n]}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"review completed"}`}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestGuardianRechecksDeferredExecutionEffects(t *testing.T) {
	for _, kind := range []string{"writer", "host_mutation", "reader"} {
		t.Run(kind, func(t *testing.T) {
			root := tool.NewRegistry()
			reader := &boundaryTarget{name: "reader", readOnly: true}
			target := &boundaryTarget{name: "target", readOnly: kind != "writer", hostMutation: kind == "host_mutation"}
			root.Add(reader)
			root.Add(target)
			root.Add(usecap.NewUseCapabilityTool(t.Context(), nil, nil, root, nil, nil, nil))
			reg := agent.FilterReadOnlyRegistry(root, agent.SubagentMetaTools()...)
			call := func(id, name string) provider.ToolCall {
				args, _ := json.Marshal(map[string]any{"action": "call", "capability_id": "tool:" + name, "arguments": map[string]any{}})
				return provider.ToolCall{ID: id, Name: "use_capability", Arguments: string(args)}
			}
			p := &boundaryProvider{calls: []provider.ToolCall{call("read", "reader"), call("effect", "target")}}
			gs := NewSession(p, reg, nil, PolicyPrompt(), "guardian-boundary", 0, nil, event.Discard, nil)
			allow, _, err := gs.ReviewVerdict(t.Context(), "write_file", json.RawMessage(`{"path":"out.txt","content":"requested"}`), sessionstore.NewSession("executor"))
			if err != nil || !allow {
				t.Fatalf("review did not finish after checking effects: allow=%v, err=%v", allow, err)
			}
			if reader.calls.Load() != 1 {
				t.Fatalf("benign reader calls=%d, want 1", reader.calls.Load())
			}
			want := int64(0)
			if kind == "reader" {
				want = 1
			}
			if target.calls.Load() != want {
				t.Fatalf("%s executed %d times, want %d", kind, target.calls.Load(), want)
			}
			if len(p.requests) != 3 {
				t.Fatalf("review requests=%d, want 3", len(p.requests))
			}
			var last string
			for _, m := range p.requests[2].Messages {
				if m.Role == provider.RoleTool {
					last = m.Content
				}
			}
			if kind != "reader" && !strings.Contains(last, "read-only agent cannot") {
				t.Fatalf("missing effect refusal: %q", last)
			}
			if kind == "reader" && !strings.Contains(last, "probe executed") {
				t.Fatalf("reader was refused: %q", last)
			}
		})
	}
}
