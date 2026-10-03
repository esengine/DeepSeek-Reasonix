package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManagementCommandsOpenPanels(t *testing.T) {
	for _, command := range []string{"/model", "/provider", "/skills", "/skill", "/mcp"} {
		t.Run(command, func(t *testing.T) {
			m, k := testModel(t)
			typeText(m, command)
			run(m, press(m, "enter"))
			if calledWith(k, "POST /submit") {
				t.Fatal("management panel command went to Submit")
			}
			if !strings.Contains(m.View().Content, "Search:") {
				t.Fatal("management panel did not open")
			}
			run(m, press(m, "esc"))
			if m.bottomLines().composerAt < 0 {
				t.Fatal("cancel did not restore composer")
			}
		})
	}
}

func TestSetupStatusLabelsTheOpenPanel(t *testing.T) {
	m, _ := testModel(t)
	typeText(m, "/setup")
	run(m, press(m, "enter"))
	if !strings.Contains(m.stateText(), "Configure connection") {
		t.Fatalf("setup status = %s", m.stateText())
	}
}

func TestPanelSelectionDrivesKernel(t *testing.T) {
	for _, tc := range []struct{ command, call string }{
		{"/model", `POST /model {"ref":"alpha/m1"}`},
		{"/provider", `POST /model {"ref":"alpha/m1"}`},
		{"/skills", `POST /skills/enabled {"enabled":false,"name":"review","scope":"project"}`},
		{"/mcp", `POST /mcp/enabled {"enabled":true,"name":"docs","scope":"project"}`},
	} {
		t.Run(tc.command, func(t *testing.T) {
			m, k := testModel(t)
			typeText(m, tc.command)
			run(m, press(m, "enter"))
			if tc.command == "/provider" {
				run(m, press(m, "enter"))
			}
			run(m, press(m, "enter"))
			if !calledWith(k, tc.call) {
				t.Fatalf("selection did not drive %s: %v", tc.call, k.seen())
			}
		})
	}
}

func TestFirstRunWarningsUseKernelFacts(t *testing.T) {
	m, _ := testModel(t)
	var state SetupState
	if err := json.Unmarshal([]byte(`{"required":true,"provider":"fixture","keyEnv":"FIXTURE_KEY"}`), &state); err != nil {
		t.Fatal(err)
	}
	run(m, m.onSetupState(setupStateMsg{state: state}))
	got := screenText(m)
	for _, want := range []string{"provider fixture missing env FIXTURE_KEY", "Selected model is missing its API key."} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
}

func TestMCPAddRequiresPreviewAndConfirmation(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "/mcp add")
	run(m, press(m, "enter"))
	m.insertPaste(`{"mcpServers":{"fixture":{"command":"fixture-server","args":["--serve"]}}}`)
	run(m, press(m, "enter"))
	if !calledWith(k, "POST /mcp/parse") || calledWith(k, "POST /mcp/install") {
		t.Fatalf("preview should not install: %v", k.seen())
	}
	if got := m.View().Content; !strings.Contains(got, "fixture-server") || !strings.Contains(got, "Runs a local command") {
		t.Fatalf("preview omits execution or risks: %s", got)
	}
	run(m, press(m, "enter"))
	if !calledWith(k, "POST /mcp/install") {
		t.Fatal("confirmed preview did not install")
	}
}
