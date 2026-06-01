package cli

import (
	"path/filepath"
	"strconv"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func saveTestSession(t *testing.T, path, prompt string) {
	t.Helper()
	s := agent.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: prompt})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
}

// TestResumeArgCompletionListsSessions proves "/resume " opens an indexed menu
// of the saved sessions, mirroring the /switch branch completion.
func TestResumeArgCompletionListsSessions(t *testing.T) {
	dir := t.TempDir()
	saveTestSession(t, filepath.Join(dir, "a.jsonl"), "first")
	saveTestSession(t, filepath.Join(dir, "b.jsonl"), "second")

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})

	m.input.SetValue("/resume ")
	m.updateCompletion()
	if !m.completion.active || m.completion.kind != compSlashArg {
		t.Fatalf("/resume should open argument completion: %+v", m.completion)
	}
	if got := labels(m.completion.items); len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Fatalf("resume completion = %v, want [1 2]", got)
	}
}

// TestRunResumeSwitchesSession proves "/resume <n>" repoints the running
// controller to the chosen saved session and loads its history.
func TestRunResumeSwitchesSession(t *testing.T) {
	dir := t.TempDir()

	active := agent.NewSession("sys")
	active.Add(provider.Message{Role: provider.RoleUser, Content: "active prompt"})
	exec := agent.New(nil, nil, active, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})
	activePath := filepath.Join(dir, "active.jsonl")
	ctrl.SetSessionPath(activePath)
	if err := ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}

	otherPath := filepath.Join(dir, "other.jsonl")
	saveTestSession(t, otherPath, "other prompt")

	m := newTestChatTUI()
	m.width = 80
	m.ctrl = ctrl

	target := 0
	for i, s := range recentSessions(dir) {
		if s.Path == otherPath {
			target = i + 1
		}
	}
	if target == 0 {
		t.Fatal("saved session not listed by recentSessions")
	}

	m.runResumeCommand("/resume " + strconv.Itoa(target))

	if got := ctrl.SessionPath(); got != otherPath {
		t.Fatalf("session path = %q, want %q", got, otherPath)
	}
	hist := ctrl.History()
	if len(hist) == 0 || hist[len(hist)-1].Content != "other prompt" {
		t.Fatalf("history not loaded from target: %+v", hist)
	}
}
