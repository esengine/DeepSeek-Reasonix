//go:build darwin && cgo

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/unix"
	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/provider"
)

const darwinLowNoFileChild = "REASONIX_DARWIN_LOW_NOFILE_CHILD"

func TestDarwinWorkspaceWatcherReportsDeepFileOperations(t *testing.T) {
	root := canonicalWorkspaceRoot(t.TempDir())
	w := newDarwinWatcherForTest(t)
	if err := w.Add(root, true); err != nil {
		t.Fatal(err)
	}

	deep := filepath.Join(root, "目录 with spaces", "nested")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(deep, "文件.txt")
	if err := os.WriteFile(path, []byte("created"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForDarwinWorkspaceEvent(t, w, func(event fsnotify.Event) bool {
		return filepath.Clean(event.Name) == path && event.Op&fsnotify.Create != 0
	})

	if err := os.WriteFile(path, []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForDarwinWorkspaceEvent(t, w, func(event fsnotify.Event) bool {
		return filepath.Clean(event.Name) == path && event.Op&fsnotify.Write != 0
	})

	renamed := filepath.Join(deep, "renamed 文件.txt")
	if err := os.Rename(path, renamed); err != nil {
		t.Fatal(err)
	}
	waitForDarwinWorkspaceEvent(t, w, func(event fsnotify.Event) bool {
		name := filepath.Clean(event.Name)
		return (name == path || name == renamed) && event.Op&fsnotify.Rename != 0
	})

	if err := os.Remove(renamed); err != nil {
		t.Fatal(err)
	}
	waitForDarwinWorkspaceEvent(t, w, func(event fsnotify.Event) bool {
		return filepath.Clean(event.Name) == renamed && event.Op&fsnotify.Remove != 0
	})
}

func TestDarwinWorkspaceWatcherHonorsRecursiveModeAndRemove(t *testing.T) {
	root := canonicalWorkspaceRoot(t.TempDir())
	nested := filepath.Join(root, "child", "grandchild")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	w := newDarwinWatcherForTest(t)
	if err := w.Add(root, false); err != nil {
		t.Fatal(err)
	}

	direct := filepath.Join(root, "direct.txt")
	if err := os.WriteFile(direct, []byte("direct"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForDarwinWorkspaceEvent(t, w, func(event fsnotify.Event) bool {
		return filepath.Clean(event.Name) == direct
	})

	deep := filepath.Join(nested, "deep.txt")
	if err := os.WriteFile(deep, []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertNoDarwinWorkspaceEvent(t, w, deep, 400*time.Millisecond)

	if err := w.Remove(root); err != nil {
		t.Fatal(err)
	}
	removedWatchPath := filepath.Join(root, "after-remove.txt")
	if err := os.WriteFile(removedWatchPath, []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertNoDarwinWorkspaceEvent(t, w, removedWatchPath, 400*time.Millisecond)

	if err := w.Add(root, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deep, []byte("recursive"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForDarwinWorkspaceEvent(t, w, func(event fsnotify.Event) bool {
		return filepath.Clean(event.Name) == deep
	})
}

func TestDarwinWorkspaceWatcherMapsFlagsAndDegradesHub(t *testing.T) {
	op, overflow := darwinWorkspaceEvent(darwinFSEventItemRemoved | darwinFSEventItemRenamed | darwinFSEventItemCreated | darwinFSEventItemModified | darwinFSEventItemXattr)
	if overflow || op != fsnotify.Remove|fsnotify.Rename|fsnotify.Create|fsnotify.Write {
		t.Fatalf("mapped event = (%v, %v), want all operation bits without overflow", op, overflow)
	}
	if op, overflow = darwinWorkspaceEvent(darwinFSEventHistoryDone); op != 0 || overflow {
		t.Fatalf("HistoryDone mapped to (%v, %v), want ignored", op, overflow)
	}
	for name, flags := range map[string]uint32{
		"must-scan":      darwinFSEventMustScanSubDirs,
		"user-dropped":   darwinFSEventUserDropped,
		"kernel-dropped": darwinFSEventKernelDropped,
		"ids-wrapped":    darwinFSEventEventIDsWrapped,
		"mount":          darwinFSEventMount,
		"unmount":        darwinFSEventUnmount,
	} {
		t.Run(name, func(t *testing.T) {
			if op, overflow := darwinWorkspaceEvent(flags); op != 0 || !overflow {
				t.Fatalf("mapped event = (%v, %v), want overflow only", op, overflow)
			}
		})
	}
	if op, overflow = darwinWorkspaceEvent(darwinFSEventRootChanged); op&fsnotify.Rename == 0 || !overflow {
		t.Fatalf("RootChanged mapped to (%v, %v), want rename and overflow", op, overflow)
	}

	root := t.TempDir()
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	if state := app.WorkspaceRevisionForTab("a").WatchState; state != "active" {
		t.Fatalf("initial watch state = %s, want active", state)
	}
	key := canonicalWorkspaceRoot(root)
	app.workspaceHub.mu.Lock()
	r := app.workspaceHub.roots[key]
	watcher, watcherOK := r.watcher.(*darwinWorkspaceWatcher)
	app.workspaceHub.mu.Unlock()
	if !watcherOK {
		t.Fatalf("watcher type = %T, want Darwin FSEvents watcher", r.watcher)
	}
	watcher.mu.Lock()
	sub := watcher.watches[key]
	watcher.mu.Unlock()
	if sub == nil {
		t.Fatal("workspace root subscription is missing")
	}
	// RootChanged mapping above proves the combined Rename+overflow contract.
	// Inject an overflow-only native flag here so the Hub degradation assertion
	// is deterministic and does not also remove the watched root.
	sub.publish(key, darwinFSEventMustScanSubDirs)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		app.workspaceHub.mu.Lock()
		degraded := r.state == "degraded" && r.allPaths
		app.workspaceHub.mu.Unlock()
		if degraded {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("dropped FSEvents notification did not degrade the hub with allPaths invalidation")
}

func TestDarwinWorkspaceWatcherCoalescesChannelOverflow(t *testing.T) {
	w := &darwinWorkspaceWatcher{
		events:  make(chan fsnotify.Event, 1),
		errors:  make(chan error, 1),
		watches: make(map[string]*darwinWorkspaceSubscription),
	}
	event := fsnotify.Event{Name: "/tmp/file", Op: fsnotify.Write}
	w.sendEvent(event)
	w.sendEvent(event)
	w.sendEvent(event)
	if err := <-w.errors; !errors.Is(err, fsnotify.ErrEventOverflow) {
		t.Fatalf("overflow error = %v", err)
	}
	select {
	case err := <-w.errors:
		t.Fatalf("duplicate overflow before recovery: %v", err)
	default:
	}
	<-w.events
	w.sendEvent(event)
	<-w.events
	w.sendEvent(event)
	w.sendEvent(event)
	if err := <-w.errors; !errors.Is(err, fsnotify.ErrEventOverflow) {
		t.Fatalf("overflow after successful recovery = %v", err)
	}
	w.closed.Store(true)
	close(w.events)
	close(w.errors)
	w.sendEvent(event)
	w.sendOverflow()
}

func TestDarwinWorkspaceWatcherKeepsWorkspaceAndExternalGitScope(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	gitDir := filepath.Join(base, "external git")
	if out, err := exec.Command("git", "init", "--separate-git-dir", gitDir, root).CombinedOutput(); err != nil {
		t.Fatalf("git init --separate-git-dir: %v: %s", err, out)
	}
	generated := filepath.Join(root, "node_modules", "pkg")
	if err := os.MkdirAll(generated, 0o700); err != nil {
		t.Fatal(err)
	}

	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	view := app.WorkspaceRevisionForTab("a")
	if view.WatchState != "active" {
		t.Fatalf("watch state = %s, want active", view.WatchState)
	}
	beforeContent := view.Revisions.Content
	for index := range 64 {
		path := filepath.Join(generated, fmt.Sprintf("generated-%03d.js", index))
		if err := os.WriteFile(path, []byte("generated"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(300 * time.Millisecond)
	view = app.WorkspaceRevisionForTab("a")
	if view.Revisions.Content != beforeContent || view.WatchState != "active" {
		t.Fatalf("generated churn changed view: before=%d after=%+v", beforeContent, view)
	}

	beforeGit := view.Revisions.GitMeta
	ref := filepath.Join(gitDir, "refs", "heads", "watch-test")
	if err := os.WriteFile(ref, []byte("0000000000000000000000000000000000000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterGit := waitForDarwinGitRevision(t, app, beforeGit)
	afterGit = waitForDarwinGitRevisionToSettle(t, app, afterGit)
	objectDir := filepath.Join(gitDir, "objects", "ff")
	if err := os.MkdirAll(objectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(objectDir, "ignored"), []byte("object"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := app.WorkspaceRevisionForTab("a").Revisions.GitMeta; got != afterGit {
		t.Fatalf("Git objects churn advanced metadata revision: before=%d after=%d", afterGit, got)
	}
}

func TestDarwinWorkspaceWatcherConcurrentLifecycle(t *testing.T) {
	base := t.TempDir()
	paths := make([]string, 24)
	for index := range paths {
		paths[index] = filepath.Join(base, fmt.Sprintf("root-%02d", index))
		if err := os.Mkdir(paths[index], 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w, err := newWorkspaceWatcher()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths[:8] {
		if err := w.Add(path, true); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	errCh := make(chan error, len(paths))
	var wg sync.WaitGroup
	for index, path := range paths {
		wg.Add(1)
		go func(index int, path string) {
			defer wg.Done()
			<-start
			if err := w.Add(path, index%2 == 0); err != nil && !errors.Is(err, fsnotify.ErrClosed) {
				errCh <- err
				return
			}
			if err := w.Remove(path); err != nil {
				errCh <- err
			}
		}(index, path)
	}
	close(start)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent watcher lifecycle: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	darwin := w.(*darwinWorkspaceWatcher)
	darwin.mu.Lock()
	remaining := len(darwin.watches)
	darwin.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("remaining subscriptions after Close = %d", remaining)
	}
}

func TestDarwinWorkspaceWatcherCloseWaitsForConcurrentRemove(t *testing.T) {
	watcher, err := newWorkspaceWatcher()
	if err != nil {
		t.Fatal(err)
	}
	w := watcher.(*darwinWorkspaceWatcher)
	root := canonicalWorkspaceRoot(t.TempDir())
	stopEntered := make(chan struct{})
	allowStop := make(chan struct{})
	sub := &darwinWorkspaceSubscription{
		watcher: w,
		path:    root,
		stopNative: func() {
			close(stopEntered)
			<-allowStop
		},
	}
	w.mu.Lock()
	w.watches[root] = sub
	w.mu.Unlock()

	removeDone := make(chan error, 1)
	go func() { removeDone <- w.Remove(root) }()
	<-stopEntered
	closeDone := make(chan error, 1)
	go func() { closeDone <- w.Close() }()
	deadline := time.Now().Add(time.Second)
	for !w.closed.Load() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if !w.closed.Load() {
		t.Fatal("Close did not start")
	}
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before concurrent Remove stopped: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(allowStop)
	if err := <-removeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if _, ok := <-w.Events(); ok {
		t.Fatal("event channel remained open after Close")
	}
	if _, ok := <-w.Errors(); ok {
		t.Fatal("error channel remained open after Close")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}
}

func TestDarwinWorkspaceWatcherLowFileDescriptorLimit(t *testing.T) {
	if os.Getenv(darwinLowNoFileChild) == "1" {
		runDarwinLowNoFileChild(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDarwinWorkspaceWatcherLowFileDescriptorLimit$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), darwinLowNoFileChild+"=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("low RLIMIT_NOFILE child failed: %v\n%s", err, output)
	}
}

func runDarwinLowNoFileChild(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "large workspace")
	deep := filepath.Join(root, "one", "two", "three")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := range 512 {
		path := filepath.Join(root, fmt.Sprintf("root-file-%03d.txt", index))
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	projectConfig := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(projectConfig, []byte("[agent]\nmemory_compiler = { enabled = true, verbosity = \"compact\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	userConfig := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, []byte("[agent]\nmemory_compiler = \"compact\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var oldLimit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &oldLimit); err != nil {
		t.Fatal(err)
	}
	limit := oldLimit
	if limit.Max < 256 {
		t.Fatalf("hard RLIMIT_NOFILE = %d, need at least 256", limit.Max)
	}
	limit.Cur = 256
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Setrlimit(unix.RLIMIT_NOFILE, &oldLimit) })

	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	view := app.WorkspaceRevisionForTab("a")
	if view.WatchState != "active" {
		t.Fatalf("watch state under RLIMIT_NOFILE=256 = %s", view.WatchState)
	}
	if fds := darwinOpenFDCount(t); fds >= 128 {
		t.Fatalf("FSEvents watcher opened too many descriptors: %d", fds)
	}

	deepFile := filepath.Join(deep, "external-write.txt")
	beforeContent := view.Revisions.Content
	if err := os.WriteFile(deepFile, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForDarwinContentRevision(t, app, beforeContent)

	changed, err := config.MigrateLegacyMemoryCompilerForRoot(root)
	if err != nil || !changed {
		t.Fatalf("config migration changed=%v err=%v", changed, err)
	}
	for _, path := range []string{userConfig, projectConfig} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "memory_compiler") {
			t.Fatalf("legacy memory_compiler remains in %s", path)
		}
	}

	subagents := filepath.Join(base, "sessions", "subagents")
	if err := os.MkdirAll(subagents, 0o700); err != nil {
		t.Fatal(err)
	}
	if cleaned, err := agent.NewSubagentStore(subagents).CleanupStaleRunning(); err != nil || cleaned != 0 {
		t.Fatalf("subagent cleanup cleaned=%d err=%v", cleaned, err)
	}

	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	sessionPath := filepath.Join(base, "sessions", "low-limit.jsonl")
	if err := session.SaveSnapshot(sessionPath); err != nil {
		t.Fatalf("save session snapshot: %v", err)
	}
}

func newDarwinWatcherForTest(t *testing.T) workspaceWatcher {
	t.Helper()
	w, err := newWorkspaceWatcher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func waitForDarwinWorkspaceEvent(t *testing.T, watcher workspaceWatcher, match func(fsnotify.Event) bool) fsnotify.Event {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-watcher.Events():
			if !ok {
				t.Fatal("watcher event channel closed")
			}
			if match(event) {
				return event
			}
		case err, ok := <-watcher.Errors():
			if !ok {
				t.Fatal("watcher error channel closed")
			}
			t.Fatalf("watcher error: %v", err)
		case <-timer.C:
			t.Fatal("timed out waiting for FSEvents notification")
		}
	}
}

func assertNoDarwinWorkspaceEvent(t *testing.T, watcher workspaceWatcher, path string, duration time.Duration) {
	t.Helper()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-watcher.Events():
			if !ok {
				return
			}
			if filepath.Clean(event.Name) == path {
				t.Fatalf("unexpected event after non-recursive filter/removal: %v", event)
			}
		case err, ok := <-watcher.Errors():
			if ok {
				t.Fatalf("watcher error: %v", err)
			}
		case <-timer.C:
			return
		}
	}
}

func waitForDarwinContentRevision(t *testing.T, app *App, before uint64) uint64 {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := app.WorkspaceRevisionForTab("a").Revisions.Content; got > before {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("content revision did not advance beyond %d", before)
	return before
}

func waitForDarwinGitRevision(t *testing.T, app *App, before uint64) uint64 {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := app.WorkspaceRevisionForTab("a").Revisions.GitMeta; got > before {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Git metadata revision did not advance beyond %d", before)
	return before
}

func waitForDarwinGitRevisionToSettle(t *testing.T, app *App, last uint64) uint64 {
	t.Helper()
	stableSince := time.Now()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		got := app.WorkspaceRevisionForTab("a").Revisions.GitMeta
		if got != last {
			last = got
			stableSince = time.Now()
			continue
		}
		if time.Since(stableSince) >= 150*time.Millisecond {
			return got
		}
	}
	t.Fatal("Git metadata revision did not settle")
	return last
}

func darwinOpenFDCount(t *testing.T) int {
	t.Helper()
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	maxFD := min(limit.Cur, 4096)
	open := 0
	for fd := range maxFD {
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err == nil {
			open++
		}
	}
	return open
}
