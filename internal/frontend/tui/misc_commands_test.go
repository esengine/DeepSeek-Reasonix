package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/base/testenv"
	"reasonix/internal/frontend/termrender"
)

func miscModel(t *testing.T) (*model, *recordingKernel, string) {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	m, k := testModel(t)
	return m, k, filepath.Join(home, "config.toml")
}

func sendLine(m *model, line string) {
	typeText(m, line)
	run(m, press(m, "enter"))
}

func notices(m *model) string {
	var b strings.Builder
	for _, it := range m.tr.Items {
		if it.Kind == ItemNotice {
			b.WriteString(it.Text + "\n")
		}
	}
	return b.String()
}

func TestTodoDismissesTheListWithoutAskingTheKernel(t *testing.T) {
	m, k, _ := miscModel(t)
	m.todos = []TodoItem{{Content: "fix", Status: "in_progress"}}
	sendLine(m, "/todo")
	if len(m.todos) != 0 || strings.Contains(m.View().Content, "To-dos") {
		t.Fatal("the list stayed up")
	}
	if !strings.Contains(notices(m), i18n.M.SlashTodoCleared) {
		t.Fatalf("no confirmation: %q", notices(m))
	}
	if submitted(k) != "" {
		t.Fatal("a local command reached the kernel")
	}
}

func TestDiffFoldTogglesTheFoldLimit(t *testing.T) {
	m, k, _ := miscModel(t)
	t.Cleanup(func() { diffFoldLines = diffPreviewLines })
	sendLine(m, "/diff-fold")
	if diffFoldLines != 0 || !strings.Contains(notices(m), i18n.M.DiffFoldDisabled) {
		t.Fatalf("limit = %d, notices %q", diffFoldLines, notices(m))
	}
	sendLine(m, "/diff-fold")
	if diffFoldLines != diffPreviewLines {
		t.Fatalf("limit = %d, want it folded again", diffFoldLines)
	}
	if submitted(k) != "" {
		t.Fatal("a local command reached the kernel")
	}
}

func TestVerboseKeepsThinkingOpenAndIsRemembered(t *testing.T) {
	m, _, cfgPath := miscModel(t)
	row := Item{Kind: ItemSay, Text: "the answer", Reasoning: "because of the cache", Done: true}
	if got := m.settledRow(row, 0).render(80, false); strings.Contains(got, "because of the cache") {
		t.Fatal("thinking was open before /verbose")
	}
	sendLine(m, "/verbose")
	if !m.verbose {
		t.Fatal("verbose did not turn on")
	}
	if got := m.settledRow(row, 0).render(80, false); !strings.Contains(got, "because of the cache") {
		t.Fatalf("thinking stayed collapsed under /verbose:\n%s", got)
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "show_reasoning = true") {
		t.Fatalf("preference not stored:\n%s", raw)
	}
	if !storedVerbose() {
		t.Fatal("a new screen would not start verbose")
	}
	sendLine(m, "/verbose")
	if m.verbose || storedVerbose() {
		t.Fatal("verbose did not turn off")
	}
}

func TestThemeListsSwitchesAndPersists(t *testing.T) {
	m, k, cfgPath := miscModel(t)
	prev := termrender.ActiveTheme()
	t.Cleanup(func() { termrender.SetThemeStyle(prev.Style) })
	sendLine(m, "/theme")
	if got := notices(m); !strings.Contains(got, "graphite") || !strings.Contains(got, i18n.M.ThemeHint) {
		t.Fatalf("listing = %q", got)
	}
	sendLine(m, "/theme glacier")
	if termrender.ActiveTheme().Style != "glacier" || termrender.ThemeName() != "light" {
		t.Fatalf("active = %s/%s", termrender.ThemeName(), termrender.ActiveTheme().Style)
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "glacier") {
		t.Fatalf("theme not stored:\n%s", raw)
	}
	sendLine(m, "/theme nope")
	if !strings.Contains(notices(m), "nope") || termrender.ActiveTheme().Style != "glacier" {
		t.Fatalf("unknown theme handling: %q", notices(m))
	}
	if submitted(k) != "" {
		t.Fatal("a local command reached the kernel")
	}
}

func TestClsRedrawsWithoutTouchingTheConversation(t *testing.T) {
	m, k, _ := miscModel(t)
	m.tr.AddUser("hello")
	m.commit()
	m.todos = []TodoItem{{Content: "fix", Status: "in_progress"}}
	sendLine(m, "/cls")
	for _, it := range m.tr.Items {
		if it.Kind == ItemUser {
			t.Fatal("the transcript the screen holds kept the old turn")
		}
	}
	if len(m.todos) != 1 {
		t.Fatal("/cls dropped the kernel's task list")
	}
	if submitted(k) != "" {
		t.Fatal("/cls reached the kernel")
	}
}

func TestLocalCommandsAreOfferedByCompletionAndHelp(t *testing.T) {
	m, _, _ := miscModel(t)
	var labels []string
	for _, c := range m.localCommands("/") {
		labels = append(labels, c.Label)
	}
	for _, want := range []string{"/cls", "/todo", "/verbose", "/diff-fold", "/theme"} {
		found := false
		for _, l := range labels {
			found = found || l == want
		}
		if !found {
			t.Errorf("%s not offered: %v", want, labels)
		}
	}
}

func TestThemeAndVerboseNeverWriteIntoAProjectFile(t *testing.T) {
	m, _, userPath := miscModel(t)
	t.Cleanup(func() { termrender.SetThemeStyle("graphite") })
	dir := t.TempDir()
	project := filepath.Join(dir, "reasonix.toml")
	const body = "# shared\n[agent]\nmax_steps = 7\n"
	if err := os.WriteFile(project, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	sendLine(m, "/theme glacier")
	sendLine(m, "/verbose")
	if raw, _ := os.ReadFile(project); string(raw) != body {
		t.Fatalf("project file was rewritten:\n%s", raw)
	}
	raw, _ := os.ReadFile(userPath)
	if !strings.Contains(string(raw), "glacier") || !strings.Contains(string(raw), "show_reasoning = true") {
		t.Fatalf("user config missing the preferences:\n%s", raw)
	}
}
