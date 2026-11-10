package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/runtime/capability"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/state/sessionstore"
)

type argumentProbeTool struct {
	name   string
	schema string
	calls  []string
}

func (t *argumentProbeTool) Name() string            { return t.name }
func (*argumentProbeTool) Description() string       { return "" }
func (t *argumentProbeTool) Schema() json.RawMessage { return json.RawMessage(t.schema) }
func (t *argumentProbeTool) ReadOnly() bool          { return t.name == "read_file" }
func (t *argumentProbeTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	t.calls = append(t.calls, string(args))
	return "ok", nil
}

type argumentProbeGate struct {
	args  []string
	names []string
	deny  bool
}

func (g *argumentProbeGate) Check(_ context.Context, name string, args json.RawMessage, _ bool) (bool, string, error) {
	g.args = append(g.args, string(args))
	g.names = append(g.names, name)
	return !g.deny, "test policy", nil
}

func argumentProbeRegistry() (*tool.Registry, *argumentProbeTool) {
	target := &argumentProbeTool{name: "read_file", schema: `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`}
	reg := tool.NewRegistry()
	reg.Add(target)
	return reg, target
}

func TestArgumentNormalizationPermissionAndProxy(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		for _, deny := range []bool{false, true} {
			reg, target := argumentProbeRegistry()
			gate := &argumentProbeGate{deny: deny}
			call := provider.ToolCall{ID: "wrapped", Name: "read_file", Arguments: `{"arguments":"{\"path\":\"test.txt\"}"}`}
			if proxy {
				reg.Add(usecap.NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), &capability.Audit{}, func() capability.Catalog { return capability.Catalog{} }))
				call.Name = "use_capability"
				call.Arguments = `{"action":"call","capability_id":"tool:read_file","arguments":{"arguments":"{\"path\":\"test.txt\"}"}}`
			}
			a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)
			out := a.executeOne(context.Background(), &a.turn, call)
			if len(gate.args) != 1 || gate.args[0] != `{"path":"test.txt"}` || gate.names[0] != "read_file" {
				t.Fatalf("proxy=%v deny=%v: gate saw %v %v; outcome=%+v", proxy, deny, gate.names, gate.args, out)
			}
			if deny {
				if len(target.calls) != 0 || !out.blocked {
					t.Fatalf("denied repaired call executed: %+v", out)
				}
			} else if len(target.calls) != 1 || target.calls[0] != gate.args[0] || out.errMsg != "" {
				t.Fatalf("execution and permission diverged: calls=%v out=%+v", target.calls, out)
			}
		}
	}
}

func TestArgumentNormalizationHistoryRoundTrip(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "proxy"}[proxy], func(t *testing.T) {
			testArgumentNormalizationHistoryRoundTrip(t, proxy)
		})
	}
}

func testArgumentNormalizationHistoryRoundTrip(t *testing.T, proxy bool) {
	t.Helper()
	raw := `{"arguments":"{\"path\":\"test.txt\"}"}`
	name := "read_file"
	reg, target := argumentProbeRegistry()
	if proxy {
		reg.Add(usecap.NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), &capability.Audit{}, func() capability.Catalog { return capability.Catalog{} }))
		name = "use_capability"
		raw = `{"action":"call","capability_id":"tool:read_file","arguments":` + raw + `}`
	}
	prov := testutil.NewMock("test",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "wrapped", Name: name, Arguments: raw, ThoughtSignature: "opaque-proof"}}},
		testutil.Turn{Text: "done"},
	)
	session := sessionstore.NewSession("stable system")
	a := New(prov, reg, session, Options{}, event.Discard)
	if err := a.Run(context.Background(), "read test.txt"); err != nil {
		t.Fatal(err)
	}
	if len(target.calls) != 1 || target.calls[0] != `{"path":"test.txt"}` {
		t.Fatalf("tool calls = %v", target.calls)
	}
	req := prov.LastRequest()
	found := false
	for _, m := range req.Messages {
		for _, call := range m.ToolCalls {
			found = true
			if call.Arguments != raw || call.ThoughtSignature != "opaque-proof" {
				t.Fatalf("provider history changed: %+v", call)
			}
		}
	}
	if !found {
		t.Fatal("provider did not receive original call")
	}
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range loaded.Snapshot() {
		for _, call := range m.ToolCalls {
			if call.Arguments != raw || call.ArgumentNormalization == nil {
				t.Fatalf("lost original/effective arguments on reload: %+v", call)
			}
			effective := json.RawMessage(call.ExecutionArguments())
			if proxy {
				proxyTool, _ := reg.Get("use_capability")
				effective = proxyTool.(tool.TargetArgsReader).TargetArgs(effective)
				if call.ResolvedName != "read_file" {
					t.Fatalf("lost resolved proxy target: %+v", call)
				}
			}
			if string(effective) != `{"path":"test.txt"}` {
				t.Fatalf("lost effective target arguments: %s", effective)
			}
		}
	}
	visible, err := json.Marshal(provider.ModelMessages(loaded.Snapshot()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(visible), "argument_normalization") {
		t.Fatal("local normalization metadata leaked to provider")
	}
}
