package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/capability"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/mcptrust"
	"reasonix/internal/permission"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

type denyAllGate struct{}

func (denyAllGate) Check(_ context.Context, name string, _ json.RawMessage, _ bool) (bool, string, error) {
	return false, "denied " + name, nil
}

type completedProxyCallTool struct{}

func (completedProxyCallTool) Name() string            { return "use_capability" }
func (completedProxyCallTool) Description() string     { return "" }
func (completedProxyCallTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (completedProxyCallTool) ReadOnly() bool          { return true }
func (completedProxyCallTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

type readOnlyBoundaryTarget struct {
	name      string
	readOnly  bool
	untrusted bool
	hostStart bool
	calls     *int
}

func (t readOnlyBoundaryTarget) Name() string                        { return t.name }
func (readOnlyBoundaryTarget) Description() string                   { return "" }
func (readOnlyBoundaryTarget) Schema() json.RawMessage               { return json.RawMessage(`{"type":"object"}`) }
func (t readOnlyBoundaryTarget) ReadOnly() bool                      { return t.readOnly }
func (t readOnlyBoundaryTarget) PlanModeUntrustedReadOnly() bool     { return t.untrusted }
func (t readOnlyBoundaryTarget) ReadOnlyExecutionHostMutation() bool { return t.hostStart }
func (t readOnlyBoundaryTarget) Execute(context.Context, json.RawMessage) (string, error) {
	if t.calls != nil {
		(*t.calls)++
	}
	return "target executed", nil
}

type readOnlyBoundaryProxy struct {
	resolved tool.ResolvedCall
}

func (readOnlyBoundaryProxy) Name() string            { return "use_capability" }
func (readOnlyBoundaryProxy) Description() string     { return "" }
func (readOnlyBoundaryProxy) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (readOnlyBoundaryProxy) ReadOnly() bool          { return true }
func (readOnlyBoundaryProxy) Execute(context.Context, json.RawMessage) (string, error) {
	return "proxy executed", nil
}
func (p readOnlyBoundaryProxy) ResolveCall(context.Context, json.RawMessage) (tool.ResolvedCall, error) {
	return p.resolved, nil
}

type layeredReadOnlyMCPBoundaryTarget struct {
	readOnlyBoundaryTarget
	destructive bool
}

func (layeredReadOnlyMCPBoundaryTarget) MCPServerName() string       { return "test" }
func (layeredReadOnlyMCPBoundaryTarget) MCPRawToolName() string      { return "read" }
func (layeredReadOnlyMCPBoundaryTarget) MCPApprovalMode() string     { return "approve" }
func (layeredReadOnlyMCPBoundaryTarget) MCPApprovalReviewer() string { return "user" }
func (t layeredReadOnlyMCPBoundaryTarget) MCPDestructiveHint() bool  { return t.destructive }

// The fake models a receipt-backed reader: a real trust store stands behind
// its classification unless the case is explicitly the untrusted server hint.
func (t layeredReadOnlyMCPBoundaryTarget) ReadOnlyExecutionTrustAuthority() bool {
	return !t.untrusted
}

func executeReadOnlyBoundaryCall(t *testing.T, resolved tool.ResolvedCall) toolOutcome {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(readOnlyBoundaryProxy{resolved: resolved})
	a := New(nil, reg, NewSession("sys"), Options{ReadOnlyExecution: true}, event.Discard)
	return a.executeOne(context.Background(), provider.ToolCall{
		ID: "ro-1", Name: "use_capability", Arguments: `{"action":"call","capability_id":"mcp-tool:test/tool"}`,
	})
}

func TestReadOnlyExecutionBlocksResolvedWriterAndHostStartup(t *testing.T) {
	for _, tc := range []struct {
		name      string
		readOnly  bool
		hostStart bool
	}{
		{name: "writer"},
		{name: "host startup", readOnly: true, hostStart: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			target := readOnlyBoundaryTarget{name: "mcp__test__tool", readOnly: tc.readOnly, hostStart: tc.hostStart, calls: &calls}
			out := executeReadOnlyBoundaryCall(t, tool.ResolvedCall{
				ProxyAction: "call", TargetName: target.Name(), Target: target, ReadOnly: tc.readOnly, Args: json.RawMessage(`{}`),
			})
			if !out.blocked || !strings.Contains(out.output, "read-only agent") {
				t.Fatalf("resolved call outcome = %+v, want host block", out)
			}
			if calls != 0 {
				t.Fatalf("target Execute calls = %d, want 0", calls)
			}
		})
	}
}

func TestReadOnlyExecutionAllowsInspectAndTrustedReadOnlyCall(t *testing.T) {
	inspect := executeReadOnlyBoundaryCall(t, tool.ResolvedCall{
		ProxyAction: "inspect", SkipExecute: true, ReadOnly: true, Result: "metadata",
	})
	if inspect.blocked || inspect.errMsg != "" || inspect.output != "metadata" {
		t.Fatalf("inspect outcome = %+v", inspect)
	}

	calls := 0
	target := readOnlyBoundaryTarget{name: "mcp__test__read", readOnly: true, calls: &calls}
	call := executeReadOnlyBoundaryCall(t, tool.ResolvedCall{
		ProxyAction: "call", TargetName: target.Name(), Target: target, ReadOnly: true, Args: json.RawMessage(`{}`),
	})
	if call.blocked || call.errMsg != "" || !strings.Contains(call.output, "target executed") {
		t.Fatalf("trusted read-only call outcome = %+v", call)
	}
	if calls != 1 {
		t.Fatalf("target Execute calls = %d, want 1", calls)
	}
}

func TestReadOnlyExecutionAllowsOnlyLayeredTrustedReadOnlyMCPStartup(t *testing.T) {
	for _, tc := range []struct {
		name        string
		untrusted   bool
		destructive bool
		wantBlocked bool
	}{
		{name: "locally trusted reader"},
		{name: "untrusted server hint", untrusted: true, wantBlocked: true},
		{name: "destructive reader", destructive: true, wantBlocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			target := layeredReadOnlyMCPBoundaryTarget{
				readOnlyBoundaryTarget: readOnlyBoundaryTarget{
					name: "mcp__test__read", readOnly: true, untrusted: tc.untrusted, hostStart: true, calls: &calls,
				},
				destructive: tc.destructive,
			}
			out := executeReadOnlyBoundaryCall(t, tool.ResolvedCall{
				ProxyAction: "call", TargetName: target.Name(), Target: target, ReadOnly: true, Args: json.RawMessage(`{}`),
			})
			if out.blocked != tc.wantBlocked {
				t.Fatalf("layered MCP outcome = %+v, want blocked=%v", out, tc.wantBlocked)
			}
			wantCalls := 1
			if tc.wantBlocked {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("target Execute calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestStrictReadOnlyExecutionRegistryFailsClosed(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "writer", readOnly: false})
	reg.Add(readOnlyBoundaryTarget{name: "ordinary_read", readOnly: true})
	reg.Add(readOnlyBoundaryTarget{name: "untrusted_read", readOnly: true, untrusted: true})
	reg.Add(layeredReadOnlyMCPBoundaryTarget{
		readOnlyBoundaryTarget: readOnlyBoundaryTarget{name: "mcp__test__trusted", readOnly: true, hostStart: true},
	})
	reg.Add(layeredReadOnlyMCPBoundaryTarget{
		readOnlyBoundaryTarget: readOnlyBoundaryTarget{name: "mcp__test__destructive", readOnly: true, hostStart: true},
		destructive:            true,
	})

	filtered := strictReadOnlyExecutionRegistry(reg)
	if got, want := strings.Join(filtered.Names(), ","), "ordinary_read,mcp__test__trusted"; got != want {
		t.Fatalf("strict registry = %q, want %q", got, want)
	}
}

func TestReadOnlyExecutionBlocksUntrustedHintAndDecline(t *testing.T) {
	calls := 0
	target := readOnlyBoundaryTarget{name: "mcp__test__hint", readOnly: true, untrusted: true, calls: &calls}
	out := executeReadOnlyBoundaryCall(t, tool.ResolvedCall{
		ProxyAction: "call", TargetName: target.Name(), Target: target, ReadOnly: true, Args: json.RawMessage(`{}`),
	})
	if !out.blocked || calls != 0 {
		t.Fatalf("untrusted read-only outcome = %+v calls=%d", out, calls)
	}

	ledger := capability.NewLedger()
	ledger.SeedCandidates(capability.RouteDecision{Candidates: []capability.RouteCandidate{
		{Entry: capability.Entry{ID: "skill:review"}, Policy: capability.AutoUsePrefer},
	}})
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, tool.NewRegistry(), ledger, nil, nil)
	reg := tool.NewRegistry()
	reg.Add(proxy)
	readOnlyAgent := New(nil, reg, NewSession("sys"), Options{ReadOnlyExecution: true}, event.Discard)
	declineArgs := `{"action":"decline","capability_id":"skill:review","reason":"not needed"}`
	decline := readOnlyAgent.executeOne(context.Background(), provider.ToolCall{ID: "decline-1", Name: "use_capability", Arguments: declineArgs})
	if !decline.blocked {
		t.Fatalf("decline outcome = %+v, want block", decline)
	}
	if gate := ledger.CheckFinalGate(); gate.Reason == "" {
		t.Fatal("read-only decline mutated the capability ledger")
	}

	ordinary := New(nil, reg, NewSession("sys"), Options{}, event.Discard)
	allowed := ordinary.executeOne(context.Background(), provider.ToolCall{ID: "decline-2", Name: "use_capability", Arguments: declineArgs})
	if allowed.blocked || allowed.errMsg != "" {
		t.Fatalf("ordinary executor decline outcome = %+v", allowed)
	}
	if gate := ledger.CheckFinalGate(); gate.Reason != "" {
		t.Fatalf("ordinary decline did not update ledger: %+v", gate)
	}
}

func TestReadOnlyExecutionDoesNotStartUntrustedUnconnectedMCP(t *testing.T) {
	host := plugin.NewHost()
	defer host.Close()
	proxy := NewUseCapabilityTool(context.Background(), host, []plugin.Spec{{
		Name: "lazy", Type: "stdio", Command: "reasonix-test-definitely-missing-binary",
	}}, tool.NewRegistry(), capability.NewLedger(), nil, nil)
	reg := tool.NewRegistry()
	reg.Add(proxy)
	a := New(nil, reg, NewSession("sys"), Options{ReadOnlyExecution: true}, event.Discard)
	out := a.executeOne(context.Background(), provider.ToolCall{
		ID: "lazy-1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:lazy/read_thing","arguments":{}}`,
	})
	if !out.blocked {
		t.Fatalf("lazy MCP outcome = %+v, want block", out)
	}
	if host.HasClient("lazy") {
		t.Fatal("read-only Agent started an unconnected MCP server")
	}
}

func receiptReaderMCPServer(t *testing.T, schemaDrift *atomic.Bool, toolCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if request.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "receipt-reader", "version": "1"}}
		case "tools/list":
			schemaType := "string"
			if schemaDrift != nil && schemaDrift.Load() {
				schemaType = "number"
			}
			result = map[string]any{"tools": []map[string]any{{
				"name": "search", "description": "search",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": schemaType}}},
				"annotations": map[string]any{"readOnlyHint": true},
			}}}
		case "tools/call":
			toolCalls.Add(1)
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "reader result"}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
	}))
}

func TestReadOnlyExecutionStartsReceiptMatchedUnconnectedMCPReader(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	var toolCalls atomic.Int32
	server := receiptReaderMCPServer(t, nil, &toolCalls)
	defer server.Close()

	manager := mcptrust.NewManager(filepath.Join(t.TempDir(), mcptrust.StateFilename), t.TempDir())
	spec := plugin.Spec{
		Name: "receipt-reader", Type: "http", URL: server.URL,
		TrustManager: manager, ConfigSource: "workspace_config",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := plugin.SetSpecTrust(ctx, spec, "session"); err != nil {
		t.Fatal(err)
	}

	host := plugin.NewHost()
	defer host.Close()
	proxy := NewUseCapabilityTool(ctx, host, []plugin.Spec{spec}, tool.NewRegistry(), capability.NewLedger(), nil, nil)
	reg := tool.NewRegistry()
	reg.Add(proxy)
	a := New(nil, reg, NewSession("sys"), Options{ReadOnlyExecution: true}, event.Discard)
	out := a.executeOne(ctx, provider.ToolCall{
		ID: "trusted-lazy-1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:receipt-reader/search","arguments":{}}`,
	})
	if out.blocked || out.errMsg != "" || !strings.Contains(out.output, "reader result") {
		t.Fatalf("trusted lazy reader outcome = %+v", out)
	}
	if got := toolCalls.Load(); got != 1 {
		t.Fatalf("reader tools/call count = %d, want 1", got)
	}
}

func TestReadOnlyExecutionBlocksReceiptSchemaDriftBeforeToolCall(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	var schemaDrift atomic.Bool
	var toolCalls atomic.Int32
	server := receiptReaderMCPServer(t, &schemaDrift, &toolCalls)
	defer server.Close()

	manager := mcptrust.NewManager(filepath.Join(t.TempDir(), mcptrust.StateFilename), t.TempDir())
	spec := plugin.Spec{
		Name: "receipt-reader", Type: "http", URL: server.URL,
		TrustManager: manager, ConfigSource: "workspace_config",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := plugin.SetSpecTrust(ctx, spec, "session"); err != nil {
		t.Fatal(err)
	}
	schemaDrift.Store(true)

	host := plugin.NewHost()
	defer host.Close()
	proxy := NewUseCapabilityTool(ctx, host, []plugin.Spec{spec}, tool.NewRegistry(), capability.NewLedger(), nil, nil)
	reg := tool.NewRegistry()
	reg.Add(proxy)
	a := New(nil, reg, NewSession("sys"), Options{ReadOnlyExecution: true}, event.Discard)
	out := a.executeOne(ctx, provider.ToolCall{
		ID: "drifted-lazy-1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:receipt-reader/search","arguments":{}}`,
	})
	if out.errMsg == "" || !strings.Contains(out.output, "requires parent-session re-verification") {
		t.Fatalf("drifted lazy reader outcome = %+v", out)
	}
	if got := toolCalls.Load(); got != 0 {
		t.Fatalf("drifted reader tools/call count = %d, want 0", got)
	}
}

func TestReadOnlyExecutionDoesNotMarkUnknownCapabilityUnavailable(t *testing.T) {
	ledger := capability.NewLedger()
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, tool.NewRegistry(), ledger, nil, nil)
	reg := tool.NewRegistry()
	reg.Add(proxy)
	a := New(nil, reg, NewSession("sys"), Options{ReadOnlyExecution: true}, event.Discard)
	out := a.executeOne(context.Background(), provider.ToolCall{
		ID: "missing-1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:missing/read","arguments":{}}`,
	})
	if !out.blocked {
		t.Fatalf("unknown capability outcome = %+v, want block", out)
	}
	if _, ok := ledger.Get("mcp-tool:missing/read"); ok {
		t.Fatal("read-only Agent mutated the ledger for an unknown capability")
	}
}
func (completedProxyCallTool) ResolveCall(context.Context, json.RawMessage) (tool.ResolvedCall, error) {
	return tool.ResolvedCall{
		DisplayName:  "use_capability",
		ProxyAction:  "call",
		CapabilityID: "mcp-server:mock",
		SkipExecute:  true,
		ReadOnly:     true,
		Result:       "mcp-tool:mock/echo",
	}, nil
}

func TestUseCapabilityDeclineAndInspect(t *testing.T) {
	ledger := capability.NewLedger()
	ledger.SeedCandidates(capability.RouteDecision{Candidates: []capability.RouteCandidate{
		{Entry: capability.Entry{ID: "skill:review"}, Policy: capability.AutoUsePrefer},
	}})
	audit := &capability.Audit{}
	tl := NewUseCapabilityTool(context.Background(), nil, nil, tool.NewRegistry(), ledger, audit, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{
			ID: "skill:review", Kind: capability.KindSkill, Name: "review", Description: "review code", Status: capability.StatusReady,
		}}}
	})

	out, err := tl.Execute(context.Background(), json.RawMessage(`{"action":"inspect","capability_id":"skill:review"}`))
	if err != nil || !strings.Contains(out, "skill:review") {
		t.Fatalf("inspect: out=%q err=%v", out, err)
	}
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"action":"decline","capability_id":"skill:review","reason":"not needed"}`)); err != nil {
		t.Fatal(err)
	}
	if gate := ledger.CheckFinalGate(); gate.Reason != "" {
		t.Fatalf("after decline gate = %+v", gate)
	}
	if got := audit.Snapshot().Declines; got != 1 {
		t.Fatalf("decline audit = %d, want 1", got)
	}
	// Cannot decline require.
	ledger.SeedCandidates(capability.RouteDecision{Candidates: []capability.RouteCandidate{
		{Entry: capability.Entry{ID: "skill:must"}, Policy: capability.AutoUseRequire},
	}})
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"action":"decline","capability_id":"skill:must","reason":"no"}`)); err == nil {
		t.Fatal("expected decline of require to fail")
	}
}

