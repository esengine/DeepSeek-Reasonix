package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/planmode"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type planSafeTool struct {
	fakeTool
	planSafe bool
}

func (p planSafeTool) PlanModeSafe() bool { return p.planSafe }

type permissionCall struct {
	name     string
	readOnly bool
}

type recordingPermissionGate struct {
	allow  bool
	reason string
	calls  []permissionCall
}

func (g *recordingPermissionGate) Check(_ context.Context, name string, _ json.RawMessage, readOnly bool) (bool, string, error) {
	g.calls = append(g.calls, permissionCall{name: name, readOnly: readOnly})
	return g.allow, g.reason, nil
}

type legacyPlanTrustGate struct{ calls int }

func (g *legacyPlanTrustGate) CheckPlanModeReadOnlyTrust(context.Context, PlanModeReadOnlyTrustRequest) (bool, string, error) {
	g.calls++
	return true, "", nil
}

type annotatedMCPTool struct {
	fakeTool
	server            string
	raw               string
	destructive       bool
	untrustedReadOnly bool
}

func (t annotatedMCPTool) MCPServerName() string           { return t.server }
func (t annotatedMCPTool) MCPRawToolName() string          { return t.raw }
func (t annotatedMCPTool) MCPDestructiveHint() bool        { return t.destructive }
func (t annotatedMCPTool) PlanModeUntrustedReadOnly() bool { return t.untrustedReadOnly }

type mcpPermissionRecordingGate struct {
	normalCalls int
	freshCalls  int
	readOnly    []bool
	allowNormal bool
	allowFresh  bool
	reason      string
	subject     string
}

func (g *mcpPermissionRecordingGate) Check(_ context.Context, _ string, _ json.RawMessage, readOnly bool) (bool, string, error) {
	g.normalCalls++
	g.readOnly = append(g.readOnly, readOnly)
	return g.allowNormal, g.reason, nil
}

func (g *mcpPermissionRecordingGate) CheckFresh(_ context.Context, _ string, subject string, _ json.RawMessage, readOnly bool) (bool, string, error) {
	g.freshCalls++
	g.subject = subject
	g.readOnly = append(g.readOnly, readOnly)
	return g.allowFresh, g.reason, nil
}

func TestPlanModeRoutesOrdinaryToolsThroughPermissionGate(t *testing.T) {
	tests := []struct {
		name     string
		tool     tool.Tool
		args     string
		readOnly bool
	}{
		{name: "built-in writer", tool: fakeTool{name: "write_file"}},
		{name: "shell writer", tool: fakeTool{name: "bash"}, args: `{"command":"rm -rf build"}`},
		{name: "reader", tool: fakeTool{name: "read_file", readOnly: true}, readOnly: true},
		{
			name: "host-trusted MCP reader",
			tool: annotatedMCPTool{
				fakeTool:          fakeTool{name: "mcp__srv__query", readOnly: true},
				server:            "srv",
				raw:               "query",
				untrustedReadOnly: false,
			},
			readOnly: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(tc.tool)
			gate := &recordingPermissionGate{allow: true}
			a := New(nil, reg, NewSession(""), Options{Gate: gate}, event.Discard)
			a.SetPlanMode(true)

			out := a.executeOne(context.Background(), provider.ToolCall{Name: tc.tool.Name(), Arguments: tc.args})
			if out.blocked || out.errMsg != "" || !strings.Contains(out.output, "done") {
				t.Fatalf("ordinary Plan call did not execute after permission approval: %+v", out)
			}
			if len(gate.calls) != 1 || gate.calls[0].name != tc.tool.Name() || gate.calls[0].readOnly != tc.readOnly {
				t.Fatalf("permission calls = %+v, want %q readOnly=%v", gate.calls, tc.tool.Name(), tc.readOnly)
			}
		})
	}
}

