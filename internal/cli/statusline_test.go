package cli

import (
	"context"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/agent"
	"reasonix/internal/agent/testutil"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestRunStatuslineCmd checks the custom status-line runner: it returns the
// first stdout line and forwards the JSON payload on stdin.
func TestRunStatuslineCmd(t *testing.T) {
	firstLineCmd := "printf 'row-one\\nrow-two\\n'"
	stdinCmd := "cat"
	failCmd := "exit 3"
	if runtime.GOOS == "windows" {
		firstLineCmd = "echo row-one & echo row-two"
		stdinCmd = "more"
		failCmd = "exit /b 3"
	}

	// Multi-line output collapses to the first row.
	if got := runStatuslineCmd(firstLineCmd, "{}"); got != "row-one" {
		t.Errorf("multi-line output should collapse to the first row, got %q", got)
	}
	// The JSON payload is delivered on stdin.
	if got := runStatuslineCmd(stdinCmd, `{"model":"deepseek"}`); got != `{"model":"deepseek"}` {
		t.Errorf("stdin payload not forwarded, got %q", got)
	}
	// A failing command yields an empty line, not an error.
	if got := runStatuslineCmd(failCmd, "{}"); got != "" {
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

func TestModelSwitchRefreshesCustomStatusline(t *testing.T) {
	oldCtrl := control.New(control.Options{Label: "old-model"})
	newCtrl := control.New(control.Options{Label: "new-model"})
	m := newChatTUI(oldCtrl, "", make(chan event.Event, 1), 80)
	m.statuslineCmd = "cat"
	m.statuslineOut = `{"model":"old-model"}`

	_, cmd := m.Update(modelSwitchMsg{
		ref:   "provider/new-model",
		ctrl:  newCtrl,
		label: "new-model",
	})
	if cmd == nil {
		t.Fatal("model switch should schedule commands")
	}
	if !statuslineCommandHasModel(cmd, "new-model") {
		t.Fatal("model switch did not refresh custom statusline with the new model")
	}
}

func statuslineCommandHasModel(cmd tea.Cmd, model string) bool {
	msg := cmd()
	switch msg := msg.(type) {
	case statuslineMsg:
		return strings.Contains(msg.out, `"model":"`+model+`"`)
	case tea.BatchMsg:
		for _, child := range msg {
			if child == nil {
				continue
			}
			if statuslineCommandHasModel(child, model) {
				return true
			}
		}
	}
	return false
}

func TestIdleStatuslineIsCompact(t *testing.T) {
	i18n.DetectLanguage("en")

	content := renderStatuslineView(t, false)
	plain := bottomStatusPlain(content)
	if !strings.Contains(plain, "Auto") || !strings.Contains(plain, "ready") {
		t.Fatalf("idle status line missing mode status:\n%s", plain)
	}
	if !strings.Contains(plain, "(shift+tab toggles plan · ctrl+y yolo)") {
		t.Fatalf("idle status line missing plan-toggle hint:\n%s", plain)
	}
	for _, old := range []string{"Shift-Tab", "Ctrl-O", "Ctrl-D", "Enter sends", "Esc clears/exits state", "PgUp/PgDn"} {
		if strings.Contains(plain, old) {
			t.Fatalf("idle status line should not contain %q:\n%s", old, plain)
		}
	}
	if strings.Contains(plain, "[auto]") {
		t.Fatalf("idle status line should use pill label, not bracketed tag:\n%s", plain)
	}
	if !strings.Contains(content, "\x1b[48;2;245;158;11m") {
		t.Fatalf("Auto status line should use amber pill background, got:\n%q", content)
	}
}

func TestYoloStatuslineUsesDangerPill(t *testing.T) {
	i18n.DetectLanguage("en")

	content := renderStatuslineView(t, true)
	plain := bottomStatusPlain(content)
	if !strings.Contains(plain, "YOLO") || !strings.Contains(plain, "approvals skipped") || !strings.Contains(plain, "(shift+tab toggles plan · ctrl+y yolo)") {
		t.Fatalf("YOLO status line missing warning text:\n%s", plain)
	}
	if strings.Contains(plain, "[YOLO]") {
		t.Fatalf("YOLO status line should use a pill label, not bracketed tag:\n%s", plain)
	}
	if !strings.Contains(content, "\x1b[48;2;229;72;77m") {
		t.Fatalf("YOLO status line should use danger pill background, got:\n%q", content)
	}
}

func TestPlanStatuslineUsesBluePill(t *testing.T) {
	i18n.DetectLanguage("en")

	content := renderPlanStatuslineView(t)
	plain := bottomStatusPlain(content)
	if !strings.Contains(plain, "Plan") || !strings.Contains(plain, "ready") || !strings.Contains(plain, "(shift+tab toggles plan · ctrl+y yolo)") {
		t.Fatalf("plan status line missing mode status:\n%s", plain)
	}
	if !strings.Contains(content, "\x1b[48;2;37;99;235m") {
		t.Fatalf("Plan status line should use blue pill background, got:\n%q", content)
	}
}

func TestStatuslineCycleHintFollowsLanguage(t *testing.T) {
	i18n.DetectLanguage("zh")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	content := renderStatuslineView(t, false)
	plain := bottomStatusPlain(content)
	if !strings.Contains(plain, "Auto") || !strings.Contains(plain, "就绪") || !strings.Contains(plain, "(shift+tab 切换计划 · ctrl+y yolo)") {
		t.Fatalf("localized plan-toggle hint missing:\n%s", plain)
	}
	if strings.Contains(plain, "ready") || strings.Contains(plain, "shift+tab toggles plan · ctrl+y yolo") {
		t.Fatalf("localized status line should not fall back to English:\n%s", plain)
	}
}

func TestDesktopShortcutStatuslineUsesPlanToggleHint(t *testing.T) {
	i18n.DetectLanguage("en")

	content := renderStatuslineViewWithShortcutLayout(t, "desktop")
	plain := bottomStatusPlain(content)
	if !strings.Contains(plain, "Ask") || !strings.Contains(plain, "(shift+tab toggles plan · ctrl+y yolo)") {
		t.Fatalf("desktop shortcut status line missing unified plan-toggle hint:\n%s", plain)
	}
	if strings.Contains(plain, "ask/auto/plan") {
		t.Fatalf("desktop shortcut status line should not advertise Ask/Auto/Plan cycling:\n%s", plain)
	}
}

func TestStatuslineShowsEffort(t *testing.T) {
	i18n.DetectLanguage("en")

	content := renderStatuslineViewWithEffort(t, "auto")
	lines := bottomStatusPlainLines(content)
	if len(lines) != 2 {
		t.Fatalf("status block lines = %d, want 2:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[0], "effort auto") {
		t.Fatalf("mode row should show effort:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(lines[1], "effort auto") {
		t.Fatalf("data row should not show effort:\n%s", strings.Join(lines, "\n"))
	}
}

func TestStatuslineKeepsCacheRatesInPrimaryDataRow(t *testing.T) {
	i18n.DetectLanguage("en")

	content := renderStatuslineViewWithCache(t)
	lines := bottomStatusPlainLines(content)
	if len(lines) != 2 {
		t.Fatalf("status block lines = %d, want 2:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	want := "deepseek-v4-flash · turn hit 90.00% · avg 90.00%"
	if !strings.Contains(lines[1], want) {
		t.Fatalf("data row should keep cache rates next to model:\n%s", strings.Join(lines, "\n"))
	}
}

func TestStatuslinePutsGitIdentityOnModeRow(t *testing.T) {
	i18n.DetectLanguage("en")

	content := renderStatuslineViewWithGitAndEffort(t)
	lines := bottomStatusPlainLines(content)
	if len(lines) != 2 {
		t.Fatalf("status block lines = %d, want 2:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[0], "effort auto · Reasonix@codex/demo (+3 -1 ?2)") {
		t.Fatalf("mode row should include effort before git identity:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(lines[1], "Reasonix@codex/demo") {
		t.Fatalf("data row should not include git identity:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[1], "deepseek-v4-flash") || strings.Contains(lines[1], "effort auto") {
		t.Fatalf("data row should keep model without effort:\n%s", strings.Join(lines, "\n"))
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
	ctrl.SetAutoApproveTools(yolo)
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return next.(chatTUI).View().Content
}

func renderStatuslineViewWithShortcutLayout(t *testing.T, layout string) string {
	t.Helper()

	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.cfg = config.Default()
	if err := m.cfg.SetUIShortcutLayout(layout); err != nil {
		t.Fatal(err)
	}
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

func renderStatuslineViewWithGitAndEffort(t *testing.T) string {
	t.Helper()

	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 120)
	m.label = "deepseek-v4-flash"
	m.effortLevel = "auto"
	m.gitStatus = gitStatus{
		Repo:      "Reasonix",
		Branch:    "codex/demo",
		Added:     3,
		Removed:   1,
		Untracked: 2,
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	return next.(chatTUI).View().Content
}

func renderStatuslineViewWithCache(t *testing.T) string {
	t.Helper()

	prov := testutil.NewMock("deepseek-v4-flash", testutil.Turn{
		Text: "ok",
		Usage: &provider.Usage{
			CacheHitTokens:   900,
			CacheMissTokens:  100,
			CompletionTokens: 50,
			PromptTokens:     1000,
			TotalTokens:      1050,
		},
	})
	exec := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{MaxSteps: 1, ContextWindow: 200_000}, event.Discard)
	if err := exec.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("seed agent usage: %v", err)
	}
	ctrl := control.New(control.Options{Executor: exec})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 160)
	m.label = "deepseek-v4-flash"
	m.effortLevel = "auto"
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
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
	return strings.Join(bottomStatusPlainLines(content), "\n")
}

func bottomStatusPlainLines(content string) []string {
	lines := strings.Split(ansi.Strip(content), "\n")
	if len(lines) < 2 {
		return lines
	}
	return lines[len(lines)-2:]
}
