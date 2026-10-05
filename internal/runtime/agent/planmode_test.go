package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
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
	allow     bool
	reason    string
	calls     []permissionCall
	denied    bool
	denyCalls []string
}

func (g *recordingPermissionGate) ExplicitlyDenies(name string, _ json.RawMessage) bool {
	g.denyCalls = append(g.denyCalls, name)
	return g.denied
}

func (g *recordingPermissionGate) Check(_ context.Context, name string, _ json.RawMessage, readOnly bool) (bool, string, error) {
	g.calls = append(g.calls, permissionCall{name: name, readOnly: readOnly})
	return g.allow, g.reason, nil
}

type annotatedMCPTool struct {
	fakeTool
	server           string
	raw              string
	destructive      bool
	serverAuthorized bool
}

func (t annotatedMCPTool) MCPServerName() string     { return t.server }
func (t annotatedMCPTool) MCPRawToolName() string    { return t.raw }
func (t annotatedMCPTool) MCPDestructiveHint() bool  { return t.destructive }
func (t annotatedMCPTool) MCPServerAuthorized() bool { return t.serverAuthorized }

type mcpPermissionRecordingGate struct {
	normalCalls int
	readOnly    []bool
	allowNormal bool
	reason      string
}

func (g *mcpPermissionRecordingGate) Check(_ context.Context, _ string, _ json.RawMessage, readOnly bool) (bool, string, error) {
	g.normalCalls++
	g.readOnly = append(g.readOnly, readOnly)
	return g.allowNormal, g.reason, nil
}

// Readers plan and still answer to Permissions: the phase barrier is not a
// second permission system and does not pre-empt one. Writers are the other
// half of this and live in TestPlanningPhaseStopsWritersBeforePermission.
func TestPlanModeRoutesOrdinaryToolsThroughPermissionGate(t *testing.T) {
	tests := []struct {
		name     string
		tool     tool.Tool
		args     string
		readOnly bool
	}{
		{name: "reader", tool: fakeTool{name: "read_file", readOnly: true}, readOnly: true},
		{
			name: "authorized MCP reader",
			tool: annotatedMCPTool{
				fakeTool:         fakeTool{name: "mcp__srv__query", readOnly: true},
				server:           "srv",
				raw:              "query",
				serverAuthorized: true,
			},
			readOnly: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(tc.tool)
			gate := &recordingPermissionGate{allow: true}
			a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)
			a.SetPlanMode(true)

			out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: tc.tool.Name(), Arguments: tc.args})
			if out.blocked || out.errMsg != "" || !strings.Contains(out.output, "done") {
				t.Fatalf("ordinary Plan call did not execute after permission approval: %+v", out)
			}
			if isInstalledMCPTool(tc.tool) {
				if len(gate.calls) != 0 || len(gate.denyCalls) != 1 || gate.denyCalls[0] != tc.tool.Name() {
					t.Fatalf("authorized MCP permission calls=%+v deny checks=%+v", gate.calls, gate.denyCalls)
				}
			} else if len(gate.calls) != 1 || gate.calls[0].name != tc.tool.Name() || gate.calls[0].readOnly != tc.readOnly {
				t.Fatalf("permission calls = %+v, want %q readOnly=%v", gate.calls, tc.tool.Name(), tc.readOnly)
			}
		})
	}
}

func TestPermissionDenialStopsWriterBeforeExecution(t *testing.T) {
	var executions int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", calls: &executions, writesPaths: true})
	gate := &recordingPermissionGate{reason: "denied by permission rule"}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "write_file"})
	if !out.blocked || !strings.Contains(out.output, gate.reason) || out.errMsg == "" {
		t.Fatalf("permission denial outcome = %+v", out)
	}
	if executions != 0 {
		t.Fatalf("denied writer executed %d times", executions)
	}
}

func TestAuthorizedMCPUsesInstallAuthorizationAndExplicitDenyOnly(t *testing.T) {
	var executions int32
	reg := tool.NewRegistry()
	reg.Add(annotatedMCPTool{
		fakeTool:         fakeTool{name: "mcp__srv__write", calls: &executions},
		server:           "srv",
		raw:              "write",
		serverAuthorized: true,
	})

	// The ordinary writer fallback would deny, but an authorized MCP server must
	// not re-enter that per-call approval path.
	gate := &recordingPermissionGate{allow: false, reason: "ordinary ask declined"}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "mcp__srv__write"})
	if out.blocked || out.errMsg != "" || executions != 1 || len(gate.calls) != 0 || len(gate.denyCalls) != 1 {
		t.Fatalf("authorized MCP outcome=%+v gate=%+v executions=%d", out, gate, executions)
	}

	gate.denied = true
	out = a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "mcp__srv__write"})
	if !out.blocked || !strings.Contains(out.output, "deny list") || executions != 1 {
		t.Fatalf("explicitly denied MCP outcome=%+v executions=%d", out, executions)
	}
}