func TestPlanModePermissionDenialStopsWriterBeforeExecution(t *testing.T) {
	var executions int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", calls: &executions})
	gate := &recordingPermissionGate{reason: "denied by permission rule"}
	a := New(nil, reg, NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "write_file"})
	if !out.blocked || !strings.Contains(out.output, gate.reason) || out.errMsg == "" {
		t.Fatalf("permission denial outcome = %+v", out)
	}
	if executions != 0 {
		t.Fatalf("denied writer executed %d times", executions)
	}
}

func TestPlanModeUnsafePhaseToolStopsBeforePermission(t *testing.T) {
	var executions int32
	reg := tool.NewRegistry()
	reg.Add(planSafeTool{fakeTool: fakeTool{name: "complete_step", readOnly: true, calls: &executions}, planSafe: false})
	gate := &recordingPermissionGate{allow: true}
	a := New(nil, reg, NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "complete_step"})
	if !out.blocked || !strings.Contains(out.output, "only available after plan approval") {
		t.Fatalf("phase opt-out outcome = %+v", out)
	}
	if len(gate.calls) != 0 || executions != 0 {
		t.Fatalf("phase-blocked call reached permission/execution: gate=%+v executions=%d", gate.calls, executions)
	}
}

func TestPlanModeSafeWriterStillUsesWriterPermission(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(planSafeTool{fakeTool: fakeTool{name: "phase_safe_writer"}, planSafe: true})
	gate := &recordingPermissionGate{allow: true}
	a := New(nil, reg, NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "phase_safe_writer"})
	if out.blocked || out.errMsg != "" {
		t.Fatalf("phase-safe writer outcome = %+v", out)
	}
	if len(gate.calls) != 1 || gate.calls[0].readOnly {
		t.Fatalf("phase-safe writer permission calls = %+v", gate.calls)
	}
}

func TestPlanModeDoesNotInvokeLegacyBashTrustPrompt(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash"})
	gate := &recordingPermissionGate{allow: true}
	legacy := &legacyPlanTrustGate{}
	a := New(nil, reg, NewSession(""), Options{
		Gate:                      gate,
		PlanModeReadOnlyTrustGate: legacy,
	}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{
		Name:      "bash",
		Arguments: `{"command":"gh issue view 6482"}`,
	})
	if out.blocked || out.errMsg != "" {
		t.Fatalf("permission-approved bash outcome = %+v", out)
	}
	if legacy.calls != 0 {
		t.Fatalf("obsolete Plan bash trust prompt was invoked %d times", legacy.calls)
	}
	if len(gate.calls) != 1 || gate.calls[0].readOnly {
		t.Fatalf("bash must reach ordinary permission as declared writer, calls=%+v", gate.calls)
	}
}

func TestPlanModeLegacyOverridesDoNotBypassPermissions(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file"})
	gate := &recordingPermissionGate{reason: "denied"}
	a := New(nil, reg, NewSession(""), Options{
		Gate:                     gate,
		PlanModeAllowedTools:     []string{"write_file"},
		PlanModeReadOnlyCommands: []string{"gh issue view"},
	}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "write_file"})
	if !out.blocked || len(gate.calls) != 1 {
		t.Fatalf("legacy Plan config bypassed permissions: outcome=%+v calls=%+v", out, gate.calls)
	}
}

func TestPlanModeCanReplacePriorExecutionTodoState(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(mustBuiltinTool(t, "todo_write"))
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	a.SeedTodoState([]evidence.TodoItem{{Content: "old execution step", Status: "in_progress"}})
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{
		ID:   "new-plan",
		Name: "todo_write",
		Arguments: `{"todos":[
			{"content":"inspect the new request","status":"in_progress"},
			{"content":"draft a revised plan","status":"pending"}
		]}`,
	})
	if out.errMsg != "" {
		t.Fatalf("plan-mode todo replacement was blocked: %s", out.errMsg)
	}
	got := a.CanonicalTodoState()
	if len(got) != 2 || got[0].Content != "inspect the new request" {
		t.Fatalf("plan-mode todo state = %+v, want revised plan", got)
	}
}

