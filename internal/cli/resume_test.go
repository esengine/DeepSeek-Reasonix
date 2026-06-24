package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TestResumeDispatchOpensPicker proves bare "/resume" writes the session list
// to the scrollback (above the input) AND opens the interactive picker overlay.
func TestResumeDispatchOpensPicker(t *testing.T) {
	dir := t.TempDir()
	saveTestSession(t, filepath.Join(dir, "a.jsonl"), "alpha prompt")
	saveTestSession(t, filepath.Join(dir, "b.jsonl"), "beta prompt")

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	m := newTestChatTUI()
	m.width = 80
	m.ctrl = control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})

	if cmd := m.runSlashCommand("/resume"); cmd != nil {
		t.Fatal("/resume should not return a tea.Cmd")
	}
	if m.resumePick == nil {
		t.Fatal("bare /resume should open the picker")
	}
	if len(m.resumePick.sessions) != 2 {
		t.Fatalf("picker should have 2 sessions, got %d", len(m.resumePick.sessions))
	}
	// Session list must also appear in the scrollback transcript (above input).
	out := strings.Join(m.transcript, "\n")
	if !strings.Contains(out, "alpha prompt") || !strings.Contains(out, "beta prompt") {
		t.Fatalf("scrollback should contain session previews:\n%s", out)
	}
}

// TestResumePickerNavigateAndSelect proves the picker's up/down navigation and
// Enter to resume the selected session.
func TestResumePickerNavigateAndSelect(t *testing.T) {
	dir := t.TempDir()
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})

	// Create two saved sessions.
	aPath := filepath.Join(dir, "a.jsonl")
	saveTestSession(t, aPath, "first session prompt")
	bPath := filepath.Join(dir, "b.jsonl")
	saveTestSession(t, bPath, "SECOND-SESSION-PROMPT")
	// Pin distinct mtimes so b is unambiguously the most recent. Created back to
	// back, the two files can land in the same filesystem mtime tick (seen on the
	// CI Windows runner), which then tie-breaks to a.jsonl by path and flakes.
	now := time.Now()
	if err := os.Chtimes(aPath, now.Add(-2*time.Second), now.Add(-2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(bPath, now, now); err != nil {
		t.Fatal(err)
	}

	m := newTestChatTUI()
	m.width = 80
	m.ctrl = ctrl

	// Open the picker via bare /resume.
	m.runSlashCommand("/resume")
	if m.resumePick == nil {
		t.Fatal("bare /resume should open the picker")
	}
	if len(m.resumePick.sessions) != 2 {
		t.Fatalf("picker should have 2 sessions, got %d", len(m.resumePick.sessions))
	}

	// The first session (default selection) is the most recent, which is b.jsonl.
	// Press Enter to resume it.
	next, _ := m.handleResumePickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(chatTUI)

	if got := ctrl.SessionPath(); got != bPath {
		t.Fatalf("session path = %q, want %q", got, bPath)
	}
	if out := strings.Join(m.transcript, "\n"); !strings.Contains(out, "SECOND-SESSION-PROMPT") {
		t.Fatalf("transcript should replay the resumed session:\n%s", out)
	}
	if m.resumePick != nil {
		t.Fatal("picker should close after resume")
	}
}

// TestResumePickerEscDismisses proves pressing Esc closes the picker without
// switching sessions.
func TestResumePickerEscDismisses(t *testing.T) {
	dir := t.TempDir()
	saveTestSession(t, filepath.Join(dir, "a.jsonl"), "alpha prompt")

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})

	m.runSlashCommand("/resume")
	if m.resumePick == nil {
		t.Fatal("bare /resume should open the picker")
	}

	next, _ := m.handleResumePickerKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(chatTUI)
	if m.resumePick != nil {
		t.Fatal("picker should close on Esc")
	}
}