func TestPlanModeUnsafePhaseToolStopsBeforePermission(t *testing.T) {
	var executions int32
	reg := tool.NewRegistry()
	reg.Add(planSafeTool{fakeTool: fakeTool{name: "complete_step", readOnly: true, calls: &executions}, planSafe: false})
	gate := &recordingPermissionGate{allow: true}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "complete_step"})
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
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "phase_safe_writer"})
	if out.blocked || out.errMsg != "" {
		t.Fatalf("phase-safe writer outcome = %+v", out)
	}
	if len(gate.calls) != 1 || gate.calls[0].readOnly {
		t.Fatalf("phase-safe writer permission calls = %+v", gate.calls)
	}
}

// Bash a command the host cannot prove read-only waits for approval, then runs
// as the declared writer it is. It used to have a trust bridge of its own that
// asked the user to accept a command prefix as read-only; that path is gone,
// and this pins what replaced it rather than that it is not called.
func TestBashWaitsForApprovalThenReachesOrdinaryPermission(t *testing.T) {
	const command = `{"command":"gh issue view 6482"}`
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash"})
	gate := &recordingPermissionGate{allow: true}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)

	a.SetPlanMode(true)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "bash", Arguments: command})
	if !out.blocked {
		t.Fatalf("bash the host cannot prove read-only ran while planning: %+v", out)
	}
	if len(gate.calls) != 0 {
		t.Fatalf("the phase barrier consulted Permissions: %+v", gate.calls)
	}

	a.SetPlanMode(false)
	out = a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "bash", Arguments: command})
	if out.blocked || out.errMsg != "" {
		t.Fatalf("permission-approved bash outcome = %+v", out)
	}
	if len(gate.calls) != 1 || gate.calls[0].readOnly {
		t.Fatalf("bash must reach ordinary permission as declared writer, calls=%+v", gate.calls)
	}
}

// The barrier runs before Permissions and does not consult it: a writer refused
// for the phase must not spend the user's approval, and turning the phase off
// must hand the same call straight back to the gate that was there all along.
func TestPlanningPhaseStopsWritersBeforePermission(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool tool.Tool
		args string
	}{
		{name: "built-in writer", tool: fakeTool{name: "write_file", writesPaths: true}},
		{name: "shell writer", tool: fakeTool{name: "bash"}, args: `{"command":"rm -rf build"}`},
		{name: "writer-capable delegation", tool: fakeTool{name: "task"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(tc.tool)
			gate := &recordingPermissionGate{allow: true}
			a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)

			a.SetPlanMode(true)
			out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: tc.tool.Name(), Arguments: tc.args})
			if !out.blocked {
				t.Fatalf("%s ran during planning: %+v", tc.name, out)
			}
			if len(gate.calls) != 0 {
				t.Fatalf("the phase barrier consulted Permissions: %+v", gate.calls)
			}

			a.SetPlanMode(false)
			if out = a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: tc.tool.Name(), Arguments: tc.args}); out.blocked {
				t.Fatalf("%s stayed blocked after the phase ended: %+v", tc.name, out)
			}
			if len(gate.calls) != 1 {
				t.Fatalf("execution-phase permission calls = %+v, want one", gate.calls)
			}
		})
	}
}

func TestPlanModeCanReplacePriorExecutionTodoState(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(mustBuiltinTool(t, "todo_write"))
	a := New(nil, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	recoveryGate := &recordingRecoveryGate{decision: RecoveryDecision{Allow: true}}
	a.SetRecoveryGate(recoveryGate)
	a.SeedTodoState([]evidence.TodoItem{{Content: "old execution step", Status: "in_progress"}})
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
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
	if len(recoveryGate.proposals) != 0 {
		t.Fatalf("Plan mode sent duplicate Auto plan review proposals: %+v", recoveryGate.proposals)
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
	reg.Add(fakeTool{name: "write_file", writesPaths: true})
	a := New(prov, reg, sessionstore.NewSession("STABLE-SYS"), Options{}, event.Discard)

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

func TestUnauthorizedMCPReaderBlockedInMainPlanAndExcludedFromReadOnlyAgents(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(fakeTool{name: "read_file", readOnly: true})
	parent.Add(annotatedMCPTool{
		fakeTool:         fakeTool{name: "mcp__srv__query", readOnly: true},
		server:           "srv",
		raw:              "query",
		serverAuthorized: false,
	})
	gate := &recordingPermissionGate{allow: true}
	a := New(nil, parent, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "mcp__srv__query"})
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
			t.Fatalf("%s registry admitted reader from unauthorized server", name)
		}
	}
}

