package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/capability"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
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

func TestUseCapabilityServerConnectGatedAndPlanModeBlocked(t *testing.T) {
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
	// Plan mode blocks the connect (subprocess spawn is not read-only work).
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"), Options{}, event.Discard)
	a.planMode.Store(true)
	out := a.executeOne(context.Background(), provider.ToolCall{
		ID: "1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-server:lazy"}`,
	})
	if !out.blocked || !strings.Contains(out.errMsg, "plan mode") {
		t.Fatalf("plan mode must block server connect, got %+v", out)
	}
	if host.HasClient("lazy") {
		t.Fatal("blocked connect must not have started the server")
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

func TestPlanModeBlocksWriteMCPThroughUseCapability(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "mcp__github__create_issue", readOnly: false})
	reg.Add(fakeTool{name: "mcp__github__search_issues", readOnly: true})
	uc := NewUseCapabilityTool(context.Background(), nil, nil, reg, capability.NewLedger(), nil, nil)
	reg.Add(uc)
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"), Options{}, event.Discard)
	a.planMode.Store(true)

	out := a.executeOne(context.Background(), provider.ToolCall{
		ID: "1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:github/create_issue","arguments":{}}`,
	})
	if !out.blocked {
		t.Fatalf("plan mode must block a write MCP tool behind the proxy, got %+v", out)
	}
	if !strings.Contains(out.errMsg, "plan mode") {
		t.Fatalf("errMsg = %q", out.errMsg)
	}
	// A trusted read-only target still passes through the proxy in plan mode.
	out = a.executeOne(context.Background(), provider.ToolCall{
		ID: "2", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:github/search_issues","arguments":{}}`,
	})
	if out.blocked {
		t.Fatalf("trusted read-only proxy call should pass in plan mode, got %+v", out)
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
