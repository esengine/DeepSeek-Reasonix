package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/capability"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
	_ "reasonix/internal/tool/builtin"
)

// recoveryArgumentTool uses the production contract without running a shell or
// reading files. Its receipts still travel through the real dispatch pipeline.
type recoveryArgumentTool struct {
	name   string
	schema json.RawMessage
	mu     sync.Mutex
	inputs []string
}

func (t *recoveryArgumentTool) Name() string            { return t.name }
func (t *recoveryArgumentTool) Description() string     { return "argument recovery fixture" }
func (t *recoveryArgumentTool) Schema() json.RawMessage { return t.schema }
func (t *recoveryArgumentTool) ReadOnly() bool          { return true }
func (t *recoveryArgumentTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inputs = append(t.inputs, string(args))
	return "executed", nil
}
func recoveryBash(t *testing.T) *recoveryArgumentTool {
	t.Helper()
	b, ok := tool.LookupBuiltin("bash")
	if !ok {
		t.Fatal("missing bash")
	}
	return &recoveryArgumentTool{name: "bash", schema: b.Schema()}
}

func TestArgumentErrorsRemainCorrectable(t *testing.T) {
	b := recoveryBash(t)
	reg := tool.NewRegistry()
	reg.Add(b)
	audit := &capability.Audit{}
	a := New(nil, reg, NewSession(""), Options{CapabilityAudit: audit}, event.Discard)
	raw := `{"arguments":{"command":"private-command-marker"}}`
	for i := 1; i <= 3; i++ {
		out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: fmt.Sprint(i), Name: "bash", Arguments: raw})
		if out.blocked {
			t.Errorf("attempt %d converted invalid input into a host refusal: %s", i, out.output)
		}
		if !strings.Contains(out.output, "726072279d1c3417") || !strings.Contains(out.output, "required properties: command") {
			t.Errorf("missing original diagnostic: %s", out.output)
		}
		if !strings.Contains(out.output, `sole "arguments" wrapper`) {
			t.Errorf("missing wrapper correction: %s", out.output)
		}
		if strings.Contains(out.output, "private-command-marker") {
			t.Fatal("argument value leaked")
		}
	}
	good := `{"command":"private-command-marker"}`
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "good", Name: "bash", Arguments: good})
	if out.errMsg != "" || out.blocked {
		t.Fatalf("corrected call: %+v", out)
	}
	if len(b.inputs) != 1 || b.inputs[0] != good {
		t.Fatalf("executions: %v", b.inputs)
	}
	snap := audit.Snapshot()
	if snap.Arguments.Validations != 4 || snap.Arguments.Fail != 3 || snap.Arguments.RemoteDispatch != 0 {
		t.Fatalf("audit: %+v", snap.Arguments)
	}
	if snap.LoopGuard.BlockedCalls != 0 || snap.LoopGuard.RepeatFailures != 0 {
		t.Fatalf("retired schema metrics: %+v", snap.LoopGuard)
	}
}

func TestArgumentStormEncouragesCorrection(t *testing.T) {
	b := recoveryBash(t)
	reg := tool.NewRegistry()
	reg.Add(b)
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	for i := 1; i <= 3; i++ {
		out := executeBatchOutputs(a, context.Background(), []provider.ToolCall{{ID: fmt.Sprint(i), Name: "bash", Arguments: `{"arguments":{"command":"pwd"}}`}})[0]
		if strings.Contains(out, "Do not retry by rewriting arguments") || strings.Contains(out, "do not keep retrying a blocked tool") {
			t.Fatalf("permission-style advice: %s", out)
		}
		if i == 3 && !strings.Contains(out, "tool argument generation failed") {
			t.Errorf("missing parameter convergence advice: %s", out)
		}
	}
	if !a.turn.loopGuardArmed {
		t.Fatal("soft final exit not armed")
	}
	out := executeBatchOutputs(a, context.Background(), []provider.ToolCall{{ID: "good", Name: "bash", Arguments: `{"command":"pwd"}`}})[0]
	if out != "executed" || len(b.inputs) != 1 {
		t.Fatalf("corrected call output %q inputs %v", out, b.inputs)
	}
}

