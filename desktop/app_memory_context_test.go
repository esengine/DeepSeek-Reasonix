package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/memory"
)

func TestSubmitToTabHistoryDisplaysRawInputAfterStandingMemoryCompose(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "memory-display.jsonl")
	mem := memory.Load(memory.Options{CWD: t.TempDir()})
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	runner := &appendingDesktopRunner{session: sess, started: make(chan string, 1)}
	ctrl := control.New(control.Options{
		Runner: runner, Executor: exec, Sink: event.Discard, SessionDir: dir,
		SessionPath: path, Label: "test", Memory: mem,
	})
	defer ctrl.Close()

	app := NewApp()
	app.setTestCtrl(ctrl, "deepseek/test")
	if _, err := ctrl.QuickAdd(memory.ScopeProject, "contribution count updated"); err != nil {
		t.Fatal(err)
	}

	const prompt = "不要，删了"
	app.SubmitToTab("test", prompt)
	composed := <-runner.started
	waitNotRunning(t, ctrl)
	if !strings.Contains(composed, "<memory-update>") || !strings.HasSuffix(composed, prompt) {
		t.Fatalf("model input should include memory update followed by prompt, got %q", composed)
	}
	got := app.HistoryForTab("test")
	if len(got) < 2 || got[0].Role != "system" || got[1].Role != "user" {
		t.Fatalf("history roles = %+v, want system then user", got[:min(len(got), 2)])
	}
	if got[1].Content != prompt || strings.Contains(got[1].Content, "<memory-update>") {
		t.Fatalf("displayed user content = %q, want raw prompt %q", got[1].Content, prompt)
	}
}