func TestDedicatedSecurityReviewUsesCanonicalSkillCapabilityID(t *testing.T) {
	got := capabilityIDFromToolCall("security_review", json.RawMessage(`{"task":"audit auth"}`))
	if got != "skill:security-review" {
		t.Fatalf("capability ID = %q, want skill:security-review", got)
	}
}

func TestSkillInvocationUnavailableIsAudited(t *testing.T) {
	audit := &capability.Audit{}
	a := New(&scriptedProvider{name: "p"}, tool.NewRegistry(), NewSession("sys"), Options{
		CapabilityLedger: capability.NewLedger(),
		CapabilityAudit:  audit,
	}, event.Discard)
	a.noteCapabilityInvocation("run_skill", json.RawMessage(`{"name":"delivery-only"}`), fmt.Errorf("run_skill: %w", skill.ErrInvocationUnavailable))
	snap := audit.Snapshot()
	if snap.SkillInvocations != 1 || snap.SkillFailures != 1 || snap.SkillUnavailable != 1 {
		t.Fatalf("skill unavailable audit: invocations=%d failures=%d unavailable=%d",
			snap.SkillInvocations, snap.SkillFailures, snap.SkillUnavailable)
	}
}

func TestUseCapabilityProxyHonorsRealMCPPermissionDeny(t *testing.T) {
	// Register a fake MCP tool in the registry so resolve uses it without host.
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "mcp__github__search_issues", readOnly: true})
	tl := NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), nil, nil)

	resolved, err := tl.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:github/search_issues","arguments":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TargetName != "mcp__github__search_issues" {
		t.Fatalf("target = %q", resolved.TargetName)
	}
	if resolved.Target == nil {
		t.Fatal("expected resolved target tool")
	}
	gate := denyAllGate{}
	allow, reason, _ := gate.Check(context.Background(), resolved.TargetName, resolved.Args, resolved.ReadOnly)
	if allow || !strings.Contains(reason, "mcp__github__search_issues") {
		t.Fatalf("gate allow=%v reason=%q", allow, reason)
	}
}