// TestResumeDispatchSwitchesAndReplays drives "/resume <n>" through the slash
// dispatcher and asserts the controller switched session AND the resumed
// transcript was replayed into the scrollback.
func TestResumeDispatchSwitchesAndReplays(t *testing.T) {
	dir := t.TempDir()
	active := agent.NewSession("sys")
	active.Add(provider.Message{Role: provider.RoleUser, Content: "active prompt"})
	exec := agent.New(nil, nil, active, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})
	ctrl.SetSessionPath(filepath.Join(dir, "active.jsonl"))
	if err := ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}

	otherPath := filepath.Join(dir, "other.jsonl")
	saveTestSession(t, otherPath, "OTHER-SESSION-PROMPT")

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
		t.Fatal("other session not listed by recentSessions")
	}

	m.runSlashCommand("/resume " + strconv.Itoa(target))

	if got := ctrl.SessionPath(); got != otherPath {
		t.Fatalf("session path = %q, want %q", got, otherPath)
	}
	if out := strings.Join(m.transcript, "\n"); !strings.Contains(out, "OTHER-SESSION-PROMPT") {
		t.Fatalf("transcript should replay the resumed session:\n%s", out)
	}
}

// TestResumeWhileScrolledUpPinsViewportToBottom covers the session-switch
// regression where a stale scroll offset was preserved if the user had read
// back in the old transcript before resuming another session.
func TestResumeWhileScrolledUpPinsViewportToBottom(t *testing.T) {
	dir := t.TempDir()
	active := agent.NewSession("sys")
	for i := 0; i < 18; i++ {
		active.Add(provider.Message{Role: provider.RoleUser, Content: "active prompt " + strconv.Itoa(i)})
	}
	exec := agent.New(nil, nil, active, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})
	activePath := filepath.Join(dir, "active.jsonl")
	ctrl.SetSessionPath(activePath)
	if err := ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}

	otherPath := filepath.Join(dir, "other.jsonl")
	saveTestSession(t, otherPath, "OTHER-SESSION-PROMPT")

	target := 0
	for i, s := range recentSessions(dir) {
		if s.Path == otherPath {
			target = i + 1
		}
	}
	if target == 0 {
		t.Fatal("other session not listed by recentSessions")
	}

	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}

	cur := adv(newChatTUI(ctrl, "", make(chan event.Event, 1), 80), tea.WindowSizeMsg{Width: 80, Height: 8})
	if !cur.viewport.AtBottom() {
		t.Fatal("initial resumed history should start at the bottom")
	}

	cur = adv(cur, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if cur.viewport.AtBottom() {
		t.Fatal("wheel-up should move the old transcript away from the bottom")
	}

	cur.input.SetValue("/resume " + strconv.Itoa(target))
	cur = adv(cur, tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := ctrl.SessionPath(); got != otherPath {
		t.Fatalf("session path = %q, want %q", got, otherPath)
	}
	out := strings.Join(cur.transcript, "\n")
	if !strings.Contains(out, "OTHER-SESSION-PROMPT") {
		t.Fatalf("transcript should replay the resumed session:\n%s", out)
	}
	if strings.Contains(out, "active prompt") {
		t.Fatalf("transcript should not retain the previous session after resume:\n%s", out)
	}
	if !cur.viewport.AtBottom() {
		t.Fatalf("resume while scrolled up should pin to bottom, AtBottom=%v, YOffset=%d", cur.viewport.AtBottom(), cur.viewport.YOffset())
	}
}

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

// TestResumeAcceptChainsIntoSessionMenu proves accepting "/resume" (a
// non-descend command that still takes arguments) immediately opens the session
// menu, rather than waiting for the next keystroke.
func TestResumeAcceptChainsIntoSessionMenu(t *testing.T) {
	dir := t.TempDir()
	saveTestSession(t, filepath.Join(dir, "a.jsonl"), "first")

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})

	m.input.SetValue("/resu")
	m.updateCompletion()
	m.acceptCompletion()
	if got := m.input.Value(); got != "/resume " {
		t.Fatalf("accepting /resume should fill %q, got %q", "/resume ", got)
	}
	if !m.completion.active || m.completion.kind != compSlashArg {
		t.Fatalf("accepting /resume should chain into the session menu: %+v", m.completion)
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
