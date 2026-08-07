package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestResolveTerminalStartDirUsesFilesystemTypeAndContainsSymlinks(t *testing.T) {
	root := t.TempDir()
	dottedDir := filepath.Join(root, "config.d")
	if err := os.Mkdir(dottedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	license := filepath.Join(root, "LICENSE")
	if err := os.WriteFile(license, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	canonicalDottedDir, err := canonicalDirectory(dottedDir)
	if err != nil {
		t.Fatal(err)
	}

	if got, err := resolveTerminalStartDir(root, "config.d"); err != nil || got != canonicalDottedDir {
		t.Fatalf("dotted directory = %q, %v; want %q", got, err, canonicalDottedDir)
	}
	if got, err := resolveTerminalStartDir(root, "LICENSE"); err != nil || got != canonicalRoot {
		t.Fatalf("extensionless file = %q, %v; want %q", got, err, canonicalRoot)
	}
	if _, err := resolveTerminalStartDir(root, "../outside"); !errors.Is(err, errTerminalOutside) {
		t.Fatalf("parent traversal error = %v, want errTerminalOutside", err)
	}
	if _, err := resolveTerminalStartDir(root, root); !errors.Is(err, errTerminalOutside) {
		t.Fatalf("absolute path error = %v, want errTerminalOutside", err)
	}

	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := resolveTerminalStartDir(root, "outside-link"); !errors.Is(err, errTerminalOutside) {
		t.Fatalf("escaping directory symlink error = %v, want errTerminalOutside", err)
	}
}

func TestResolveTerminalCommandTrustsOnlyUserConfigPath(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	t.Setenv("REASONIX_SAFE_MODE", "")
	root := t.TempDir()
	projectShell := testExecutable(t, root, "project-shell")
	projectConfig := "[tools.shell]\nprefer = \"bash\"\npath = " + strconv.Quote(projectShell) + "\n"
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	command, err := resolveTerminalCommand(root, "default")
	if err != nil {
		t.Fatal(err)
	}
	if command.path == projectShell {
		t.Fatal("project reasonix.toml selected the integrated terminal executable")
	}

	userShell := testExecutable(t, t.TempDir(), "user-shell")
	userConfig := "[tools.shell]\nprefer = \"bash\"\npath = " + strconv.Quote(userShell) + "\n"
	userConfigPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userConfigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfigPath, []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	command, err = resolveTerminalCommand(root, "default")
	if err != nil {
		t.Fatal(err)
	}
	if command.path != userShell {
		t.Fatalf("user-configured shell = %q, want %q", command.path, userShell)
	}
	if _, err := resolveTerminalCommand(root, userShell); err == nil || !strings.Contains(err.Error(), "unsupported terminal shell") {
		t.Fatalf("renderer path override error = %v, want unsupported shell", err)
	}
}

func TestTerminalEnvironmentOverridesInheritedTerminalCapabilities(t *testing.T) {
	env := terminalEnvironment([]string{
		"PATH=/bin",
		"TERM=dumb",
		"colorterm=legacy",
		"REASONIX_TEST=value",
	})
	joined := strings.Join(env, "\n")
	if strings.Count(strings.ToUpper(joined), "TERM=") != 2 {
		t.Fatalf("terminal environment contains duplicate TERM variables: %q", env)
	}
	if !strings.Contains(joined, "TERM=xterm-256color") || !strings.Contains(joined, "COLORTERM=truecolor") {
		t.Fatalf("terminal capability overrides missing: %q", env)
	}
	if !strings.Contains(joined, "PATH=/bin") || !strings.Contains(joined, "REASONIX_TEST=value") {
		t.Fatalf("terminal environment dropped unrelated variables: %q", env)
	}
}

func testExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTerminalTargetRejectsStaleAndReadOnlyTabs(t *testing.T) {
	app := NewApp()
	root := t.TempDir()
	tab := &WorkspaceTab{ID: "active", Scope: "project", WorkspaceRoot: root, ReadOnly: true}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	view, err := app.TerminalWorkspaceForTab(tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.ReadOnly || view.Sessions == nil || view.Shells == nil {
		t.Fatalf("read-only workspace view = %+v, want non-nil arrays", view)
	}
	if _, err := app.CreateTerminalForTab(tab.ID, ".", "default"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("read-only create error = %v", err)
	}
	if _, err := app.TerminalWorkspaceForTab("stale"); !errors.Is(err, errTerminalStaleTab) {
		t.Fatalf("stale tab error = %v, want errTerminalStaleTab", err)
	}
}

func TestTerminalTargetScopesSessionsToTheChatTab(t *testing.T) {
	app := NewApp()
	root := t.TempDir()
	app.tabs["one"] = &WorkspaceTab{ID: "one", Scope: "project", WorkspaceRoot: root}
	app.tabs["two"] = &WorkspaceTab{ID: "two", Scope: "project", WorkspaceRoot: root}
	app.tabOrder = []string{"one", "two"}
	app.activeTabID = "one"

	first, err := app.terminalTargetForTab("one", false)
	if err != nil {
		t.Fatal(err)
	}
	app.activeTabID = "two"
	second, err := app.terminalTargetForTab("two", false)
	if err != nil {
		t.Fatal(err)
	}
	if first.workspaceRoot != second.workspaceRoot {
		t.Fatalf("same project root changed: %q != %q", first.workspaceRoot, second.workspaceRoot)
	}
	if first.workspaceKey == second.workspaceKey {
		t.Fatalf("terminal scope key shared across chat tabs: %q", first.workspaceKey)
	}
}

func TestEmptyTerminalWorkspaceViewSerializesArrays(t *testing.T) {
	view := emptyTerminalWorkspaceView()
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"sessions":[]`) || !strings.Contains(text, `"shells":[]`) {
		t.Fatalf("terminal workspace JSON = %s, want [] arrays", text)
	}
}

type fakeTerminalWait struct {
	code int
	err  error
}

type fakeTerminalProcess struct {
	waitResult chan fakeTerminalWait
	closed     chan struct{}
	closeOnce  sync.Once

	mu      sync.Mutex
	writes  []byte
	resizes [][2]int
}

func newFakeTerminalProcess() *fakeTerminalProcess {
	return &fakeTerminalProcess{
		waitResult: make(chan fakeTerminalWait, 1),
		closed:     make(chan struct{}),
	}
}

func (p *fakeTerminalProcess) Read([]byte) (int, error) {
	<-p.closed
	return 0, io.EOF
}

func (p *fakeTerminalProcess) Write(data []byte) (int, error) {
	p.mu.Lock()
	p.writes = append(p.writes, data...)
	p.mu.Unlock()
	return len(data), nil
}

func (p *fakeTerminalProcess) Resize(cols, rows int) error {
	p.mu.Lock()
	p.resizes = append(p.resizes, [2]int{cols, rows})
	p.mu.Unlock()
	return nil
}

func (p *fakeTerminalProcess) Wait() (int, error) {
	select {
	case result := <-p.waitResult:
		p.closeOnce.Do(func() { close(p.closed) })
		return result.code, result.err
	case <-p.closed:
		return -1, errors.New("closed")
	}
}

func (p *fakeTerminalProcess) Close() error {
	p.closeOnce.Do(func() { close(p.closed) })
	return nil
}

type drainingTerminalProcess struct {
	waitRelease chan struct{}
	readRelease chan struct{}
	closed      chan struct{}
	closeOnce   sync.Once
}

func newDrainingTerminalProcess() *drainingTerminalProcess {
	return &drainingTerminalProcess{
		waitRelease: make(chan struct{}),
		readRelease: make(chan struct{}),
		closed:      make(chan struct{}),
	}
}

func (p *drainingTerminalProcess) Read(data []byte) (int, error) {
	select {
	case <-p.readRelease:
		return copy(data, []byte("final output")), io.EOF
	case <-p.closed:
		return 0, io.EOF
	}
}

func (p *drainingTerminalProcess) Write(data []byte) (int, error) { return len(data), nil }
func (p *drainingTerminalProcess) Resize(int, int) error          { return nil }
func (p *drainingTerminalProcess) Wait() (int, error) {
	<-p.waitRelease
	return 0, nil
}
func (p *drainingTerminalProcess) Close() error {
	p.closeOnce.Do(func() { close(p.closed) })
	return nil
}

func TestTerminalManagerCountsConcurrentStartsTowardLimit(t *testing.T) {
	manager := newTerminalManager(nil)
	entered := make(chan struct{}, maxTerminalsPerWorkspace+1)
	release := make(chan struct{})
	manager.start = func(terminalStartSpec) (terminalProcess, error) {
		entered <- struct{}{}
		<-release
		return newFakeTerminalProcess(), nil
	}

	type result struct{ err error }
	results := make(chan result, maxTerminalsPerWorkspace+1)
	for range maxTerminalsPerWorkspace + 1 {
		go func() {
			_, err := manager.create("tab", "workspace", ".", terminalCommand{path: "shell", label: "shell"})
			results <- result{err: err}
		}()
	}
	for range maxTerminalsPerWorkspace {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("terminal starts did not reach the concurrency barrier")
		}
	}
	close(release)

	succeeded, limited := 0, 0
	for range maxTerminalsPerWorkspace + 1 {
		result := <-results
		switch {
		case result.err == nil:
			succeeded++
		case strings.Contains(result.err.Error(), "session limit"):
			limited++
		default:
			t.Fatalf("unexpected create error: %v", result.err)
		}
	}
	if succeeded != maxTerminalsPerWorkspace || limited != 1 {
		t.Fatalf("create results: succeeded=%d limited=%d", succeeded, limited)
	}
	manager.closeAll()
}

func TestTerminalManagerRejectsStartThatFinishesAfterTabClose(t *testing.T) {
	manager := newTerminalManager(nil)
	proc := newFakeTerminalProcess()
	entered := make(chan struct{})
	release := make(chan struct{})
	manager.start = func(terminalStartSpec) (terminalProcess, error) {
		close(entered)
		<-release
		return proc, nil
	}

	result := make(chan error, 1)
	go func() {
		_, err := manager.create("closing-tab", "workspace", ".", terminalCommand{path: "shell", label: "shell"})
		result <- err
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("terminal start did not reach the concurrency barrier")
	}
	manager.closeForTab("closing-tab")
	close(release)

	select {
	case err := <-result:
		if !errors.Is(err, errTerminalStaleTab) {
			t.Fatalf("create error = %v, want errTerminalStaleTab", err)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal create did not finish after tab close")
	}
	select {
	case <-proc.closed:
	case <-time.After(time.Second):
		t.Fatal("terminal process started for a closed tab was not closed")
	}
	if got := manager.list("workspace"); len(got) != 0 {
		t.Fatalf("closed tab registered terminal sessions: %+v", got)
	}
	if _, err := manager.create("closing-tab", "workspace", ".", terminalCommand{path: "shell", label: "shell"}); !errors.Is(err, errTerminalStaleTab) {
		t.Fatalf("create after tab close error = %v, want errTerminalStaleTab", err)
	}
	manager.closeAll()
}

func TestTerminalManagerRejectsStaleStartAfterTabGateReopens(t *testing.T) {
	manager := newTerminalManager(nil)
	proc := newFakeTerminalProcess()
	entered := make(chan struct{})
	release := make(chan struct{})
	manager.start = func(terminalStartSpec) (terminalProcess, error) {
		close(entered)
		<-release
		return proc, nil
	}

	result := make(chan error, 1)
	go func() {
		_, err := manager.create("rebinding-tab", "old-workspace", ".", terminalCommand{path: "shell", label: "shell"})
		result <- err
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("terminal start did not reach the concurrency barrier")
	}
	manager.closeForTab("rebinding-tab")
	manager.reopenForTab("rebinding-tab")
	close(release)

	select {
	case err := <-result:
		if !errors.Is(err, errTerminalStaleTab) {
			t.Fatalf("create error = %v, want errTerminalStaleTab", err)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal create did not finish after the tab gate reopened")
	}
	select {
	case <-proc.closed:
	case <-time.After(time.Second):
		t.Fatal("stale terminal process was not closed after the tab gate reopened")
	}
	if got := manager.list("old-workspace"); len(got) != 0 {
		t.Fatalf("stale tab registered terminal sessions: %+v", got)
	}
	manager.closeAll()
}

func TestTerminalManagerDrainsFinalOutputBeforePublishingExit(t *testing.T) {
	manager := newTerminalManager(nil)
	proc := newDrainingTerminalProcess()
	manager.start = func(terminalStartSpec) (terminalProcess, error) { return proc, nil }
	view, err := manager.create("tab", "workspace", ".", terminalCommand{path: "shell", label: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	done := manager.sessions[view.ID].done
	manager.mu.Unlock()

	close(proc.waitRelease)
	select {
	case <-done:
		t.Fatal("terminal exit completed before the reader drained final output")
	case <-time.After(50 * time.Millisecond):
	}
	close(proc.readRelease)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("terminal exit did not finish after output drained")
	}
	if got := manager.snapshot("workspace", view.ID); got != "final output" {
		t.Fatalf("final terminal snapshot = %q, want final output", got)
	}
	manager.closeAll()
}

func TestTerminalReadOnlyTransitionClosesAndReopensTheTabGate(t *testing.T) {
	app := NewApp()
	root := t.TempDir()
	app.tabs["tab"] = &WorkspaceTab{ID: "tab", Scope: "project", WorkspaceRoot: root}
	app.tabOrder = []string{"tab"}
	app.activeTabID = "tab"

	manager := newTerminalManager(nil)
	app.terminals = manager
	started := make([]*fakeTerminalProcess, 0, 2)
	manager.start = func(terminalStartSpec) (terminalProcess, error) {
		proc := newFakeTerminalProcess()
		started = append(started, proc)
		return proc, nil
	}

	if _, err := manager.create("tab", "workspace", root, terminalCommand{path: "shell", label: "shell"}); err != nil {
		t.Fatal(err)
	}
	app.setTabReadOnly("tab", true)
	if !app.tabs["tab"].ReadOnly {
		t.Fatal("tab did not enter read-only mode")
	}
	select {
	case <-started[0].closed:
	default:
		t.Fatal("entering read-only mode did not close the terminal process")
	}
	if got := manager.list("workspace"); len(got) != 0 {
		t.Fatalf("read-only tab retained terminal sessions: %+v", got)
	}
	if _, err := manager.create("tab", "workspace", root, terminalCommand{path: "shell", label: "shell"}); !errors.Is(err, errTerminalStaleTab) {
		t.Fatalf("create while read-only gate is closed = %v, want errTerminalStaleTab", err)
	}

	app.setTabReadOnly("tab", false)
	if app.tabs["tab"].ReadOnly {
		t.Fatal("tab did not return to writable mode")
	}
	if _, err := manager.create("tab", "workspace", root, terminalCommand{path: "shell", label: "shell"}); err != nil {
		t.Fatalf("create after writable transition: %v", err)
	}
	manager.closeAll()
}

func TestTerminalWorkspaceRebindClosesOldSessionsAndReopensTheTabGate(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	app := NewApp()
	oldRoot := t.TempDir()
	newRoot := t.TempDir()
	tab := &WorkspaceTab{
		ID:            "tab",
		Scope:         "project",
		WorkspaceRoot: oldRoot,
		SessionPath:   filepath.Join(oldRoot, "old-session.jsonl"),
	}
	app.tabs["tab"] = tab
	app.tabOrder = []string{"tab"}
	app.activeTabID = "tab"

	manager := newTerminalManager(nil)
	app.terminals = manager
	started := make([]*fakeTerminalProcess, 0, 2)
	manager.start = func(terminalStartSpec) (terminalProcess, error) {
		proc := newFakeTerminalProcess()
		started = append(started, proc)
		return proc, nil
	}

	oldTarget, err := app.terminalTargetForTab("tab", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.create("tab", oldTarget.workspaceKey, oldTarget.workspaceRoot, terminalCommand{path: "shell", label: "shell"}); err != nil {
		t.Fatal(err)
	}

	app.applySessionBindingToTab(tab, sessionBinding{
		path:          filepath.Join(newRoot, "new-session.jsonl"),
		scope:         "project",
		workspaceRoot: newRoot,
	})

	select {
	case <-started[0].closed:
	default:
		t.Fatal("workspace rebind did not close the old terminal process")
	}
	if got := manager.list(oldTarget.workspaceKey); len(got) != 0 {
		t.Fatalf("workspace rebind retained old terminal sessions: %+v", got)
	}
	newTarget, err := app.terminalTargetForTab("tab", false)
	if err != nil {
		t.Fatal(err)
	}
	if newTarget.workspaceKey == oldTarget.workspaceKey {
		t.Fatalf("workspace rebind retained terminal scope %q", newTarget.workspaceKey)
	}
	if _, err := manager.create("tab", newTarget.workspaceKey, newTarget.workspaceRoot, terminalCommand{path: "shell", label: "shell"}); err != nil {
		t.Fatalf("create after workspace rebind: %v", err)
	}
	manager.closeAll()
}

func TestTerminalManagerCloseAndNaturalExit(t *testing.T) {
	t.Run("close cleans up process", func(t *testing.T) {
		manager := newTerminalManager(nil)
		proc := newFakeTerminalProcess()
		manager.start = func(terminalStartSpec) (terminalProcess, error) { return proc, nil }
		view, err := manager.create("tab", "workspace", ".", terminalCommand{path: "shell", label: "shell"})
		if err != nil {
			t.Fatal(err)
		}
		if err := manager.closeTerminal("workspace", view.ID); err != nil {
			t.Fatal(err)
		}
		select {
		case <-proc.closed:
		default:
			t.Fatal("terminal process was not closed")
		}
		if got := manager.list("workspace"); len(got) != 0 {
			t.Fatalf("sessions after close = %+v", got)
		}
	})

	t.Run("natural exit updates view", func(t *testing.T) {
		manager := newTerminalManager(nil)
		proc := newFakeTerminalProcess()
		manager.start = func(terminalStartSpec) (terminalProcess, error) { return proc, nil }
		view, err := manager.create("tab", "workspace", ".", terminalCommand{path: "shell", label: "shell"})
		if err != nil {
			t.Fatal(err)
		}
		manager.mu.Lock()
		done := manager.sessions[view.ID].done
		manager.mu.Unlock()
		proc.waitResult <- fakeTerminalWait{code: 7, err: errors.New("exit status 7")}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("terminal wait loop did not finish")
		}
		sessions := manager.list("workspace")
		if len(sessions) != 1 || sessions[0].Running || sessions[0].ExitCode == nil || *sessions[0].ExitCode != 7 {
			t.Fatalf("session after exit = %+v", sessions)
		}
		manager.closeAll()
	})
}

func TestTerminalManagerClosesOnlyTheClosingTabAndBoundsOutput(t *testing.T) {
	manager := newTerminalManager(nil)
	procs := make([]*fakeTerminalProcess, 0, 2)
	manager.start = func(terminalStartSpec) (terminalProcess, error) {
		proc := newFakeTerminalProcess()
		procs = append(procs, proc)
		return proc, nil
	}
	first, err := manager.create("tab-one", "tab-one\x00workspace", ".", terminalCommand{path: "shell", label: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.create("tab-two", "tab-two\x00workspace", ".", terminalCommand{path: "shell", label: "shell"})
	if err != nil {
		t.Fatal(err)
	}

	manager.mu.Lock()
	manager.sessions[first.ID].output = appendTerminalSnapshot(
		[]byte(strings.Repeat("x", maxTerminalSnapshotBytes)),
		[]byte("y"),
	)
	manager.mu.Unlock()
	if got := manager.snapshot("tab-one\x00workspace", first.ID); len(got) != maxTerminalSnapshotBytes || !strings.HasSuffix(got, "y") {
		t.Fatalf("bounded snapshot = len %d suffix %q", len(got), got[len(got)-1:])
	}
	manager.mu.Lock()
	manager.sessions[first.ID].output = appendTerminalSnapshot(nil, []byte(strings.Repeat("z", maxTerminalSnapshotBytes+1)))
	manager.mu.Unlock()
	if got := manager.snapshot("tab-one\x00workspace", first.ID); len(got) != maxTerminalSnapshotBytes || !strings.HasPrefix(got, "z") {
		t.Fatalf("large bounded snapshot = len %d prefix %q", len(got), got[:1])
	}

	manager.closeForTab("tab-one")
	select {
	case <-procs[0].closed:
	case <-time.After(time.Second):
		t.Fatal("closing tab did not close its terminal")
	}
	select {
	case <-procs[1].closed:
		t.Fatal("closing tab closed another tab's terminal")
	default:
	}
	if got := manager.list("tab-one\x00workspace"); len(got) != 0 {
		t.Fatalf("closed tab sessions = %+v", got)
	}
	if got := manager.list("tab-two\x00workspace"); len(got) != 1 || got[0].ID != second.ID {
		t.Fatalf("surviving tab sessions = %+v", got)
	}
	manager.closeAll()
}