func TestReviewReportToolValidatesSchema(t *testing.T) {
	tl := NewReviewReportTool()
	led := evidence.NewLedger()
	led.Record(evidence.ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"a.go"}`), true, true))
	ctx := evidence.WithLedger(context.Background(), led)
	if _, err := tl.Execute(ctx, json.RawMessage(`{"kind":"review","verdict":"pass","reviewed_paths":[]}`)); err == nil {
		t.Fatal("empty reviewed_paths should fail")
	}
	out, err := tl.Execute(ctx, json.RawMessage(`{"kind":"security","verdict":"block","reviewed_paths":["a.go"],"findings":[{"severity":"critical","summary":"secret"}]}`))
	if err != nil || !strings.Contains(out, "blocking") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestReviewReportRequiresHostReadEvidence(t *testing.T) {
	tl := NewReviewReportTool()
	// No ledger on ctx: fail closed.
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"kind":"review","verdict":"pass","reviewed_paths":["a.go"]}`)); err == nil {
		t.Fatal("expected failure without a host evidence ledger")
	}
	led := evidence.NewLedger()
	ctx := evidence.WithLedger(context.Background(), led)
	// Claimed paths without any host-observed read: rejected, names the path.
	_, err := tl.Execute(ctx, json.RawMessage(`{"kind":"review","verdict":"pass","reviewed_paths":["internal/agent/agent.go"]}`))
	if err == nil || !strings.Contains(err.Error(), "internal/agent/agent.go") {
		t.Fatalf("expected fake-coverage rejection naming the path, got %v", err)
	}
	// A successful read receipt makes the same report acceptable.
	led.Record(evidence.ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"internal/agent/agent.go"}`), true, true))
	if _, err := tl.Execute(ctx, json.RawMessage(`{"kind":"review","verdict":"pass","reviewed_paths":["internal/agent/agent.go"]}`)); err != nil {
		t.Fatalf("host-read path should be accepted: %v", err)
	}
	// A git-diff bash receipt with real printed output also counts.
	led2 := evidence.NewLedger()
	diffRec := evidence.ReceiptFromToolCall("bash", json.RawMessage(`{"command":"git diff -- internal/boot/boot.go"}`), true, true)
	diffRec.OutputBytes = 512
	led2.Record(diffRec)
	ctx2 := evidence.WithLedger(context.Background(), led2)
	if _, err := tl.Execute(ctx2, json.RawMessage(`{"kind":"review","verdict":"pass","reviewed_paths":["internal/boot/boot.go"]}`)); err != nil {
		t.Fatalf("diffed path should be accepted: %v", err)
	}
}

func TestReviewReportRejectsNonContentEvidence(t *testing.T) {
	tl := NewReviewReportTool()
	report := json.RawMessage(`{"kind":"review","verdict":"pass","reviewed_paths":["internal/agent/agent.go"]}`)

	// git status mentions the path but never shows content.
	led := evidence.NewLedger()
	led.Record(evidence.ReceiptFromToolCall("bash", json.RawMessage(`{"command":"git status --short -- internal/agent/agent.go"}`), true, true))
	if _, err := tl.Execute(evidence.WithLedger(context.Background(), led), report); err == nil {
		t.Fatal("git status must not count as review evidence")
	}
	// echo output containing the path shows nothing either.
	led = evidence.NewLedger()
	led.Record(evidence.ReceiptFromToolCall("bash", json.RawMessage(`{"command":"echo internal/agent/agent.go"}`), true, true))
	if _, err := tl.Execute(evidence.WithLedger(context.Background(), led), report); err == nil {
		t.Fatal("echo must not count as review evidence")
	}
	// Writing a file is not reviewing it.
	led = evidence.NewLedger()
	led.Record(evidence.ReceiptFromToolCall("write_file", json.RawMessage(`{"path":"internal/agent/agent.go"}`), true, false))
	if _, err := tl.Execute(evidence.WithLedger(context.Background(), led), report); err == nil {
		t.Fatal("a write receipt must not count as review evidence")
	}
	// A bare basename read must not satisfy a claim for a specific full path.
	led = evidence.NewLedger()
	led.Record(evidence.ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"agent.go"}`), true, true))
	if _, err := tl.Execute(evidence.WithLedger(context.Background(), led), report); err == nil {
		t.Fatal("reverse basename matching must not count as review evidence")
	}
	// Content-suppressing shell shapes: each produced-or-not output case must fail.
	bashCases := []struct {
		name    string
		command string
		output  int
	}{
		{"null redirect", "cat internal/agent/agent.go >/dev/null", 0},
		{"null redirect with output claim", "cat internal/agent/agent.go >/dev/null", 64},
		{"stat only", "git diff --stat -- internal/agent/agent.go", 64},
		{"name only", "git diff --name-only -- internal/agent/agent.go", 64},
		{"zero lines", "head -n 0 internal/agent/agent.go", 0},
		{"pipeline transform", "cat internal/agent/agent.go | wc -l", 8},
		{"and unrelated output", "git diff HEAD~1 -- internal/agent/agent.go && echo done", 512},
		{"or unrelated output", "git diff HEAD~1 -- internal/agent/agent.go || echo done", 512},
		{"separate unrelated output", "git diff HEAD~1 -- internal/agent/agent.go; echo done", 512},
		{"git show metadata", "git show HEAD -- internal/agent/agent.go", 512},
		{"substring superset", "cat internal/agent/agent.go.bak", 512},
	}
	for _, tc := range bashCases {
		led := evidence.NewLedger()
		rec := evidence.ReceiptFromToolCall("bash", json.RawMessage(`{"command":`+strconv.Quote(tc.command)+`}`), true, true)
		rec.OutputBytes = tc.output
		led.Record(rec)
		if _, err := tl.Execute(evidence.WithLedger(context.Background(), led), report); err == nil {
			t.Fatalf("%s (%q) must not count as review evidence", tc.name, tc.command)
		}
	}
	// Genuine content commands with real output still pass.
	for _, cmd := range []string{
		"cat internal/agent/agent.go",
		"git show HEAD:internal/agent/agent.go",
		"git diff HEAD~1 -- internal/agent/agent.go",
	} {
		led := evidence.NewLedger()
		rec := evidence.ReceiptFromToolCall("bash", json.RawMessage(`{"command":`+strconv.Quote(cmd)+`}`), true, true)
		rec.OutputBytes = 512
		led.Record(rec)
		if _, err := tl.Execute(evidence.WithLedger(context.Background(), led), report); err != nil {
			t.Fatalf("%q with real output should count as review evidence: %v", cmd, err)
		}
	}
}

