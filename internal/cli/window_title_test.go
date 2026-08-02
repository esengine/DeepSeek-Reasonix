package cli

import (
	"path/filepath"
	"testing"

	"charm.land/bubbles/v2/spinner"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

func windowTitleFixture(t *testing.T, path string) chatTUI {
	t.Helper()
	dir := filepath.Dir(path)
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	m := newChatTUI(control.New(control.Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"}), "", make(chan event.Event, 1), 80)
	return m
}

func TestWindowTitleFreshSession(t *testing.T) {
	// No session at all: the title is the product name.
	m := windowTitleFixture(t, "")
	if got := m.windowTitle(); got != "Reasonix" {
		t.Fatalf("no-session window title = %q, want Reasonix", got)
	}
}

func TestWindowTitleSessionTitles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	saveTestSession(t, path, "hello")

	// Session file with no meta yet: still a fresh conversation.
	m := windowTitleFixture(t, path)
	if got := m.windowTitle(); got != "Reasonix" {
		t.Fatalf("untitled window title = %q, want Reasonix", got)
	}

	// The first user prompt (meta preview) names the conversation when no
	// explicit title exists — this is what `-c`/`-r` into a plain session shows.
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Preview:       "hello",
		SchemaVersion: agent.BranchMetaCountsVersion,
	}); err != nil {
		t.Fatal(err)
	}
	if got := m.windowTitle(); got != "hello" {
		t.Fatalf("preview window title = %q, want hello", got)
	}

	// Topic title wins over the raw preview.
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		TopicTitle:    "fix the parser",
		Preview:       "hello",
		SchemaVersion: agent.BranchMetaCountsVersion,
	}); err != nil {
		t.Fatal(err)
	}
	m.invalidateTitleCache()
	if got := m.windowTitle(); got != "fix the parser" {
		t.Fatalf("topic window title = %q, want fix the parser", got)
	}

	// The memoized title is reused without re-reading the sidecar: renaming the
	// file directly does not change the cached title until invalidation.
	if err := agent.RenameSession(path, "My Session"); err != nil {
		t.Fatal(err)
	}
	if got := m.windowTitle(); got != "fix the parser" {
		t.Fatalf("stale cached window title = %q, want fix the parser", got)
	}
	m.invalidateTitleCache()
	if got := m.windowTitle(); got != "My Session" {
		t.Fatalf("renamed window title = %q, want My Session", got)
	}
}

func TestWindowTitleRunningPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	saveTestSession(t, path, "hello")
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		CustomTitle:   "My Session",
		SchemaVersion: agent.BranchMetaCountsVersion,
	}); err != nil {
		t.Fatal(err)
	}

	m := windowTitleFixture(t, path)
	m.state = tuiRunning
	want := spinner.Dot.Frames[0] + " My Session"
	if got := m.windowTitle(); got != want {
		t.Fatalf("running window title = %q, want %q", got, want)
	}

	// The spinner frame advances with the tick that drives the in-TUI spinner.
	m.titleSpin = 3
	want = spinner.Dot.Frames[3] + " My Session"
	if got := m.windowTitle(); got != want {
		t.Fatalf("running window title frame 3 = %q, want %q", got, want)
	}

	m0, _ := m.Update(spinner.TickMsg{})
	m = m0.(chatTUI)
	want = spinner.Dot.Frames[4] + " My Session"
	if got := m.windowTitle(); got != want {
		t.Fatalf("running window title after tick = %q, want %q", got, want)
	}

	// Idle again: the spinner prefix drops.
	m.state = tuiIdle
	if got := m.windowTitle(); got != "My Session" {
		t.Fatalf("idle window title = %q, want My Session", got)
	}
}

func TestRenameCommandInvalidatesWindowTitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	saveTestSession(t, path, "hello")
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		CustomTitle:   "Old Name",
		SchemaVersion: agent.BranchMetaCountsVersion,
	}); err != nil {
		t.Fatal(err)
	}

	m := windowTitleFixture(t, path)
	if got := m.windowTitle(); got != "Old Name" {
		t.Fatalf("window title = %q, want Old Name", got)
	}

	m.runRenameCommand("/rename New Name")
	if got := m.windowTitle(); got != "New Name" {
		t.Fatalf("window title after /rename = %q, want New Name", got)
	}
}
