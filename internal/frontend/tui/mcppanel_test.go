package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func enterMCPLine(m *model, line string) {
	m.composer.SetValue(line)
	run(m, press(m, "enter"))
}

func keyRune(m *model, r rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	return cmd
}

// /mcp opens the server panel instead of printing a list; Space flips the
// server under the cursor for this project and the list is read again.
func TestMCPPanelListsAndToggles(t *testing.T) {
	m, k := testModel(t)
	enterMCPLine(m, "/mcp")
	if m.mcp == nil || len(m.mcp.servers) != 3 {
		t.Fatalf("mcp = %+v", m.mcp)
	}
	if v := m.View().Content; !strings.Contains(v, "docs · ready · 2 tools · http · user") || !strings.Contains(v, "db · off") {
		t.Fatalf("panel:\n%s", v)
	}
	run(m, press(m, "down"))
	run(m, press(m, "down"))
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	run(m, cmd)
	calls := strings.Join(k.seen(), "\n")
	if !strings.Contains(calls, `POST /mcp/enabled {"enabled":true,"name":"mail","scope":"project"}`) {
		t.Fatalf("calls:\n%s", calls)
	}
	if strings.Count(calls, "GET /mcp") != 2 || m.mcp == nil || m.mcp.busy || m.mcp.sel != 2 {
		t.Fatalf("list was not read again with the cursor kept: %+v\n%s", m.mcp, calls)
	}
}

// A server a repository declares is only enabled after the launch line is shown
// and confirmed; r never connects a server that is off.
func TestMCPPanelConfirmsRepoDeclaredServerAndIgnoresReconnectWhenOff(t *testing.T) {
	m, k := testModel(t)
	enterMCPLine(m, "/mcp")
	run(m, press(m, "down"))
	run(m, keyRune(m, 'r'))
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	run(m, cmd)
	if strings.Contains(strings.Join(k.seen(), "\n"), "POST /mcp") {
		t.Fatalf("r or an unconfirmed Space posted:\n%s", strings.Join(k.seen(), "\n"))
	}
	if v := m.View().Content; !strings.Contains(v, "runs the repository-declared server db: node db.js --token ***") {
		t.Fatalf("no confirm line:\n%s", v)
	}
	run(m, keyRune(m, 'n'))
	if strings.Contains(strings.Join(k.seen(), "\n"), "POST /mcp") || m.mcp.confirm != "" {
		t.Fatal("n posted or kept the confirm")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	run(m, cmd)
	run(m, keyRune(m, 'y'))
	if !strings.Contains(strings.Join(k.seen(), "\n"), `POST /mcp/enabled {"enabled":true,"name":"db","scope":"project"}`) {
		t.Fatalf("y did not enable:\n%s", strings.Join(k.seen(), "\n"))
	}
}

func TestMCPPanelReconnectsAndShowsTools(t *testing.T) {
	m, k := testModel(t)
	enterMCPLine(m, "/mcp")
	m.mcp.servers[0].State = "failed"
	run(m, keyRune(m, 'r'))
	if !strings.Contains(strings.Join(k.seen(), "\n"), `POST /mcp/reconnect {"name":"docs"}`) {
		t.Fatalf("calls:\n%s", strings.Join(k.seen(), "\n"))
	}
	run(m, press(m, "enter"))
	v := m.View().Content
	if !strings.Contains(v, "search · read-only") || !strings.Contains(v, "purge · destructive") {
		t.Fatalf("detail:\n%s", v)
	}
	run(m, press(m, "esc"))
	if m.mcp == nil || m.mcp.detail {
		t.Fatal("esc did not step back to the list")
	}
	run(m, press(m, "esc"))
	if m.mcp != nil {
		t.Fatal("esc did not close the panel")
	}
}

func TestMCPArgumentsStillGoToTheKernel(t *testing.T) {
	m, k := testModel(t)
	enterMCPLine(m, "/mcp connect docs")
	if m.mcp != nil || !strings.Contains(strings.Join(k.seen(), "\n"), "POST /submit") {
		t.Fatalf("calls:\n%s", strings.Join(k.seen(), "\n"))
	}
}

func TestMCPConfirmLineStripsEscapesFromTheLaunchText(t *testing.T) {
	m, _ := testModel(t)
	enterMCPLine(m, "/mcp")
	run(m, press(m, "down"))
	m.mcp.servers[1].Launch = "node \x1b[2J\x1b]0;x\x07a\r\u202eb"
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	run(m, cmd)
	v := m.View().Content
	if strings.Contains(v, "\x1b[2J") || strings.Contains(v, "\x07") || strings.Contains(v, "\u202e") || !strings.Contains(v, "node a b") {
		t.Fatalf("confirm line:\n%q", v)
	}
}

// 1.x's MCP manager went into a server with l / Right, back out with h / Left,
// and closed from anywhere with q; docs/GUIDE.md still lists those keys.
func TestMCPPanelTakesTheOneXKeys(t *testing.T) {
	m, k := testModel(t)
	enterMCPLine(m, "/mcp")
	steps := []struct {
		name         string
		key          tea.KeyPressMsg
		open, detail bool
	}{
		{"l", tea.KeyPressMsg{Code: 'l', Text: "l"}, true, true},
		{"h", tea.KeyPressMsg{Code: 'h', Text: "h"}, true, false},
		{"right", tea.KeyPressMsg{Code: tea.KeyRight}, true, true},
		{"left", tea.KeyPressMsg{Code: tea.KeyLeft}, true, false},
		{"h on the list", tea.KeyPressMsg{Code: 'h', Text: "h"}, false, false},
	}
	for _, s := range steps {
		m.Update(s.key)
		if (m.mcp != nil) != s.open || (m.mcp != nil && m.mcp.detail != s.detail) {
			t.Fatalf("after %s: panel %+v, want open=%v detail=%v", s.name, m.mcp, s.open, s.detail)
		}
	}
	for _, path := range [][]tea.KeyPressMsg{nil, {{Code: tea.KeyEnter}}} {
		enterMCPLine(m, "/mcp")
		for _, key := range path {
			m.Update(key)
		}
		run(m, keyRune(m, 'q'))
		if m.mcp != nil {
			t.Fatalf("q left the panel open (detail=%v)", m.mcp.detail)
		}
	}
	if calls := strings.Join(k.seen(), "\n"); strings.Contains(calls, "POST /mcp") {
		t.Fatalf("moving through the panel changed a server:\n%s", calls)
	}
}