func TestUseCapabilityServerConnectHonorsPermissionInPlanMode(t *testing.T) {
	host := plugin.NewHost()
	defer host.Close()
	specs := []plugin.Spec{{Name: "lazy", Type: "stdio", Command: "reasonix-test-definitely-missing-binary"}}
	reg := tool.NewRegistry()
	uc := NewUseCapabilityTool(context.Background(), host, specs, reg, capability.NewLedger(), nil, nil)
	reg.Add(uc)

	resolved, err := uc.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-server:lazy"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Target == nil || resolved.SkipExecute {
		t.Fatalf("expected deferred connect target, got %+v", resolved)
	}
	if resolved.TargetName != plugin.MCPConnectPermissionName("lazy") || resolved.ReadOnly {
		t.Fatalf("connect gating identity wrong: name=%q readOnly=%v", resolved.TargetName, resolved.ReadOnly)
	}
	policyGate := permission.NewGate(permission.New("ask", nil, nil, []string{plugin.MCPConnectPermissionName("lazy")}), nil)
	allow, _, err := policyGate.Check(context.Background(), resolved.TargetName, resolved.Args, resolved.ReadOnly)
	if err != nil || allow {
		t.Fatalf("exact MCP connect deny must block before spawn: allow=%v err=%v", allow, err)
	}
	deniedAgent := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"), Options{Gate: policyGate}, event.Discard)
	deniedAgent.SetPlanMode(true)
	denied := deniedAgent.executeOne(context.Background(), provider.ToolCall{
		ID: "deny", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-server:lazy"}`,
	})
	if !denied.blocked || host.HasClient("lazy") {
		t.Fatalf("exact connect deny must block before process start: outcome=%+v connected=%v", denied, host.HasClient("lazy"))
	}
	if host.HasClient("lazy") {
		t.Fatal("server-level resolution must not start the server")
	}
}