func TestPlanModeMCPWriterIsHardBlockedBeforePermission(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(annotatedMCPTool{fakeTool: fakeTool{name: "mcp__srv__write"}, server: "srv", raw: "write"})
	gate := &mcpPermissionRecordingGate{allowNormal: true}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "mcp__srv__write"})
	if !out.blocked || gate.normalCalls != 0 {
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
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "mcp__srv__write"})
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
	gate := &mcpPermissionRecordingGate{allowNormal: true}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "mcp__srv__danger"})
	if !out.blocked || gate.normalCalls != 0 {
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
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: ordinary}, event.Discard)
	a.SetPlanMode(true)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "mcp__srv__danger"})
	if !out.blocked || !strings.Contains(out.output, "Plan mode") {
		t.Fatalf("destructive MCP fail-closed outcome = %+v", out)
	}
	if len(ordinary.calls) != 0 {
		t.Fatalf("destructive MCP fell back to ordinary gate: %+v", ordinary.calls)
	}
}

func TestPlanModeOffStillUsesSamePermissionGate(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", writesPaths: true})
	gate := &recordingPermissionGate{allow: true}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "write_file"})
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
	sess := sessionstore.NewSession("CHILD-SYSTEM")
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
	on := WithCallContext(context.Background(), "c", event.Discard, nil, true)
	if !PlanModeFromContext(on) || !planmode.Active(on) {
		t.Fatal("plan-mode flags disagree for an active planning call")
	}
	off := WithCallContext(context.Background(), "c", event.Discard, nil, false)
	if PlanModeFromContext(off) || planmode.Active(off) {
		t.Fatal("plan-mode flags disagree for a standard call")
	}
	if !planmode.Active(WithToolCallContext(context.Background(), "c", event.Discard, nil, true)) {
		t.Fatal("host-initiated wrapper lost the leaf plan-mode flag")
	}
}

func mustBuiltinTool(t *testing.T, name string) tool.Tool {
	t.Helper()
	builtin, ok := tool.LookupBuiltin(name)
	if !ok {
		t.Fatalf("builtin %q is not registered", name)
	}
	return builtin
}

type commitRecordingProxy struct {
	fakeTool
	target    tool.Tool
	committed *int
}

func (p commitRecordingProxy) ResolveCall(context.Context, json.RawMessage) (tool.ResolvedCall, error) {
	return tool.ResolvedCall{
		ProxyAction: "call",
		TargetName:  p.target.Name(),
		Target:      p.target,
		ReadOnly:    p.target.ReadOnly(),
		Args:        json.RawMessage(`{}`),
		Commit:      func() error { *p.committed++; return nil },
	}, nil
}

// A proxy names its real target only after resolving, so the phase gate has to
// run there too — and before Commit, which is a state transition the resolver
// contract already says waits for the host's checks. Commit running first would
// leave a refused planning call with a committed ledger entry behind it.
func TestPlanPhaseGateRunsBeforeResolvedCommit(t *testing.T) {
	for _, tc := range []struct {
		name          string
		target        tool.Tool
		wantBlocked   bool
		wantCommitted int
	}{
		{name: "side-effect target", target: fakeTool{name: "mcp__srv__write"}, wantBlocked: true},
		{name: "read-only target", target: annotatedMCPTool{
			fakeTool:         fakeTool{name: "mcp__srv__query", readOnly: true},
			server:           "srv",
			raw:              "query",
			serverAuthorized: true,
		}, wantCommitted: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			committed := 0
			reg := tool.NewRegistry()
			reg.Add(commitRecordingProxy{
				fakeTool:  fakeTool{name: "use_capability", readOnly: true},
				target:    tc.target,
				committed: &committed,
			})
			a := New(nil, reg, sessionstore.NewSession(""), Options{}, event.Discard)
			a.SetPlanMode(true)

			out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
				ID: "1", Name: "use_capability", Arguments: `{"action":"call"}`,
			})
			if out.blocked != tc.wantBlocked {
				t.Fatalf("blocked = %v, want %v (%+v)", out.blocked, tc.wantBlocked, out)
			}
			if committed != tc.wantCommitted {
				t.Fatalf("Commit ran %d times, want %d", committed, tc.wantCommitted)
			}
		})
	}
}

