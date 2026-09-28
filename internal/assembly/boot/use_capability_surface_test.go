package boot

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent/testutil"
)

func TestUnifiedSurfaceExposesUseCapabilityForAllRoleSettings(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	approveWorkspace(t, dir)
	registerBootTokenProfileTestProvider()

	var surfaces [][]string
	for _, mode := range []string{"", TokenModeEconomy, TokenModeFull, TokenModeDelivery, "light", "balanced"} {
		// Delivery routes through the semantic router before the agent samples,
		// so it draws a turn of its own. The first turn answers the router with
		// an empty selection; the surface under test is the agent's request.
		prov := testutil.NewMock("ucap-"+mode, testutil.Turn{Text: "[]"}, testutil.Turn{Text: "done"})
		setBootTokenProfileTestProvider(t, prov)
		ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: mode, AgentPreset: mode})
		if err != nil {
			t.Fatalf("Build(%q): %v", mode, err)
		}
		if err := ctrl.Run(context.Background(), "hello"); err != nil {
			ctrl.Close()
			t.Fatalf("Run(%q): %v", mode, err)
		}
		agentReqs := agentRequests(prov.Requests())
		if len(agentReqs) != 1 {
			ctrl.Close()
			t.Fatalf("agent requests(%q)=%d, want 1", mode, len(agentReqs))
		}
		agentReq := agentReqs[0]
		surfaces = append(surfaces, toolSchemaNames(agentReq.Tools))
		if !requestHasTool(agentReq, "use_capability") {
			ctrl.Close()
			t.Fatalf("%q missing use_capability: %v", mode, toolSchemaNames(agentReq.Tools))
		}
		if requestHasTool(agentReq, "connect_tool_source") {
			ctrl.Close()
			t.Fatalf("%q still exposes connect_tool_source", mode)
		}
		// grep stays registered for dispatch
		reg := map[string]bool{}
		for _, e := range ctrl.AllToolContractEntries() {
			reg[e.Name] = true
		}
		if !reg["grep"] {
			ctrl.Close()
			t.Fatalf("%q registry missing grep for use_capability dispatch", mode)
		}
		ctrl.Close()
	}
	for i := 1; i < len(surfaces); i++ {
		if !reflect.DeepEqual(surfaces[0], surfaces[i]) {
			t.Fatalf("provider-visible surface diverged\nbase=%v\nother=%v", surfaces[0], surfaces[i])
		}
	}
}

func TestUseCapabilityCallDispatchesHiddenGrep(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "a.go", "package a\n// needle_token\n")
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	approveWorkspace(t, dir)
	registerBootTokenProfileTestProvider()
	args, _ := json.Marshal(map[string]any{
		"action":        "call",
		"capability_id": "tool:grep",
		"arguments":     map[string]any{"pattern": "needle_token", "path": "."},
	})
	prov := testutil.NewMock("ucap-grep",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "g1", Name: "use_capability", Arguments: string(args)}}},
		testutil.Turn{Text: "found"},
	)
	setBootTokenProfileTestProvider(t, prov)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "find needle"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Schema must stay lean after the call
	for _, req := range prov.Requests() {
		if requestHasTool(req, "grep") {
			t.Fatalf("grep must not appear top-level after use_capability: %v", toolSchemaNames(req.Tools))
		}
	}
	var toolOut strings.Builder
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool {
			toolOut.WriteString(msg.Content)
		}
	}
	if toolOut.Len() == 0 {
		t.Fatal("expected use_capability/grep tool output")
	}
	if strings.Contains(toolOut.String(), "unavailable") || strings.Contains(toolOut.String(), "not registered") {
		t.Fatalf("use_capability failed to dispatch grep: %s", toolOut.String())
	}
}