func TestOnDemandModelNameMatchesPluginCanonicalName(t *testing.T) {
	host := plugin.NewHost()
	defer host.Close()
	specs := []plugin.Spec{{Name: "lazy", Type: "stdio", Command: "reasonix-test-definitely-missing-binary"}}
	tl := NewUseCapabilityTool(context.Background(), host, specs, tool.NewRegistry(), capability.NewLedger(), nil, nil)
	for _, raw := range []string{"@model/tool", "search/issues", "with space", "plain_ok"} {
		resolved, err := tl.ResolveCall(context.Background(),
			json.RawMessage(`{"action":"call","capability_id":"mcp-tool:lazy/`+raw+`"}`))
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		want := plugin.ModelToolName("lazy", raw)
		if resolved.TargetName != want {
			t.Fatalf("raw %q: permission-checked name %q differs from executed canonical name %q — deny/ask rules would miss", raw, resolved.TargetName, want)
		}
	}
}

func TestOnDemandMCPToolUsesSharedApprovalResolution(t *testing.T) {
	host := plugin.NewHost()
	defer host.Close()
	specs := []plugin.Spec{
		{Name: "user", Type: "stdio", Command: "reasonix-test-definitely-missing-binary", ImplicitApproval: true},
		{Name: "scoped", Type: "stdio", Command: "reasonix-test-definitely-missing-binary", ImplicitApproval: true,
			ToolApprovalModes: map[string]string{"wipe": "prompt"}},
		{Name: "project", Type: "stdio", Command: "reasonix-test-definitely-missing-binary"},
	}
	tl := NewUseCapabilityTool(context.Background(), host, specs, tool.NewRegistry(), capability.NewLedger(), nil, nil)
	cases := []struct{ id, want string }{
		// A user-configured server carries implicit approval; the delivery
		// on-demand path must resolve it to direct approval exactly like the
		// direct MCP tool path, not fall back to the global posture.
		{"mcp-tool:user/write", tool.MCPApprovalApprove},
		{"mcp-tool:scoped/wipe", tool.MCPApprovalPrompt},
		{"mcp-tool:project/write", tool.MCPApprovalAuto},
	}
	for _, tc := range cases {
		resolved, err := tl.ResolveCall(context.Background(),
			json.RawMessage(`{"action":"call","capability_id":"`+tc.id+`"}`))
		if err != nil {
			t.Fatalf("%s: %v", tc.id, err)
		}
		policy, ok := resolved.Target.(tool.MCPApprovalPolicy)
		if !ok {
			t.Fatalf("%s: resolved target %T does not expose an MCP approval policy", tc.id, resolved.Target)
		}
		if got := policy.MCPApprovalMode(); got != tc.want {
			t.Fatalf("%s: on-demand approval mode = %q, want %q (plugin.Spec.ToolApprovalMode parity)", tc.id, got, tc.want)
		}
	}
}

