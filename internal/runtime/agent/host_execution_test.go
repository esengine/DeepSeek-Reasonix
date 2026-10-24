package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/dispatch"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/safety/evidence"
)

type hostCheckProbe struct {
	calls       int
	panicOnCall bool
}

func (*hostCheckProbe) Name() string            { return "bash" }
func (*hostCheckProbe) Description() string     { return "test check" }
func (*hostCheckProbe) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*hostCheckProbe) ReadOnly() bool          { return false }
func (*hostCheckProbe) ExecutionDescriptor(json.RawMessage) *tool.ShellExecution {
	return &tool.ShellExecution{Kind: "shell"}
}
func (*hostCheckProbe) Execute(context.Context, json.RawMessage) (string, error) {
	panic("detailed execution required")
}
func (p *hostCheckProbe) ExecuteDetailed(context.Context, json.RawMessage) (tool.DetailedResult, error) {
	p.calls++
	if p.panicOnCall {
		panic("fixture check panic")
	}
	return tool.DetailedResult{Output: "ok", Execution: &tool.ShellExecution{Kind: "shell", State: tool.ShellStateCompleted, ExitCode: new(0), Verification: tool.ShellVerificationPassed}}, nil
}

func TestHostExecutorDoesNotAcceptMaskedVerificationFailure(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(scriptedShell{ex: tool.ShellExecution{Kind: "shell", State: tool.ShellStateCompleted, ExitCode: new(0), PipeStatus: []int{1, 0}}})
	host := NewHostExecutor(reg, Options{}, event.Discard)
	defer host.Close()
	result := host.Execute(t.Context(), provider.ToolCall{Name: "bash", Arguments: `{"command":"go test ./... | tee output.txt"}`})
	if result.VerificationSucceeded() {
		t.Fatalf("failed pipeline verifier accepted as success: %+v", result)
	}
	if result.Executed && (result.Execution == nil || result.Execution.Verification != tool.ShellVerificationFailed) {
		t.Fatalf("pipeline failure lost typed verdict: %+v", result)
	}
}

func TestHostExecutorDoesNotInheritDeliveryEvidenceScope(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&hostCheckProbe{})
	host := NewHostExecutor(reg, Options{}, event.Discard)
	defer host.Close()
	parent := evidence.NewLedger()
	parent.Record(evidence.Receipt{ToolName: "parent-only", Success: true})
	ctx := evidence.WithLedger(WithDeliveryExecutionScope(t.Context(), DeliveryExecutionScope{ID: "parent", TaskText: "source task"}), parent)
	ctx = beginExecutionContext(WithRunExecution(ctx, "parent-execution"))
	ids := map[string]bool{}
	for range 2 {
		result := host.Execute(ctx, provider.ToolCall{Name: "bash", Arguments: `{"command":"go test ./..."}`})
		if !result.VerificationSucceeded() || len(result.Evidence.Receipts) != 1 || result.ExecutionID == "" || result.ExecutionID == "parent-execution" || ids[result.ExecutionID] {
			t.Fatalf("check reused parent or previous check evidence: %+v", result)
		}
		if result.Evidence.Receipts[0].SourceExecutionID != result.ExecutionID {
			t.Fatal("check receipt lacks host execution identity")
		}
		ids[result.ExecutionID] = true
	}
	if len(parent.Summary().Receipts) != 1 || host.agent.task.scopeID != "" {
		t.Fatal("host check changed source delivery evidence")
	}
}