func recoveryProxy(reg *tool.Registry) *UseCapabilityTool {
	p := NewUseCapabilityTool(context.Background(), nil, nil, reg, nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{ID: "tool:bash", Kind: capability.KindTool, Name: "bash"}}}
	})
	reg.Add(p)
	return p
}

func TestArgumentRecoveryProxyLayers(t *testing.T) {
	tests := []struct {
		name, raw, want string
		validation      bool
	}{
		{"outer wrapper", `{"arguments":{"action":"call","capability_id":"tool:bash","arguments":{"command":"pwd"}}}`, "parameters for use_capability directly at the root", true},
		{"missing action", `{}`, "required properties: action", true},
		{"action type", `{"action":123}`, "expected string", true},
		{"invalid limit", `{"action":"search","query":"bash","limit":10}`, "argument validation failed", true},
		{"missing target", `{"action":"call"}`, "capability_id is required", false},
		{"missing query", `{"action":"search"}`, "query is required", false},
		{"missing inspect target", `{"action":"inspect"}`, "capability_id is required", false},
		{"bad MCP object", `{"action":"call","capability_id":"mcp-tool:missing/test","arguments":"{}"}`, "/arguments", true},
		{"inner wrapper", `{"action":"call","capability_id":"tool:bash","arguments":{"arguments":{"command":"pwd"}}}`, "inside use_capability.arguments", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := recoveryBash(t)
			reg := tool.NewRegistry()
			reg.Add(b)
			recoveryProxy(reg)
			audit := &capability.Audit{}
			a := New(nil, reg, NewSession(""), Options{CapabilityAudit: audit}, event.Discard)
			out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "bad", Name: "use_capability", Arguments: tc.raw})
			if out.blocked || !strings.Contains(out.output, tc.want) || !strings.Contains(out.output, "not executed") {
				t.Fatalf("outcome: %+v", out)
			}
			if len(b.inputs) != 0 {
				t.Fatal("invalid call executed")
			}
			if strings.HasPrefix(out.errMsg, "argument_validation:") != tc.validation {
				t.Fatalf("wrong error class %q", out.errMsg)
			}
			snap := audit.Snapshot()
			want := 0
			if tc.validation {
				want = 1
			}
			if snap.Arguments.Fail != want || snap.Arguments.Validations != want {
				t.Fatalf("diagnostic counted more than once: %+v", snap.Arguments)
			}
			good := `{"action":"call","capability_id":"tool:bash","arguments":{"command":"pwd"},"ignored_legacy_field":true}`
			out = a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "good", Name: "use_capability", Arguments: good})
			if out.errMsg != "" || len(b.inputs) != 1 || b.inputs[0] != `{"command":"pwd"}` {
				t.Fatalf("successful envelope compatibility: %+v, %v", out, b.inputs)
			}
		})
	}
}