// TestPlanModeDoesNotMutateSystemOrTools is the cache-stability test. Toggling
// plan mode between two stream calls must not change the system prompt or the
// tool list seen by the provider — those are the cache-key prefix, and any
// change there forces an expensive cache miss.
func TestPlanModeDoesNotMutateSystemOrTools(t *testing.T) {
	prov := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	reg.Add(fakeTool{name: "write_file"})
	a := New(prov, reg, NewSession("STABLE-SYS"), Options{}, event.Discard)

	if err := a.Run(context.Background(), "explore"); err != nil {
		t.Fatalf("standard Run: %v", err)
	}
	standardSystem := prov.lastReq.Messages[0]
	standardTools := serializeToolSchemas(t, prov.lastReq.Tools)

	prov.chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}}
	a.SetPlanMode(true)
	if err := a.Run(context.Background(), "now in plan mode"); err != nil {
		t.Fatalf("Plan Run: %v", err)
	}
	planSystem := prov.lastReq.Messages[0]
	planTools := serializeToolSchemas(t, prov.lastReq.Tools)

	if planSystem.Role != standardSystem.Role || planSystem.Content != standardSystem.Content {
		t.Fatalf("system message changed across Plan toggle:\nstandard=%+v\nplan=%+v", standardSystem, planSystem)
	}
	if planTools != standardTools {
		t.Fatalf("tool schemas changed across Plan toggle:\nstandard=%s\nplan=%s", standardTools, planTools)
	}
}

func serializeToolSchemas(t *testing.T, schemas []provider.ToolSchema) string {
	t.Helper()
	b, err := json.Marshal(schemas)
	if err != nil {
		t.Fatalf("serialize tool schemas: %v", err)
	}
	return string(b)
}

func TestUntrustedMCPReaderBlockedInMainPlanAndExcludedFromReadOnlyAgents(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(fakeTool{name: "read_file", readOnly: true})
	parent.Add(annotatedMCPTool{
		fakeTool:          fakeTool{name: "mcp__srv__query", readOnly: true},
		server:            "srv",
		raw:               "query",
		untrustedReadOnly: true,
	})
	gate := &recordingPermissionGate{allow: true}
	a := New(nil, parent, NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "mcp__srv__query"})
	if !out.blocked || len(gate.calls) != 0 {
		t.Fatalf("main Plan MCP reader outcome=%+v calls=%+v", out, gate.calls)
	}

	for name, filtered := range map[string]*tool.Registry{
		"planner":  FilterReadOnlyRegistry(parent),
		"subagent": ReadOnlySubagentToolRegistry(parent, nil),
	} {
		if _, ok := filtered.Get("read_file"); !ok {
			t.Fatalf("%s registry lost local reader", name)
		}
		if _, ok := filtered.Get("mcp__srv__query"); ok {
			t.Fatalf("%s registry admitted externally asserted reader", name)
		}
	}
}

func TestPlanModeMCPWriterIsHardBlockedBeforePermission(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(annotatedMCPTool{fakeTool: fakeTool{name: "mcp__srv__write"}, server: "srv", raw: "write"})
	gate := &mcpPermissionRecordingGate{allowNormal: true}
	a := New(nil, reg, NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "mcp__srv__write"})
	if !out.blocked || gate.normalCalls != 0 || gate.freshCalls != 0 {
		t.Fatalf("MCP writer outcome=%+v gate=%+v", out, gate)
	}
}

func TestPlanModeMCPWriterHonorsPermissionDenial(t *testing.T) {
	var executions int32
	reg := tool.NewRegistry()
	reg.Add(annotatedMCPTool{
		fakeTool: fakeTool{name: "mcp__srv__write", calls: &executions},
		server:   "srv",
		raw:      "write",
	})
	gate := &mcpPermissionRecordingGate{reason: "denied by policy"}
	a := New(nil, reg, NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "mcp__srv__write"})
	if !out.blocked || !strings.Contains(out.output, "Plan mode") || gate.normalCalls != 0 || executions != 0 {
		t.Fatalf("denied MCP writer outcome=%+v gate=%+v executions=%d", out, gate, executions)
	}
}