func TestProxyCallAuditCountsOnAgentPath(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "mcp__github__search_issues", readOnly: true})
	audit := &capability.Audit{}
	uc := NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), audit, nil)
	reg.Add(uc)
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"),
		Options{CapabilityLedger: capability.NewLedger(), CapabilityAudit: audit}, event.Discard)
	out := a.executeOne(context.Background(), provider.ToolCall{
		ID: "1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:github/search_issues","arguments":{}}`,
	})
	if out.blocked || out.errMsg != "" {
		t.Fatalf("call failed: %+v", out)
	}
	if snap := audit.Snapshot(); snap.MCPCall != 1 || snap.MCPCallFailures != 0 {
		t.Fatalf("MCPCall=%d failures=%d, want 1/0", snap.MCPCall, snap.MCPCallFailures)
	}
}

func TestCompletedProxyCallCountsOnAgentSkipExecutePath(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(completedProxyCallTool{})
	ledger := capability.NewLedger()
	audit := &capability.Audit{}
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"),
		Options{CapabilityLedger: ledger, CapabilityAudit: audit}, event.Discard)
	out := a.executeOne(context.Background(), provider.ToolCall{
		ID: "1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-server:mock"}`,
	})
	if out.blocked || out.errMsg != "" {
		t.Fatalf("completed call failed: %+v", out)
	}
	if entry, ok := ledger.Get("mcp-server:mock"); !ok || entry.Outcome != capability.OutcomeSucceeded {
		t.Fatalf("completed call ledger = %+v, found=%v", entry, ok)
	}
	if snap := audit.Snapshot(); snap.MCPCall != 1 || snap.MCPCallFailures != 0 {
		t.Fatalf("completed call audit = %d/%d, want 1/0", snap.MCPCall, snap.MCPCallFailures)
	}
}

