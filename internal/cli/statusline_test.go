package cli

import "testing"

// TestRunStatuslineCmd checks the custom status-line runner: it returns the
// first stdout line and forwards the JSON payload on stdin.
func TestRunStatuslineCmd(t *testing.T) {
	// Multi-line output collapses to the first row.
	if got := runStatuslineCmd("printf 'row-one\\nrow-two\\n'", "{}"); got != "row-one" {
		t.Errorf("multi-line output should collapse to the first row, got %q", got)
	}
	// The JSON payload is delivered on stdin.
	if got := runStatuslineCmd("cat", `{"model":"deepseek"}`); got != `{"model":"deepseek"}` {
		t.Errorf("stdin payload not forwarded, got %q", got)
	}
	// A failing command yields an empty line, not an error.
	if got := runStatuslineCmd("exit 3", "{}"); got != "" {
		t.Errorf("failed command should yield empty, got %q", got)
	}
}

// TestRunStatuslineDisabled confirms no command means no work (nil cmd), without
// touching the controller.
func TestRunStatuslineDisabled(t *testing.T) {
	m := chatTUI{} // no statuslineCmd, nil ctrl
	if cmd := m.runStatusline(); cmd != nil {
		t.Error("an unconfigured status line must return a nil tea.Cmd")
	}
}