func TestDestructiveMCPUsesFreshApprovalInPlanEvenWhenReadOnly(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(annotatedMCPTool{
		fakeTool:    fakeTool{name: "mcp__srv__danger", readOnly: true},
		server:      "srv",
		raw:         "danger/raw",
		destructive: true,
	})
	gate := &mcpPermissionRecordingGate{allowNormal: true, allowFresh: true}
	a := New(nil, reg, NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "mcp__srv__danger"})
	if !out.blocked || gate.normalCalls != 0 || gate.freshCalls != 0 {
		t.Fatalf("destructive MCP outcome=%+v gate=%+v", out, gate)
	}
}

func TestDestructiveMCPFailsClosedWithoutFreshApprovalGate(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(annotatedMCPTool{
		fakeTool:    fakeTool{name: "mcp__srv__danger"},
		server:      "srv",
		raw:         "danger",
		destructive: true,
	})
	ordinary := &recordingPermissionGate{allow: true}
	a := New(nil, reg, NewSession(""), Options{Gate: ordinary}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "mcp__srv__danger"})
	if !out.blocked || !strings.Contains(out.output, "Plan mode") {
		t.Fatalf("destructive MCP fail-closed outcome = %+v", out)
	}
	if len(ordinary.calls) != 0 {
		t.Fatalf("destructive MCP fell back to ordinary gate: %+v", ordinary.calls)
	}
}

func TestPlanModeOffStillUsesSamePermissionGate(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file"})
	gate := &recordingPermissionGate{allow: true}
	a := New(nil, reg, NewSession(""), Options{Gate: gate}, event.Discard)

	out := a.executeOne(context.Background(), provider.ToolCall{Name: "write_file"})
	if out.blocked || len(gate.calls) != 1 {
		t.Fatalf("standard mode outcome=%+v calls=%+v", out, gate.calls)
	}
}

func TestRunSubAgentWithSessionInheritsPlanWorkflow(t *testing.T) {
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(completeStep)
	prov := &scriptedProvider{name: "plan-child", turns: [][]provider.Chunk{
		{toolCallChunk("phase", "complete_step", `{}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Plan ready."}, {Type: provider.ChunkDone}},
	}}
	sess := NewSession("CHILD-SYSTEM")
	ctx := WithToolCallContext(context.Background(), "parent", event.Discard, nil, true)
	answer, err := RunSubAgentWithSession(ctx, prov, reg, sess, "inspect the change", Options{}, event.Discard)
	if err != nil {
		t.Fatalf("Plan child: %v", err)
	}
	if answer != "Plan ready." {
		t.Fatalf("Plan child answer = %q", answer)
	}
	if len(prov.requests) < 1 {
		t.Fatal("Plan child made no provider request")
	}
	var user string
	for _, msg := range prov.requests[0].Messages {
		if msg.Role == provider.RoleUser {
			user = msg.Content
			break
		}
	}
	if !strings.Contains(user, planmode.Marker) {
		t.Fatalf("Plan child user turn missing workflow marker: %q", user)
	}
	if got := lastToolResult(sess, "complete_step"); !strings.Contains(got, "only available after plan approval") {
		t.Fatalf("Plan child complete_step result = %q", got)
	}
}

func TestCallContextMirrorsPlanModeOntoLeafKey(t *testing.T) {
	on := withCallContext(context.Background(), "c", event.Discard, nil, true)
	if !PlanModeFromContext(on) || !planmode.Active(on) {
		t.Fatal("plan-mode flags disagree for an active planning call")
	}
	off := withCallContext(context.Background(), "c", event.Discard, nil, false)
	if PlanModeFromContext(off) || planmode.Active(off) {
		t.Fatal("plan-mode flags disagree for a standard call")
	}
	if !planmode.Active(WithToolCallContext(context.Background(), "c", event.Discard, nil, true)) {
		t.Fatal("host-initiated wrapper lost the leaf plan-mode flag")
	}
}
