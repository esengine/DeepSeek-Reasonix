package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

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

func TestIdleStatuslineIsCompact(t *testing.T) {
	i18n.DetectLanguage("en")

	content := renderStatuslineView(t, false)
	plain := bottomStatusPlain(content)
	if !strings.Contains(plain, "Auto · ready") {
		t.Fatalf("idle status line missing mode status:\n%s", plain)
	}
	for _, old := range []string{"Shift-Tab", "Ctrl-O", "Ctrl-D", "Enter sends", "Esc clears/exits state", "PgUp/PgDn"} {
		if strings.Contains(plain, old) {
			t.Fatalf("idle status line should not contain %q:\n%s", old, plain)
		}
	}
	if strings.Contains(plain, "[auto]") {
		t.Fatalf("idle status line should use Auto label, not bracketed tag:\n%s", plain)
	}
}

func TestYoloStatuslineUsesDangerPill(t *testing.T) {
	i18n.DetectLanguage("en")

	content := renderStatuslineView(t, true)
	plain := bottomStatusPlain(content)
	if !strings.Contains(plain, "YOLO") || !strings.Contains(plain, "approvals skipped") {
		t.Fatalf("YOLO status line missing warning text:\n%s", plain)
	}
	if strings.Contains(plain, "[YOLO]") {
		t.Fatalf("YOLO status line should use a pill label, not bracketed tag:\n%s", plain)
	}
	if !strings.Contains(content, "\x1b[48;2;229;72;77m") {
		t.Fatalf("YOLO status line should use desktop danger red background, got:\n%q", content)
	}
}

func TestPlanStatuslineUsesBluePill(t *testing.T) {
	i18n.DetectLanguage("en")

	content := renderPlanStatuslineView(t)
	plain := bottomStatusPlain(content)
	if !strings.Contains(plain, "Plan") || !strings.Contains(plain, "ready") {
		t.Fatalf("plan status line missing mode status:\n%s", plain)
	}
	if !strings.Contains(content, "\x1b[48;2;37;99;235m") {
		t.Fatalf("Plan status line should use blue background, got:\n%q", content)
	}
}

func TestStatuslineShowsEffort(t *testing.T) {
	i18n.DetectLanguage("en")

	content := renderStatuslineViewWithEffort(t, "auto")
	plain := bottomStatusPlain(content)
	if !strings.Contains(plain, "deepseek-v4-flash · effort auto") {
		t.Fatalf("status data line should show effort:\n%s", plain)
	}
}

func TestStatuslineExplicitEffortUsesBlue(t *testing.T) {
	i18n.DetectLanguage("en")

	content := renderStatuslineViewWithEffort(t, "max")
	plain := bottomStatusPlain(content)
	if !strings.Contains(plain, "effort max") {
		t.Fatalf("status data line should show explicit effort:\n%s", plain)
	}
	if !strings.Contains(content, "\x1b[1;38;2;37;99;235m") {
		t.Fatalf("explicit effort should use blue foreground, got:\n%q", content)
	}
}

func TestRefreshEffortStatusUsesCurrentModel(t *testing.T) {
	isolateUserConfig(t)

	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.modelRef = "deepseek-flash/deepseek-v4-flash"
	m.refreshEffortStatus()
	if m.effortLevel != "auto" {
		t.Fatalf("effortLevel = %q, want auto", m.effortLevel)
	}
}

func renderStatuslineView(t *testing.T, yolo bool) string {
	t.Helper()

	ctrl := control.New(control.Options{})
	ctrl.SetBypass(yolo)
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return next.(chatTUI).View().Content
}

func renderStatuslineViewWithEffort(t *testing.T, effort string) string {
	t.Helper()

	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.label = "deepseek-v4-flash"
	m.effortLevel = effort
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return next.(chatTUI).View().Content
}

func renderPlanStatuslineView(t *testing.T) string {
	t.Helper()

	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.planMode = true
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return next.(chatTUI).View().Content
}

func bottomStatusPlain(content string) string {
	lines := strings.Split(ansi.Strip(content), "\n")
	if len(lines) < 2 {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-2:], "\n")
}
