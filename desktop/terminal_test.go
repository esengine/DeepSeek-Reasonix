package main

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/secrets"
)

func TestTerminalStartReusesTabSessionAndUsesWorkspace(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspace := t.TempDir()
	app := NewApp()
	app.ctx = context.Background()
	app.tabs["tab-terminal"] = &WorkspaceTab{
		ID:            "tab-terminal",
		Scope:         "project",
		WorkspaceRoot: workspace,
		disabledMCP:   map[string]ServerView{},
	}
	app.tabOrder = []string{"tab-terminal"}
	app.activeTabID = "tab-terminal"

	var outputMu sync.Mutex
	var output strings.Builder
	outputReady := make(chan struct{}, 1)
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...interface{}) {
		if name != terminalDataEvent || len(payload) == 0 {
			return
		}
		event, ok := payload[0].(terminalDataPayload)
		if !ok || event.TabID != "tab-terminal" {
			return
		}
		raw, err := base64.StdEncoding.DecodeString(event.Data)
		if err != nil {
			return
		}
		outputMu.Lock()
		output.Write(raw)
		matched := strings.Contains(output.String(), "REASONIX_TERMINAL_TEST")
		outputMu.Unlock()
		if matched {
			select {
			case outputReady <- struct{}{}:
			default:
			}
		}
	}

	view, err := app.TerminalStart("tab-terminal", 100, 30)
	if err != nil {
		t.Fatalf("TerminalStart: %v", err)
	}
	t.Cleanup(func() { _ = app.TerminalClose("tab-terminal", view.SessionID) })
	if view.CWD != workspace {
		t.Fatalf("terminal cwd = %q, want %q", view.CWD, workspace)
	}
	if view.SessionID == "" {
		t.Fatal("terminal session id is empty")
	}

	reused, err := app.TerminalStart("tab-terminal", 120, 40)
	if err != nil {
		t.Fatalf("second TerminalStart: %v", err)
	}
	if reused.SessionID != view.SessionID {
		t.Fatalf("second start created %q, want reused %q", reused.SessionID, view.SessionID)
	}

	input := base64.StdEncoding.EncodeToString([]byte("echo REASONIX_TERMINAL_TEST\r"))
	if err := app.TerminalWrite("tab-terminal", view.SessionID, input); err != nil {
		t.Fatalf("TerminalWrite: %v", err)
	}
	select {
	case <-outputReady:
	case <-time.After(10 * time.Second):
		outputMu.Lock()
		got := output.String()
		outputMu.Unlock()
		t.Fatalf("timed out waiting for terminal output; got %q", got)
	}
}

func TestTerminalEnvironmentHonorsSubprocessSecretFilter(t *testing.T) {
	previous := secrets.FilterSubprocessEnv()
	secrets.SetFilterSubprocessEnv(true)
	t.Cleanup(func() { secrets.SetFilterSubprocessEnv(previous) })
	t.Setenv("REASONIX_TEST_API_KEY", "must-not-leak")

	env := terminalEnvironment()
	for _, item := range env {
		if strings.HasPrefix(item, "REASONIX_TEST_API_KEY=") {
			t.Fatal("terminal environment leaked a filtered credential variable")
		}
	}
	if !containsEnvAssignment(env, "TERM", "xterm-256color") {
		t.Fatal("terminal environment is missing TERM=xterm-256color")
	}
}

func containsEnvAssignment(env []string, key, value string) bool {
	want := key + "=" + value
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}

func TestTerminalHistoryIsBoundedAndSequenced(t *testing.T) {
	session := &terminalSession{}
	first := session.appendHistory([]byte("first"))
	second := session.appendHistory(make([]byte, terminalHistoryMax+128))
	if first != 1 || second != 2 {
		t.Fatalf("terminal sequences = %d, %d; want 1, 2", first, second)
	}
	if len(session.history) != terminalHistoryMax {
		t.Fatalf("terminal history len = %d, want %d", len(session.history), terminalHistoryMax)
	}
}