func TestCapabilityGateRecoveryIsAudited(t *testing.T) {
	reg := tool.NewRegistry()
	audit := &capability.Audit{}
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"),
		Options{DeliveryProfile: true, CapabilityLedger: capability.NewLedger(), CapabilityAudit: audit}, event.Discard)
	a.SeedCapabilityRoute(capability.RouteDecision{Candidates: []capability.RouteCandidate{
		{Entry: capability.Entry{ID: "skill:review"}, Policy: capability.AutoUseRequire},
	}})
	a.evidence.Record(evidence.ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"a.go"}`), true, true))
	if check := a.finalReadinessCheck(); check.reason == "" {
		t.Fatal("expected a require miss first")
	}
	a.capabilityLedger.MarkInvoked("skill:review")
	a.capabilityLedger.MarkSucceeded("skill:review")
	if check := a.finalReadinessCheck(); strings.Contains(check.reason, "required capabilities") {
		t.Fatalf("gate should be clean after success, reason=%q", check.reason)
	}
	if snap := audit.Snapshot(); snap.RequireRecovered != 1 {
		t.Fatalf("RequireRecovered=%d, want 1", snap.RequireRecovered)
	}
}

func TestRunSubAgentRequiresReviewReport(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "looks fine"}, {Type: provider.ChunkDone}},
	}}
	_, err := RunSubAgentWithSession(context.Background(), prov, tool.NewRegistry(), NewSession("sys"), "review it",
		Options{RequireReviewReportKind: evidence.ReviewKindReview}, event.Discard)
	if err == nil || !strings.Contains(err.Error(), "review_report") {
		t.Fatalf("expected missing-report failure, got %v", err)
	}
}

func TestUseCapabilityResolveCallIsSideEffectFree(t *testing.T) {
	host := plugin.NewHost()
	defer host.Close()
	specs := []plugin.Spec{{
		Name:              "lazy",
		Type:              "stdio",
		Command:           "reasonix-test-definitely-missing-binary",
		ReadOnlyToolNames: map[string]bool{"read_thing": true},
	}}
	tl := NewUseCapabilityTool(context.Background(), host, specs, tool.NewRegistry(), capability.NewLedger(), nil, nil)

	resolved, err := tl.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:lazy/do_write","arguments":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.SkipExecute || resolved.Target == nil {
		t.Fatalf("expected a deferred target, got %+v", resolved)
	}
	if resolved.ReadOnly {
		t.Fatal("unstarted, untrusted tool must resolve as a writer")
	}
	if host.HasClient("lazy") {
		t.Fatal("ResolveCall must not start the MCP server")
	}
	// Config-trusted read-only tool keeps its trust without a handshake.
	roResolved, err := tl.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:lazy/read_thing"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !roResolved.ReadOnly {
		t.Fatal("spec-trusted read-only tool should resolve read-only")
	}
	if host.HasClient("lazy") {
		t.Fatal("read-only resolution must not start the MCP server either")
	}
	// Execution is where the connect finally happens — and fails for the
	// missing binary, marking the capability unavailable.
	ledger := capability.NewLedger()
	tl.ledger = ledger
	if _, err := resolved.Target.Execute(context.Background(), resolved.Args); err == nil {
		t.Fatal("expected connect failure for missing binary")
	}
	if e, ok := ledger.Get("mcp-tool:lazy/do_write"); !ok || e.Outcome != capability.OutcomeUnavailable {
		t.Fatalf("expected unavailable outcome, got %+v ok=%v", e, ok)
	}
}

func TestUseCapabilityInspectDoesNotStartServer(t *testing.T) {
	host := plugin.NewHost()
	defer host.Close()
	specs := []plugin.Spec{{Name: "lazy", Type: "stdio", Command: "reasonix-test-definitely-missing-binary"}}
	tl := NewUseCapabilityTool(context.Background(), host, specs, tool.NewRegistry(), capability.NewLedger(), nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{
			ID: "mcp-server:lazy", Kind: capability.KindMCPServer, Name: "lazy", Source: "lazy", Status: capability.StatusConfigured,
		}}}
	})
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"action":"inspect","capability_id":"mcp-server:lazy"}`))
	if err != nil {
		t.Fatal(err)
	}
	if host.HasClient("lazy") {
		t.Fatal("inspect must not start the MCP server")
	}
	if !strings.Contains(out, "not connected") {
		t.Fatalf("inspect output should say the server is not connected: %q", out)
	}
}

