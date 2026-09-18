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
	"reasonix/internal/config"
	"reasonix/internal/event"
)

// TestResumeDispatchOpensPicker proves bare "/resume" opens the interactive
// picker without duplicating the same list in transcript scrollback.
func TestResumeDispatchOpensPicker(t *testing.T) {
	isolateCLIConfigHome(t)
	dir := t.TempDir()
	seedNativeTestSession(t, dir, "alpha prompt")
	seedNativeTestSession(t, dir, "beta prompt")

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	m := newTestChatTUI()
	m.width = 80
	m.ctrl = newExclusiveTestController(t, dir, exec)

	if cmd := m.runSlashCommand("/resume"); cmd != nil {
		t.Fatal("/resume should not return a tea.Cmd")
	}
	if m.resumePick == nil {
		t.Fatal("bare /resume should open the picker")
	}
	if len(m.resumePick.entries) != 2 {
		t.Fatalf("picker should have 2 sessions, got %d", len(m.resumePick.entries))
	}
	out := strings.Join(m.transcript, "\n")
	if strings.Contains(out, "alpha prompt") || strings.Contains(out, "beta prompt") {
		t.Fatalf("picker previews should not be duplicated in scrollback:\n%s", out)
	}
}

func TestOrderResumeSessionsGroupsRecoveryCopiesAndPrefersNewestLeaf(t *testing.T) {
	base := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	root := agent.SessionInfo{Path: "/sessions/root.jsonl", ModTime: base.Add(4 * time.Minute)}
	olderLeaf := agent.SessionInfo{
		Path: "/sessions/recovery-old.jsonl", ModTime: base.Add(2 * time.Minute),
		Recovered: true, ParentID: "root",
	}
	newerLeaf := agent.SessionInfo{
		Path: "/sessions/recovery-new.jsonl", ModTime: base.Add(3 * time.Minute),
		Recovered: true, ParentID: "root",
	}
	other := agent.SessionInfo{Path: "/sessions/other.jsonl", ModTime: base.Add(time.Minute)}

	got := orderResumeSessions([]agent.SessionInfo{root, newerLeaf, olderLeaf, other})
	want := []string{newerLeaf.Path, olderLeaf.Path, root.Path, other.Path}
	if len(got) != len(want) {
		t.Fatalf("ordered sessions len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Path != want[i] {
			t.Fatalf("ordered[%d] = %q, want %q (all=%v)", i, got[i].Path, want[i], got)
		}
	}
}

func TestCapResumeSessionGroupsDoesNotSplitRecoveryFamily(t *testing.T) {
	base := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	sessions := make([]agent.SessionInfo, 0, 12)
	for i := range 9 {
		sessions = append(sessions, agent.SessionInfo{
			Path:    filepath.Join("/sessions", "standalone-"+strconv.Itoa(i)+".jsonl"),
			ModTime: base.Add(time.Duration(20-i) * time.Minute),
		})
	}
	sessions = append(sessions,
		agent.SessionInfo{Path: "/sessions/root.jsonl", ModTime: base.Add(3 * time.Minute)},
		agent.SessionInfo{Path: "/sessions/recovery-a.jsonl", ModTime: base.Add(2 * time.Minute), Recovered: true, ParentID: "root"},
		agent.SessionInfo{Path: "/sessions/recovery-b.jsonl", ModTime: base.Add(time.Minute), Recovered: true, ParentID: "root"},
	)

	got := capResumeSessionGroups(orderResumeSessions(sessions), resumeListCap)
	if len(got) != 9 {
		t.Fatalf("capped sessions len = %d, want 9 complete standalone groups", len(got))
	}
	for _, session := range got {
		if session.Recovered || agent.BranchID(session.Path) == "root" {
			t.Fatalf("cap split recovery family instead of omitting it: %+v", got)
		}
	}
}

