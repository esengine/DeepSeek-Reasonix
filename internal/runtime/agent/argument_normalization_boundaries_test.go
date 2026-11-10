package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/diff"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/dispatch"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/runtime/capability"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/state/sessionstore"
)

func TestArgumentNormalizationCannotBypassShellPermission(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		target := &argumentProbeTool{name: "bash", schema: `{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`}
		reg := tool.NewRegistry()
		reg.Add(target)
		gate := &argumentProbeGate{deny: true}
		raw := `{"arguments":"{\"command\":\"rm -rf ./fixture\"}"}`
		call := provider.ToolCall{ID: "denied", Name: "bash", Arguments: raw}
		if proxy {
			reg.Add(usecap.NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), &capability.Audit{}, func() capability.Catalog { return capability.Catalog{} }))
			call.Name = "use_capability"
			call.Arguments = `{"action":"call","capability_id":"tool:bash","arguments":` + raw + `}`
		}
		a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)
		out := a.executeOne(context.Background(), &a.turn, call)
		if !out.blocked || len(target.calls) != 0 || len(gate.args) != 1 || gate.args[0] != `{"command":"rm -rf ./fixture"}` {
			t.Fatalf("proxy=%v: denied command escaped permission: gate=%v calls=%v out=%+v", proxy, gate.args, target.calls, out)
		}
	}
}

func TestArgumentNormalizationNestedCallsUsePermission(t *testing.T) {
	reg, target := argumentProbeRegistry()
	gate := &argumentProbeGate{deny: true}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)
	parent := &toolCallPlan{call: provider.ToolCall{ID: "parent"}}
	invoker := &nestedInvoker{a: a, turn: &a.turn, parent: parent}
	_, err := invoker.Invoke(context.Background(), "read_file", json.RawMessage(`{"arguments":{"path":"nested.txt"}}`))
	if err == nil || len(target.calls) != 0 || len(gate.args) != 1 || gate.args[0] != `{"path":"nested.txt"}` {
		t.Fatalf("nested normalization bypassed permission: err=%v calls=%v gate=%v", err, target.calls, gate.args)
	}
}

type argumentPreviewTool struct {
	argumentProbeTool
	previews []string
}

func (t *argumentPreviewTool) Preview(_ context.Context, args json.RawMessage) (diff.Change, error) {
	t.previews = append(t.previews, string(args))
	return diff.Change{}, nil
}

func TestArgumentNormalizationPrecedesPreviewAndScheduling(t *testing.T) {
	target := &argumentPreviewTool{argumentProbeTool: argumentProbeTool{
		name: "bash", schema: `{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`,
	}}
	reg := tool.NewRegistry()
	reg.Add(target)
	a := New(nil, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	raw := `{"arguments":{"command":"pwd"}}`
	calls := []provider.ToolCall{{Name: "bash", Arguments: raw}}
	prepared := a.prepareCallBatch(calls)
	out := a.executeOne(context.Background(), &a.turn, prepared[0])
	if out.errMsg != "" {
		t.Fatalf("prepared call failed: %+v", out)
	}
	if len(target.previews) != 1 || target.previews[0] != `{"command":"pwd"}` {
		t.Fatalf("preview did not receive effective input: %v", target.previews)
	}
	if !parallelisable(context.Background(), reg, prepared[0]) {
		t.Fatal("read-only command was not classified using effective input")
	}
	if calls[0].Arguments != raw || calls[0].ArgumentNormalization != nil {
		t.Fatal("preparation mutated the caller's slice")
	}
}

func TestArgumentNormalizationDoesNotTrustPersistedMetadata(t *testing.T) {
	reg, target := argumentProbeRegistry()
	a := New(nil, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	call := provider.ToolCall{Name: "read_file", Arguments: `{}`,
		ArgumentNormalization: &provider.ArgumentNormalization{Effective: `{"path":"unexpected.txt"}`}}
	out := a.executeOne(context.Background(), &a.turn, call)
	if len(target.calls) != 0 || out.refusalCode != refusalArgumentsInvalid {
		t.Fatalf("stored metadata authorized execution: %+v", out)
	}
}

func TestArgumentNormalizationHooksAndExecutionAgree(t *testing.T) {
	reg, target := argumentProbeRegistry()
	client := &fakeDispatchClient{}
	d := newExtDispatcher(client, false, nil, extension.PointToolBefore, extension.PointToolAfter)
	gate := &argumentProbeGate{}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate, Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		Name: "read_file", Arguments: `{"arguments":{"path":"a.txt"}}`,
	})
	var before dispatch.ToolBeforePayload
	var after dispatch.ToolAfterPayload
	if !client.interceptPayloadFor(protocol.EventToolBefore, &before) || !client.interceptPayloadFor(protocol.EventToolAfter, &after) {
		t.Fatal("tool interceptors were not invoked")
	}
	if out.errMsg != "" || len(target.calls) != 1 || before.Arguments != target.calls[0] || after.Arguments != target.calls[0] || gate.args[0] != target.calls[0] {
		t.Fatalf("hooks/permission/execution diverged: before=%+v after=%+v gate=%v calls=%v out=%+v", before, after, gate.args, target.calls, out)
	}
}

func TestArgumentNormalizationBatchPreservesCallAndResultPairing(t *testing.T) {
	reg, target := argumentProbeRegistry()
	a := New(nil, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	raw := `{"arguments":{"path":"batch.txt"}}`
	result := a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{ID: "one", Name: "read_file", Arguments: raw}})
	if len(target.calls) != 1 || len(result.results) != 1 || result.outcomes[0].errMsg != "" {
		t.Fatalf("normalized call retried or failed: calls=%v batch=%+v", target.calls, result)
	}
}

type normalizedOutputReader struct{ argumentProbeTool }

func (*normalizedOutputReader) ReadOnly() bool { return true }
func (*normalizedOutputReader) ReadTarget(args json.RawMessage) string {
	return stubReadTarget(args)
}
func (*normalizedOutputReader) Execute(context.Context, json.RawMessage) (string, error) {
	return strings.Repeat("a matching line of spilled content\n", 2000), nil
}

func TestArgumentNormalizationPreservesSpilledOutputReads(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&normalizedOutputReader{argumentProbeTool{
		name: "grep", schema: `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
	}})
	var checked json.RawMessage
	a := New(nil, reg, sessionstore.NewSession(""), Options{
		ArchiveDir: testenv.TempDir(t),
		CheckTargetAccess: func(_ context.Context, _ tool.Tool, args json.RawMessage) error {
			checked = append(json.RawMessage(nil), args...)
			return nil
		},
	}, event.Discard)
	raw, err := json.Marshal(map[string]any{"arguments": map[string]string{"path": filepath.Join(a.spillDir(), "call-1.txt")}})
	if err != nil {
		t.Fatal(err)
	}
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "fetch", Name: "grep", Arguments: string(raw)})
	if out.errMsg != "" || out.bound.Kind != event.BoundWindowed {
		t.Fatalf("normalized fetch spilled again instead of returning content: err=%s bound=%+v", out.errMsg, out.bound)
	}
	if stubReadTarget(checked) != filepath.Join(a.spillDir(), "call-1.txt") {
		t.Fatalf("file access policy did not receive the effective path: %s", checked)
	}
}