func TestArgumentRecoveryWrapperHintIsConservative(t *testing.T) {
	simple := `{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`
	tests := []struct {
		name, schema, raw string
		hint              bool
	}{
		{"confirmed", simple, `{"arguments":{"command":"sensitive-value"}}`, true},
		{"empty", simple, ``, false}, {"null", simple, `null`, false},
		{"array", simple, `[]`, false}, {"string", simple, `"{}"`, false},
		{"truncated", simple, `{"arguments":`, false},
		{"multiple values", simple, `{"arguments":{"command":"secret"}} {}`, false},
		{"null inner", simple, `{"arguments":null}`, false},
		{"array inner", simple, `{"arguments":[]}`, false},
		{"string inner", simple, `{"arguments":"{\"command\":\"secret\"}"}`, false},
		{"invalid inner", simple, `{"arguments":{"command":42}}`, false},
		{"recursive wrapping", simple, `{"arguments":{"arguments":{"command":"secret"}}}`, false},
		{"outer sibling", simple, `{"arguments":{"command":"secret"},"extra":true}`, false},
		{"legitimate arguments", `{"type":"object","properties":{"arguments":{"type":"object"},"command":{"type":"string"}},"required":["command"]}`, `{"arguments":{"command":"secret"}}`, false},
		{"composition", `{"type":"object","properties":{"command":{"type":"string"}},"allOf":[{"required":["command"]}]}`, `{"arguments":{"command":"secret"}}`, false},
		{"reference", `{"type":"object","properties":{"command":{"type":"string"}},"$defs":{"args":{"required":["command"]}},"$ref":"#/$defs/args"}`, `{"arguments":{"command":"secret"}}`, false},
		{"pattern", `{"type":"object","properties":{"command":{"type":"string"}},"patternProperties":{"arguments":{}},"required":["command"]}`, `{"arguments":{"command":"secret"}}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			target := &recoveryArgumentTool{name: "fixture", schema: json.RawMessage(tc.schema)}
			raw := json.RawMessage(tc.raw)
			before := string(raw)
			result := tool.ValidateArguments(target, raw)
			if result.CompileErr != nil || len(result.Violations) == 0 {
				t.Fatalf("invalid test fixture: %+v", result)
			}
			msg := argumentValidationMessage(&toolCallPlan{permName: target.Name(), execTool: target, execArgs: raw}, result)
			if strings.Contains(msg, `sole "arguments" wrapper`) != tc.hint {
				t.Fatalf("hint=%v: %s", tc.hint, msg)
			}
			if string(raw) != before || len(target.inputs) != 0 {
				t.Fatal("diagnostics changed or executed arguments")
			}
			if strings.Contains(msg, "sensitive-value") || strings.Contains(msg, "secret") {
				t.Fatal("argument value leaked")
			}
		})
	}
}

type conditionalRecoveryTool struct{ *recoveryArgumentTool }

func (t conditionalRecoveryTool) ValidateArguments(raw json.RawMessage) []tool.ArgumentViolation {
	return []tool.ArgumentViolation{{Path: "/command", Keyword: "conditional", Expected: "an eligible command"}}
}
func TestArgumentWrapperHintRespectsConditionalValidation(t *testing.T) {
	target := conditionalRecoveryTool{recoveryBash(t)}
	raw := json.RawMessage(`{"arguments":{"command":"pwd"}}`)
	result := tool.ValidateArguments(target, raw)
	msg := argumentValidationMessage(&toolCallPlan{permName: "bash", execTool: target, execArgs: raw}, result)
	if strings.Contains(msg, `sole "arguments" wrapper`) {
		t.Fatalf("conditional rejection ignored: %s", msg)
	}
}

func TestArgumentRecoveryStillChecksPermissions(t *testing.T) {
	b := recoveryBash(t)
	reg := tool.NewRegistry()
	reg.Add(b)
	gate := &stubGate{deny: map[string]bool{"bash": true}}
	a := New(nil, reg, NewSession(""), Options{Gate: gate}, event.Discard)
	for range 3 {
		a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "bash", Arguments: `{}`})
	}
	if len(gate.checked) != 0 {
		t.Fatal("invalid arguments reached permission gate")
	}
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "bash", Arguments: `{"command":"pwd"}`})
	if !out.blocked || len(gate.checked) != 1 || len(b.inputs) != 0 {
		t.Fatalf("permission bypass: %+v", out)
	}
}

func TestArgumentRecoveryConcurrentFailures(t *testing.T) {
	b := recoveryBash(t)
	reg := tool.NewRegistry()
	reg.Add(b)
	audit := &capability.Audit{}
	a := New(nil, reg, NewSession(""), Options{CapabilityAudit: audit}, event.Discard)
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: fmt.Sprint(i), Name: "bash", Arguments: `{"arguments":{"command":"pwd"}}`})
			if out.blocked || !strings.Contains(out.output, "required properties: command") {
				t.Errorf("call %d: %+v", i, out)
			}
		}(i)
	}
	wg.Wait()
	if len(b.inputs) != 0 || audit.Snapshot().Arguments.Fail != 32 {
		t.Fatal("concurrent failure accounting or execution")
	}
}

func TestArgumentRecoveryCompileFailure(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint("mixed=", mixed), func(t *testing.T) {
			target := &recoveryArgumentTool{name: "broken", schema: json.RawMessage(`{"type":"invalid-schema-type"}`)}
			reg := tool.NewRegistry()
			reg.Add(target)
			reg.Add(recoveryBash(t))
			a := New(nil, reg, NewSession(""), Options{}, event.Discard)
			for i := range 3 {
				calls := []provider.ToolCall{{ID: fmt.Sprint(i), Name: "broken", Arguments: `{}`}}
				if mixed {
					calls = append(calls, provider.ToolCall{ID: fmt.Sprint(i, "bad"), Name: "bash", Arguments: `{}`})
				}
				out := executeBatchOutputs(a, context.Background(), calls)[0]
				if !strings.Contains(out, "host configuration error") || strings.Contains(out, "tool argument generation failed") || strings.Contains(out, "otherwise fix the arguments") {
					t.Fatalf("schema failure treated as input mistake: %s", out)
				}
			}
		})
	}
}

func TestArgumentRecoveryMessageBound(t *testing.T) {
	msg := truncateValidationMessage(strings.Repeat("中", maxArgumentValidationMessageBytes))
	if len(msg) > maxArgumentValidationMessageBytes || !utf8.ValidString(msg) || !strings.HasSuffix(msg, "[truncated]") {
		t.Fatal("invalid UTF-8 or oversized diagnostic")
	}
}

func TestArgumentRecoveryNullProxyAndResolutionErrors(t *testing.T) {
	reg := tool.NewRegistry()
	noop := &recoveryArgumentTool{name: "noop", schema: json.RawMessage(`{"type":"object"}`)}
	reg.Add(noop)
	recoveryProxy(reg)
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "use_capability", Arguments: `{"action":"call","capability_id":"tool:noop","arguments":null,"ignored":true}`})
	if out.errMsg != "" || len(noop.inputs) != 1 || noop.inputs[0] != "null" {
		t.Fatalf("null compatibility: %+v, %v", out, noop.inputs)
	}
	out = a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "use_capability", Arguments: `{"action":"call","capability_id":"tool:absent"}`})
	if !strings.Contains(out.output, `tool "absent" is not registered`) || strings.HasPrefix(out.errMsg, "argument_validation:") {
		t.Fatalf("resolution error lost: %+v", out)
	}
}

func TestArgumentRecoveryDoesNotMaskResolverPolicyFailure(t *testing.T) {
	reg := tool.NewRegistry()
	ledger := capability.NewLedger()
	ledger.SeedCandidates(capability.RouteDecision{Candidates: []capability.RouteCandidate{{Entry: capability.Entry{ID: "tool:bash"}, Policy: capability.AutoUseRequire}}})
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, reg, ledger, nil, nil)
	reg.Add(proxy)
	audit := &capability.Audit{}
	a := New(nil, reg, NewSession(""), Options{CapabilityAudit: audit}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "use_capability", Arguments: `{"action":"decline","capability_id":"tool:bash","reason":"skip","unknown_field":true}`})
	if !strings.Contains(out.output, "cannot decline a require capability") || strings.HasPrefix(out.errMsg, "argument_validation:") || audit.Snapshot().Arguments.Validations != 0 {
		t.Fatalf("policy error masked by outer schema: %+v", out)
	}
}

func TestArgumentRecoverySkillNesting(t *testing.T) {
	store := skillStoreWithArchitect(t)
	calls := 0
	reg := tool.NewRegistry()
	reg.Add(skill.NewRunSkillTool(store, func(_ context.Context, _ skill.Skill, input string, _ skill.SubagentRunOptions) (string, error) {
		calls++
		if input != "specific task" {
			t.Errorf("skill input changed: %q", input)
		}
		return "skill executed", nil
	}))
	reg.Add(NewUseCapabilityTool(context.Background(), nil, nil, reg, nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{ID: "skill:team-architect", Kind: capability.KindSkill, Name: "team-architect"}}}
	}))
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	for range 3 {
		out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "use_capability", Arguments: `{"action":"call","capability_id":"skill:team-architect","arguments":{}}`})
		if out.blocked || !strings.Contains(out.output, `"arguments":{"arguments"`) || strings.Contains(out.output, `sole "arguments" wrapper`) {
			t.Fatalf("skill correction: %+v", out)
		}
	}
	out := a.executeOne(withNoClosedLoop(context.Background()), &a.turn, provider.ToolCall{Name: "use_capability", Arguments: `{"action":"call","capability_id":"skill:team-architect","arguments":{"arguments":"specific task"}}`})
	if out.errMsg != "" || calls != 1 {
		t.Fatalf("skill nested call: %+v calls %d", out, calls)
	}
}

func TestArgumentStormBatchCountsAndSuccessReset(t *testing.T) {
	reg := tool.NewRegistry()
	b := recoveryBash(t)
	reg.Add(b)
	reader := &recoveryArgumentTool{name: "read_file", schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}
	reg.Add(reader)
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	bad := []provider.ToolCall{{ID: "b", Name: "bash", Arguments: `{}`}, {ID: "r", Name: "read_file", Arguments: `{}`}}
	for i := 1; i <= 3; i++ {
		batch := a.executeBatch(context.Background(), &a.turn, bad)
		if (strings.Contains(batch.results[0], "tool argument generation failed")) != (i == 3) {
			t.Fatalf("batch %d: %v", i, batch.results)
		}
		for _, out := range batch.outcomes {
			if out.blocked {
				t.Fatal("parameter batch became blocked")
			}
		}
	}
	mixed := []provider.ToolCall{{ID: "b2", Name: "bash", Arguments: `{}`}, {ID: "r2", Name: "read_file", Arguments: `{"path":"file.go"}`}}
	batch := a.executeBatch(context.Background(), &a.turn, mixed)
	if len(reader.inputs) != 1 || a.turn.stormCount != 0 {
		t.Fatalf("mixed success did not reset: %v", batch.results)
	}
	batch = a.executeBatch(context.Background(), &a.turn, bad)
	if strings.Contains(batch.results[0], "tool argument generation failed") {
		t.Fatal("success failed to break the streak")
	}
}

func TestArgumentStormMixedPermissionFailure(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(recoveryBash(t))
	reg.Add(&recoveryArgumentTool{name: "denied", schema: json.RawMessage(`{"type":"object"}`)})
	a := New(nil, reg, NewSession(""), Options{Gate: &stubGate{deny: map[string]bool{"denied": true}}}, event.Discard)
	for range 3 {
		batch := a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{Name: "bash", Arguments: `{}`}, {Name: "denied", Arguments: `{}`}})
		if batch.outcomes[0].blocked || !batch.outcomes[1].blocked {
			t.Fatalf("per-call classification lost: %+v", batch.outcomes)
		}
		if i == 2 && (!strings.Contains(batch.results[0], "Respect the permission") || !strings.Contains(batch.results[0], "required properties: command") || !strings.Contains(batch.results[1], "denied")) {
			t.Fatalf("mixed batch lost its errors: %v", batch.results)
		}
	}
}

// Capture serialized messages at request time, before later rounds can mutate
// session state, so this checks real prefix preservation rather than aliases.
type recoveryCaptureProvider struct {
	*testutil.MockProvider
	messages [][]json.RawMessage
	tools    [][]byte
	cancel   context.CancelFunc
}

func (p *recoveryCaptureProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	raw, _ := json.Marshal(req.Messages)
	var messages []json.RawMessage
	_ = json.Unmarshal(raw, &messages)
	p.messages = append(p.messages, messages)
	schema, _ := json.Marshal(req.Tools)
	p.tools = append(p.tools, schema)
	if p.cancel != nil && len(p.messages) == 7 {
		p.cancel()
		return nil, ctx.Err()
	}
	return p.MockProvider.Stream(ctx, req)
}

func TestArgumentRecoveryPreservesProviderPrefix(t *testing.T) {
	b := recoveryBash(t)
	reg := tool.NewRegistry()
	reg.Add(b)
	var turns []testutil.Turn
	raw := `{"arguments":{"command":"pwd"}}`
	for i := range 3 {
		turns = append(turns, testutil.Turn{ToolCalls: []provider.ToolCall{{ID: fmt.Sprint(i), Name: "bash", Arguments: raw}}})
	}
	turns = append(turns, testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "fixed", Name: "bash", Arguments: `{"command":"pwd"}`}}}, testutil.Turn{Text: "done"})
	p := &recoveryCaptureProvider{MockProvider: testutil.NewMock("recovery", turns...)}
	sink := &recordSink{}
	a := New(p, reg, NewSession("Stable system prefix"), Options{MaxSteps: 0}, sink)
	if err := a.Run(withNoClosedLoop(context.Background()), "Run the fixture"); err != nil {
		t.Fatal(err)
	}
	if len(p.messages) != 5 || len(b.inputs) != 1 {
		t.Fatalf("requests=%d executions=%d", len(p.messages), len(b.inputs))
	}
	for i := 1; i < len(p.messages); i++ {
		if !bytes.Equal(p.tools[0], p.tools[i]) {
			t.Fatal("tool schema changed during correction")
		}
		for j, old := range p.messages[i-1] {
			if j >= len(p.messages[i]) || !bytes.Equal(old, p.messages[i][j]) {
				t.Fatalf("existing prefix rewritten at request %d message %d", i, j)
			}
		}
	}
	results := map[string]int{}
	originals := 0
	for _, m := range a.Session().Snapshot() {
		if m.Role == provider.RoleTool {
			results[m.ToolCallID]++
		}
		for _, c := range m.ToolCalls {
			if c.ID != "fixed" {
				if c.Arguments != raw {
					t.Fatalf("original malformed call rewritten: %+v", c)
				}
				originals++
			}
		}
	}
	if originals != 3 || len(results) != 4 {
		t.Fatalf("history originals=%d results=%v", originals, results)
	}
	for _, n := range results {
		if n != 1 {
			t.Fatal("duplicate tool result")
		}
	}
	if len(sink.kinds(event.Retrying)) != 0 {
		t.Fatal("tool correction invoked network retry")
	}
}

func TestArgumentRecoverySoftLimitAndExplicitBudget(t *testing.T) {
	for _, limit := range []int{0, 3} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			b := recoveryBash(t)
			reg := tool.NewRegistry()
			reg.Add(b)
			turns := make([]testutil.Turn, 12)
			for i := range turns {
				turns[i] = testutil.Turn{ToolCalls: []provider.ToolCall{{ID: fmt.Sprint(i), Name: "bash", Arguments: `{}`}}}
			}
			p := &recoveryCaptureProvider{MockProvider: testutil.NewMock("recovery", turns...)}
			ctx, cancel := context.WithCancel(withNoClosedLoop(context.Background()))
			defer cancel()
			if limit == 0 {
				p.cancel = cancel
			}
			a := New(p, reg, NewSession("system"), Options{MaxSteps: limit}, event.Discard)
			err := a.Run(ctx, "Use the fixture")
			if len(b.inputs) != 0 {
				t.Fatal("invalid calls executed")
			}
			if limit == 0 {
				if ctx.Err() == nil || len(p.messages) != 7 {
					t.Fatalf("unconfigured run stopped before cancellation: %v requests=%d", err, len(p.messages))
				}
				if !a.turn.loopGuardArmed {
					t.Fatal("soft convergence never activated")
				}
			} else {
				var pause *maxStepsPause
				if !errors.As(err, &pause) {
					t.Fatalf("explicit steps failed to pause: %v", err)
				}
			}
		})
	}
}
