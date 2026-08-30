package boot

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/agent/testutil"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
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
	registerBootTokenProfileTestProvider()

	var surfaces [][]string
	for _, mode := range []string{"", "economy", TokenModeFull, TokenModeDelivery, "light", "balanced"} {
		prov := testutil.NewMock("ucap-"+mode, testutil.Turn{Text: "done"})
		setBootTokenProfileTestProvider(t, prov)
		ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: mode, AgentPreset: mode})
		if err != nil {
			t.Fatalf("Build(%q): %v", mode, err)
		}
		if err := ctrl.Run(context.Background(), "hello"); err != nil {
			ctrl.Close()
			t.Fatalf("Run(%q): %v", mode, err)
		}
		reqs := prov.Requests()
		if len(reqs) != 1 {
			ctrl.Close()
			t.Fatalf("requests(%q)=%d", mode, len(reqs))
		}
		surfaces = append(surfaces, toolSchemaNames(reqs[0].Tools))
		if !requestHasTool(reqs[0], "use_capability") {
			ctrl.Close()
			t.Fatalf("%q missing use_capability: %v", mode, toolSchemaNames(reqs[0].Tools))
		}
		if requestHasTool(reqs[0], "connect_tool_source") {
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
		if !reg["set_session_title"] {
			ctrl.Close()
			t.Fatalf("%q registry missing set_session_title for use_capability dispatch", mode)
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

func TestUseCapabilitySetsOnlyTheCurrentSessionTitle(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	sessionDir := robustTempDir(t)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	registerBootTokenProfileTestProvider()
	args, _ := json.Marshal(map[string]any{
		"action":        "call",
		"capability_id": "tool:set_session_title",
		"arguments":     map[string]any{"title": "Current integration task"},
	})
	prov := testutil.NewMock("ucap-session-title",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "t1", Name: "use_capability", Arguments: string(args)}}},
		testutil.Turn{Text: "renamed"},
	)
	setBootTokenProfileTestProvider(t, prov)
	var projectedPath string
	ctrl, err := Build(context.Background(), Options{
		SessionDir:           sessionDir,
		Sink:                 event.Discard,
		HeadlessApprovalMode: control.ToolApprovalYolo,
		OnSessionTitleChanged: func(gotDir, gotPath, gotTitle string) error {
			if gotDir != sessionDir || gotTitle != "Current integration task" {
				t.Fatalf("projection = %q %q %q", gotDir, gotPath, gotTitle)
			}
			projectedPath = gotPath
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	ctrl.EnsureSessionPath()
	if err := ctrl.Run(context.Background(), "name this session"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if projectedPath == "" || projectedPath != ctrl.SessionPath() {
		t.Fatalf("projected path = %q, current = %q", projectedPath, ctrl.SessionPath())
	}
	meta, ok, err := agent.LoadBranchMeta(projectedPath)
	if err != nil || !ok || meta.CustomTitle != "Current integration task" {
		t.Fatalf("meta = %+v, ok=%v, err=%v", meta, ok, err)
	}
	for _, req := range prov.Requests() {
		if requestHasTool(req, "set_session_title") {
			t.Fatalf("set_session_title leaked into provider-visible schemas: %v", toolSchemaNames(req.Tools))
		}
	}
}