func TestPlanModeBlocksInstalledWriteMCPResolvedThroughUseCapability(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(annotatedMCPTool{fakeTool: fakeTool{name: "mcp__github__create_issue", readOnly: false}, server: "github", raw: "create_issue"})
	reg.Add(annotatedMCPTool{fakeTool: fakeTool{name: "mcp__github__search_issues", readOnly: true}, server: "github", raw: "search_issues"})
	uc := NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), nil, nil)
	reg.Add(uc)
	gate := &mcpPermissionRecordingGate{allowNormal: true}
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"), Options{Gate: gate}, event.Discard)
	a.planMode.Store(true)

	out := a.executeOne(context.Background(), provider.ToolCall{
		ID: "1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:github/create_issue","arguments":{}}`,
	})
	if !out.blocked || gate.normalCalls != 0 {
		t.Fatalf("installed MCP writer should be blocked before permission, outcome=%+v calls=%d", out, gate.normalCalls)
	}
	// A read-only target still passes through the proxy in plan mode.
	out = a.executeOne(context.Background(), provider.ToolCall{
		ID: "2", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:github/search_issues","arguments":{}}`,
	})
	if out.blocked {
		t.Fatalf("read-only proxy call should pass in plan mode, got %+v", out)
	}
}

func TestPlanModeMCPStyleNameWithoutMetadataStillUsesPermission(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "mcp__github__create_issue", readOnly: false})
	uc := NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), nil, nil)
	reg.Add(uc)
	gate := &recordingPermissionGate{reason: "denied by ordinary permission"}
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"), Options{Gate: gate}, event.Discard)
	a.planMode.Store(true)

	out := a.executeOne(context.Background(), provider.ToolCall{
		ID: "1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:github/create_issue","arguments":{}}`,
	})
	if !out.blocked || !strings.Contains(out.output, "Plan mode") || len(gate.calls) != 0 {
		t.Fatalf("MCP-style name must be hard-blocked in Plan: outcome=%+v calls=%+v", out, gate.calls)
	}
}

func TestDestructiveMCPThroughUseCapabilityUsesFreshApproval(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(annotatedMCPTool{
		fakeTool:    fakeTool{name: "mcp__github__delete_issue", readOnly: false},
		server:      "github",
		raw:         "delete_issue",
		destructive: true,
	})
	uc := NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), nil, nil)
	reg.Add(uc)
	gate := &mcpPermissionRecordingGate{allowNormal: true, allowFresh: true}
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"), Options{Gate: gate}, event.Discard)
	a.planMode.Store(true)

	out := a.executeOne(context.Background(), provider.ToolCall{
		ID: "1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:github/delete_issue","arguments":{"number":1}}`,
	})
	if !out.blocked || gate.normalCalls != 0 || gate.freshCalls != 0 {
		t.Fatalf("destructive proxy outcome=%+v normal=%d fresh=%d subject=%q", out, gate.normalCalls, gate.freshCalls, gate.subject)
	}
}

func TestCapabilityGateAppliesToReadOnlyTasks(t *testing.T) {
	reg := tool.NewRegistry()
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"),
		Options{DeliveryProfile: true, CapabilityLedger: capability.NewLedger()}, event.Discard)
	a.SeedCapabilityRoute(capability.RouteDecision{Candidates: []capability.RouteCandidate{
		{Entry: capability.Entry{ID: "skill:review"}, Policy: capability.AutoUseRequire},
	}})
	// Only ordinary reads happened — no writer. The require gate must still hold.
	a.evidence.Record(evidence.ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"a.go"}`), true, true))
	check := a.finalReadinessCheck()
	if !strings.Contains(check.reason, "required capabilities") {
		t.Fatalf("read-only answer must not skip the require gate; reason = %q", check.reason)
	}
}

func TestStrictReadOnlyFailsClosedWithoutTrustAuthority(t *testing.T) {
	host := plugin.NewHost()
	defer host.Close()
	// No TrustManager: the compatibility path still classifies the config
	// reader for ordinary permissions, but it carries no receipt authority.
	specs := []plugin.Spec{{
		Name:              "lazy",
		Type:              "stdio",
		Command:           "reasonix-test-definitely-missing-binary",
		ReadOnlyToolNames: map[string]bool{"read_thing": true},
	}}
	tl := NewUseCapabilityTool(context.Background(), host, specs, tool.NewRegistry(), capability.NewLedger(), nil, nil)
	resolved, err := tl.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:lazy/read_thing"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.ReadOnly {
		t.Fatal("compatibility classification should still resolve read-only for ordinary sessions")
	}
	if readOnlyExecutionAllowsTrustedMCPStartup(resolved.Target) {
		t.Fatal("hint/config-classified reader without a trust store must not start hosts in strict mode")
	}
	reg := tool.NewRegistry()
	reg.Add(resolved.Target)
	if names := strictReadOnlyExecutionRegistry(reg).Names(); len(names) != 0 {
		t.Fatalf("strict registry admitted an authority-less MCP reader: %v", names)
	}
	if host.HasClient("lazy") {
		t.Fatal("strict evaluation must not start the MCP server")
	}
}