func TestSessionPickerLabelIdentifiesRecoveryParent(t *testing.T) {
	session := agent.SessionInfo{
		Path: "/sessions/recovery.jsonl", Preview: "keep working", Turns: 3,
		Recovered: true, ParentID: "20260803-long-parent-id",
	}
	label := sessionPickerLabel(session)
	if recoverySessionBadge(session) == "" || !strings.Contains(label, "20260803") {
		t.Fatalf("recovery picker label = %q, want recovery badge and short parent id", label)
	}
}

// TestResumePickerNavigateAndSelect proves the picker's Enter resumes the
// selected native session.
func TestResumePickerNavigateAndSelect(t *testing.T) {
	isolateCLIConfigHome(t)
	dir := t.TempDir()
	targetID := seedNativeTestSession(t, dir, "SECOND-SESSION-PROMPT")
	seedNativeTestSession(t, dir, "first session prompt")

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := newExclusiveTestController(t, dir, exec)

	m := newTestChatTUI()
	m.width = 80
	m.ctrl = ctrl

	m.runSlashCommand("/resume")
	if m.resumePick == nil {
		t.Fatal("bare /resume should open the picker")
	}
	if len(m.resumePick.entries) != 2 {
		t.Fatalf("picker should have 2 sessions, got %d", len(m.resumePick.entries))
	}
	sel := -1
	for i, e := range m.resumePick.entries {
		if id, ok := v4ResumeID(e.session.Path); ok && id == targetID {
			sel = i
		}
	}
	if sel < 0 {
		t.Fatalf("target row missing from picker: %+v", m.resumePick.entries)
	}
	m.resumePick.sel = sel
	if m.resumePick.quick != nil {
		m.resumePick.quick.selected = sel
	}

	next, _ := m.handleResumePickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(chatTUI)

	ref, ok := ctrl.SessionRef()
	if !ok || ref.SessionID != targetID {
		t.Fatalf("resumed ref = %+v ok=%v, want session %q", ref, ok, targetID)
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
	isolateCLIConfigHome(t)
	dir := t.TempDir()
	seedNativeTestSession(t, dir, "alpha prompt")

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	m := newTestChatTUI()
	m.ctrl = newExclusiveTestController(t, dir, exec)

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
	isolateCLIConfigHome(t)
	dir := t.TempDir()
	targetID := seedNativeTestSession(t, dir, "OTHER-SESSION-PROMPT")
	seedNativeTestSession(t, dir, "first session prompt")

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := newExclusiveTestController(t, dir, exec)

	m := newTestChatTUI()
	m.width = 80
	m.ctrl = ctrl

	target := 0
	for i, s := range recentSessions(dir) {
		if id, ok := v4ResumeID(s.Path); ok && id == targetID {
			target = i + 1
		}
	}
	if target == 0 {
		t.Fatal("other session not listed by recentSessions")
	}

	m.runSlashCommand("/resume " + strconv.Itoa(target))

	ref, ok := ctrl.SessionRef()
	if !ok || ref.SessionID != targetID {
		t.Fatalf("resumed ref = %+v ok=%v, want session %q", ref, ok, targetID)
	}
	if out := strings.Join(m.transcript, "\n"); !strings.Contains(out, "OTHER-SESSION-PROMPT") {
		t.Fatalf("transcript should replay the resumed session:\n%s", out)
	}
}

// TestResumeWhileScrolledUpPinsViewportToBottom covers the session-switch
// regression where a stale scroll offset was preserved if the user had read
// back in the old transcript before resuming another session.
func TestResumeWhileScrolledUpPinsViewportToBottom(t *testing.T) {
	isolateCLIConfigHome(t)
	dir := t.TempDir()
	activeID := nextNativeTestID()
	prompts := make([]string, 18)
	for i := range prompts {
		prompts[i] = "active prompt " + strconv.Itoa(i)
	}
	seedNativeSessionMessages(t, dir, activeID, "", "", prompts...)
	targetID := seedNativeTestSession(t, dir, "OTHER-SESSION-PROMPT")

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := newExclusiveTestController(t, dir, exec)
	if err := resumeWithPersistedSelection(ctrl, v4ResumeLocator(activeID)); err != nil {
		t.Fatal(err)
	}

	target := 0
	for i, s := range recentSessions(dir) {
		if id, ok := v4ResumeID(s.Path); ok && id == targetID {
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

	ref, ok := ctrl.SessionRef()
	if !ok || ref.SessionID != targetID {
		t.Fatalf("resumed ref = %+v ok=%v, want session %q", ref, ok, targetID)
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

// TestResumeArgCompletionListsSessions proves "/resume " opens an indexed menu
// of the saved sessions, mirroring the /switch branch completion.
func TestResumeArgCompletionListsSessions(t *testing.T) {
	isolateCLIConfigHome(t)
	dir := t.TempDir()
	seedNativeTestSession(t, dir, "first")
	seedNativeTestSession(t, dir, "second")

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	m := newTestChatTUI()
	m.ctrl = newExclusiveTestController(t, dir, exec)

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
	isolateCLIConfigHome(t)
	dir := t.TempDir()
	seedNativeTestSession(t, dir, "first")

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	m := newTestChatTUI()
	m.ctrl = newExclusiveTestController(t, dir, exec)

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
	isolateCLIConfigHome(t)
	dir := t.TempDir()
	targetID := seedNativeTestSession(t, dir, "other prompt")
	seedNativeTestSession(t, dir, "first prompt")

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := newExclusiveTestController(t, dir, exec)

	m := newTestChatTUI()
	m.width = 80
	m.ctrl = ctrl

	target := 0
	for i, s := range recentSessions(dir) {
		if id, ok := v4ResumeID(s.Path); ok && id == targetID {
			target = i + 1
		}
	}
	if target == 0 {
		t.Fatal("saved session not listed by recentSessions")
	}

	m.runResumeCommand("/resume " + strconv.Itoa(target))

	ref, ok := ctrl.SessionRef()
	if !ok || ref.SessionID != targetID {
		t.Fatalf("resumed ref = %+v ok=%v, want session %q", ref, ok, targetID)
	}
	hist := ctrl.History()
	if len(hist) == 0 || hist[len(hist)-1].Content != "other prompt" {
		t.Fatalf("history not loaded from target: %+v", hist)
	}
}

// TestResumeEntriesIncludeOtherProjects proves the picker surfaces the newest
// session of other known projects (#9477): a user who worked here over SSH
// resumes from any directory, not only the original workspace root.
func TestResumeEntriesIncludeOtherProjects(t *testing.T) {
	isolateCLIConfigHome(t)
	currentDir := t.TempDir()
	currentID := seedNativeTestSession(t, currentDir, "current project work")

	otherRoot := t.TempDir()
	otherDir := config.ProjectSessionDir(otherRoot)
	if otherDir == "" {
		t.Skip("project session dir unavailable")
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.ReasonixHomeDir(), "desktop-projects.json"),
		[]byte(`{"projects":[{"root":`+strconv.Quote(filepath.ToSlash(otherRoot))+`}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	otherID := seedNativeTestSession(t, otherDir, "other project work")

	entries := resumeEntries(currentDir)
	if len(entries) != 2 {
		t.Fatalf("resumeEntries = %d entries, want current + other project", len(entries))
	}
	if entries[0].project != "" || entries[0].session.Path != v4ResumeLocator(currentID) {
		t.Fatalf("first entry = %+v, want the current directory session", entries[0])
	}
	if entries[1].project == "" {
		t.Fatalf("second entry = %+v, want a project label for the other project", entries[1])
	}
	if entries[1].session.Path != v4ResumeLocator(otherID) {
		t.Fatalf("second entry path = %q, want %q", entries[1].session.Path, v4ResumeLocator(otherID))
	}
}
