package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/event"
)

func TestTerminalTitleIncludesRunningActivity(t *testing.T) {
	m := newTestChatTUI()
	m.terminalTitleItems = []string{config.TerminalTitleActivity}
	m.state = tuiRunning
	m.runStart = time.Now().Add(-343 * time.Second)
	m.elapsed = 343
	m.turnTokens = 9100

	m.syncWindowTitle()

	if !strings.Contains(m.windowTitle, "343") || !strings.Contains(m.windowTitle, "↓9.1K") {
		t.Fatalf("windowTitle = %q, want elapsed seconds and output tokens", m.windowTitle)
	}
}

func TestTerminalTitleTodoProgressKeepsCompletedList(t *testing.T) {
	m := newTestChatTUI()
	m.terminalTitleItems = []string{config.TerminalTitleTodoProgress}
	m.todoArgs = `{"todos":[` +
		`{"content":"one","status":"completed"},` +
		`{"content":"two","status":"completed"}]}`

	if got := m.renderTerminalTitle(); got != "Tasks 2/2" {
		t.Fatalf("renderTerminalTitle = %q, want Tasks 2/2", got)
	}
}

func TestTerminalTitleActivityPrefersActionRequired(t *testing.T) {
	m := newTestChatTUI()
	m.terminalTitleItems = []string{config.TerminalTitleActivity}
	m.state = tuiRunning
	m.elapsed = 42
	m.turnTokens = 1200
	m.pendingApproval = &event.Approval{Tool: "bash"}

	if got := m.renderTerminalTitle(); got != "Action required" {
		t.Fatalf("renderTerminalTitle = %q, want Action required", got)
	}
}

func TestTitleCommandPersistsUserConfigNotProjectConfig(t *testing.T) {
	isolateUserConfig(t)
	projectPath := filepath.Join(mustGetwd(t), "reasonix.toml")
	projectBody := "[terminal_title]\nitems = [\"activity\"]\n"
	if err := os.WriteFile(projectPath, []byte(projectBody), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	m := newTestChatTUI()
	m.terminalTitleItems = []string{config.TerminalTitleActivity}
	m.runSlashCommand("/title")
	if m.titlePick == nil {
		t.Fatal("/title should open the terminal title picker")
	}
	m.titlePick.enabled = map[string]bool{config.TerminalTitleAppName: true}

	next, _ := m.saveTitlePick()
	updated := next.(chatTUI)

	if got := updated.windowTitle; got != "Reasonix" {
		t.Fatalf("windowTitle after save = %q, want Reasonix", got)
	}
	userBody, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read user config: %v", err)
	}
	if !strings.Contains(string(userBody), `[terminal_title]`) || !strings.Contains(string(userBody), `items = ["app-name"]`) {
		t.Fatalf("user config missing terminal title app-name:\n%s", userBody)
	}
	gotProject, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if string(gotProject) != projectBody {
		t.Fatalf("/title should not rewrite project config:\n%s", gotProject)
	}
}