// No ordinary path reaches this assertion, which is why it needs a test: it
// exists for the day a new pre-execution path forgets the gate, and a backstop
// nobody ever exercised is not one.
func TestPlanPhaseAssertionRefusesAnUnadmittedCall(t *testing.T) {
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, event.Discard)

	a.SetPlanMode(true)
	out, blocked := a.assertPlanPhaseAdmitted(&toolCallPlan{})
	if !blocked || !strings.Contains(out.errMsg, "gate bypassed") {
		t.Fatalf("an unadmitted planning call reached execution: blocked=%v out=%+v", blocked, out)
	}
	if !strings.Contains(out.output, "host bug") {
		t.Errorf("the violation must not read as an ordinary refusal: %s", out.output)
	}
	fresh := a.plan().State()
	if _, blocked := a.assertPlanPhaseAdmitted(&toolCallPlan{admission: &fresh}); blocked {
		t.Error("an admitted call was refused at the execution boundary")
	}

	// An admission is a capability granted by one authority, not a permanent
	// one: after a transition the same token no longer opens the boundary.
	stale := planmode.State{Phase: fresh.Phase, Epoch: fresh.Epoch - 1}
	if _, blocked := a.assertPlanPhaseAdmitted(&toolCallPlan{admission: &stale}); !blocked {
		t.Error("an admission from an earlier epoch still opened the execution boundary")
	}

	a.SetPlanMode(false)
	if _, blocked := a.assertPlanPhaseAdmitted(&toolCallPlan{}); blocked {
		t.Error("the assertion fired outside the planning phase")
	}
}

// Approval grants capability to the work that follows it, not to whatever was
// already in flight. A batch the model streamed while planning arrives after
// the transition; it must be refused for being stale, and refused as stale
// rather than as a planning side effect, because the reason is what tells the
// model to look at the current state instead of retrying.
func TestStaleAuthorityCallsAreRefusedAcrossATransition(t *testing.T) {
	var executions int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", writesPaths: true, calls: &executions})
	a := New(nil, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	a.SetPlanMode(true)
	born := a.PlanState()
	if born.Phase != planmode.Planning {
		t.Fatalf("state = %+v, want planning", born)
	}

	if _, ok := a.plan().Apply(planmode.Submit); !ok {
		t.Fatal("submit refused")
	}
	if _, ok := a.plan().Apply(planmode.Start); !ok {
		t.Fatal("start refused")
	}

	call := provider.ToolCall{Name: "write_file", Arguments: `{"path":"x.txt","content":"x"}`}
	out := a.executeOne(planmode.WithAuthority(context.Background(), born), &a.turn, call)
	if !out.blocked || !strings.Contains(out.errMsg, "stale plan authority") {
		t.Fatalf("in-flight planning call after approval = %+v, want a stale refusal", out)
	}
	if executions != 0 {
		t.Fatalf("stale call executed %d times", executions)
	}

	out = a.executeOne(planmode.WithAuthority(context.Background(), a.PlanState()), &a.turn, call)
	if out.blocked {
		t.Fatalf("a call produced under the current authority was refused: %+v", out)
	}
	if executions != 1 {
		t.Fatalf("fresh call executed %d times, want 1", executions)
	}
}

// Turning Plan on mid-turn narrows what may run; it does not invalidate the
// turn. Refusing the work already in flight would throw away a round every time
// someone touched the toggle — and buy nothing, because what actually holds a
// side effect during planning is the phase gate, which judges each call on its
// own terms. A reader survives; a writer is refused by the gate, not as stale.
func TestEnteringThePlanWorkflowDoesNotInvalidateWorkInFlight(t *testing.T) {
	var reads, writes int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true, calls: &reads})
	reg.Add(fakeTool{name: "write_file", writesPaths: true, calls: &writes})
	a := New(nil, reg, sessionstore.NewSession(""), Options{}, event.Discard)

	born := a.PlanState()
	a.SetPlanMode(true)
	ctx := planmode.WithAuthority(context.Background(), born)

	if out := a.executeOne(ctx, &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"a.go"}`}); out.blocked {
		t.Fatalf("an in-flight reader was thrown away by the toggle: %+v", out)
	}
	if reads != 1 {
		t.Fatalf("reader ran %d times, want 1", reads)
	}

	out := a.executeOne(ctx, &a.turn, provider.ToolCall{Name: "write_file", Arguments: `{"path":"a.go","content":"x"}`})
	if !out.blocked || writes != 0 {
		t.Fatalf("an in-flight writer ran during planning: %+v writes=%d", out, writes)
	}
	if !strings.Contains(out.errMsg, "planning") || strings.Contains(out.errMsg, "stale") {
		t.Errorf("the writer was refused as stale rather than by the phase: %q", out.errMsg)
	}
}