func TestHostExecutorPanicClosesResultAndCanFinish(t *testing.T) {
	reg := tool.NewRegistry()
	probe := &hostCheckProbe{panicOnCall: true}
	reg.Add(probe)
	sink := &recordSink{}
	host := NewHostExecutor(reg, Options{}, sink)
	defer host.Close()
	result := host.Execute(t.Context(), provider.ToolCall{Name: "bash", Arguments: `{"command":"go test ./..."}`})
	if !result.Executed || result.Error == "" || result.Execution == nil || result.Execution.MutationRisk != tool.ShellMutationUnknown || result.VerificationSucceeded() {
		t.Fatalf("check panic lost unknown outcome: %+v", result)
	}
	if events := sink.kinds(event.ToolResult); len(events) != 1 || events[0].Tool.ID != result.CallID {
		t.Fatal("check panic left an open tool event")
	}
	probe.panicOnCall = false
	if next := host.Execute(t.Context(), provider.ToolCall{Name: "bash", Arguments: `{"command":"go test ./..."}`}); !next.VerificationSucceeded() {
		t.Fatalf("check panic left executor busy: %+v", next)
	}
}

func TestHostExecutorUsesTypedResultPermissionAndHooks(t *testing.T) {
	for _, blocked := range []string{"", "permission", "hook", "target"} {
		t.Run(blocked, func(t *testing.T) {
			probe := &hostCheckProbe{}
			reg := tool.NewRegistry()
			reg.Add(probe)
			gate, hooks := &stubGate{}, &stubHooks{}
			if blocked == "permission" {
				gate.deny = map[string]bool{"bash": true}
			}
			if blocked == "hook" {
				hooks.blockPre = map[string]bool{"bash": true}
			}
			sink := &recordSink{}
			host := NewHostExecutor(reg, Options{Gate: gate, Hooks: hooks, CheckTargetAccess: func(context.Context, tool.Tool, json.RawMessage) error {
				if blocked == "target" {
					return tool.Refusal{Code: "fixture.denied", Message: "target denied"}
				}
				return nil
			}}, sink)
			defer host.Close()
			result := host.Execute(t.Context(), provider.ToolCall{Name: "bash", Arguments: `{"command":"go test ./..."}`})
			if blocked != "" {
				if probe.calls != 0 || result.Executed || result.Error == "" {
					t.Fatalf("host check bypassed %s: %+v", blocked, result)
				}
				return
			}
			if probe.calls != 1 || !result.Executed || result.Error != "" || result.Execution == nil || result.Execution.ExitCode == nil || *result.Execution.ExitCode != 0 {
				t.Fatalf("host execution lost typed result: %+v", result)
			}
			if len(hooks.preSeen) == 0 || len(hooks.postSeen) == 0 || len(gate.checked) == 0 || len(result.Evidence.Receipts) == 0 {
				t.Fatal("host check omitted an ordinary pipeline stage")
			}
			for _, kind := range []event.Kind{event.ToolDispatch, event.ToolResult} {
				events := sink.kinds(kind)
				if len(events) == 0 {
					t.Fatal("host check emitted no tool event")
				}
				for _, e := range events {
					if e.Tool.Issuer != event.IssuedByHost {
						t.Fatalf("host check attributed to model: %+v", e.Tool)
					}
				}
			}
			host.Close()
			if after := host.Execute(t.Context(), provider.ToolCall{Name: "bash", Arguments: `{}`}); !after.Blocked || probe.calls != 1 {
				t.Fatal("closed host executor ran another check")
			}
		})
	}
}

func TestHostExecutorRefusesReplacedCheckBeforeExecution(t *testing.T) {
	probe := &hostCheckProbe{}
	reg := tool.NewRegistry()
	reg.Add(probe)
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, raw json.RawMessage) (protocol.InterceptResult, error) {
		if ev != protocol.EventToolBefore {
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}
		var p dispatch.ToolBeforePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return protocol.InterceptResult{}, err
		}
		p.Arguments = `{"command":"echo replaced"}`
		return replaceWith(t, p), nil
	}}
	host := NewHostExecutor(reg, Options{Extensions: newExtDispatcher(client, true, nil, extension.PointToolBefore)}, event.Discard)
	defer host.Close()
	result := host.Execute(t.Context(), provider.ToolCall{Name: "bash", Arguments: `{"command":"go test ./..."}`})
	if probe.calls != 0 || result.Executed || result.RefusalCode != "isolation.check_changed" {
		t.Fatalf("host check replacement executed: %+v", result)
	}
}
