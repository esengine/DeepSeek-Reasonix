package control

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/gitstatus"
	"reasonix/internal/state/sessioninbox"
	"reasonix/internal/state/workspacelease"
	"reasonix/internal/tools/jobs"
)

// branchFixture is a controller whose workspace is a real repository with one
// commit on the init branch and a second local "next", the way a session that
// opened the folder holds it.
func branchFixture(t *testing.T) (*Controller, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	dir := testenv.TempDir(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "core.autocrlf=false"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	// The controller's later checkouts must use the fixture's LF bytes too.
	git("config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "base")
	git("checkout", "-qb", "next")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("commit", "-qam", "next")
	git("checkout", "-q", "main")
	repo, err := gitcmd.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	c := New(Options{
		SessionDir:    testenv.TempDir(t),
		Label:         "test",
		Sink:          event.Discard,
		WorkspaceRoot: dir,
		WorkspaceRepo: repo,
	})
	t.Cleanup(c.Close)
	return c, dir
}

// The write is answered with the work tree's identity as it now stands, so
// the caller updates its branch reading from the same round trip.
func TestSwitchWorkspaceBranchMovesHead(t *testing.T) {
	c, _ := branchFixture(t)
	info, ok, err := c.SwitchWorkspaceBranch(context.Background(), "next")
	if err != nil || !ok {
		t.Fatalf("switch: %v ok=%v", err, ok)
	}
	if info.Branch != "next" || info.Detached {
		t.Fatalf("answer should carry the new identity, got %+v", info)
	}
	// The unknown name is the platform's refusal, carried as-is.
	if _, _, err := c.SwitchWorkspaceBranch(context.Background(), "no-such-branch"); !errors.Is(err, gitstatus.ErrBranchUnknown) {
		t.Fatalf("want ErrBranchUnknown, got %v", err)
	}
}

// The checkout rewrites the files a turn is working in, so a running turn
// refuses the write by type and the tree stays put.
func TestSwitchWorkspaceBranchRefusedWhileATurnRuns(t *testing.T) {
	c, _ := branchFixture(t)
	c.mu.Lock()
	c.gate.running = true
	c.mu.Unlock()
	_, _, err := c.SwitchWorkspaceBranch(context.Background(), "next")
	if !errors.Is(err, ErrTurnRunning) {
		t.Fatalf("want ErrTurnRunning, got %v", err)
	}
	info, ok := gitstatus.Summary(context.Background(), c.workspaceRepo)
	if !ok || info.Branch != "main" {
		t.Fatalf("the refused switch must not move HEAD: %+v ok=%v", info, ok)
	}
}

func branchPeer(t *testing.T, root string, runner *gatedTurnRunner, manager *jobs.Manager) *Controller {
	t.Helper()
	repo, err := gitcmd.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	c := New(Options{Runner: runner, WorkspaceRoot: root, WorkspaceRepo: repo, Jobs: manager, Sink: event.Discard})
	t.Cleanup(c.Close)
	return c
}

func branchSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for workspace operation")
	}
}

func assertWorkspaceBranch(t *testing.T, c *Controller, branch, contents string) {
	t.Helper()
	info, ok := gitstatus.Summary(t.Context(), c.workspaceRepo)
	if !ok || info.Branch != branch {
		t.Fatalf("branch = %+v, ok=%v, want %s", info, ok, branch)
	}
	got, err := os.ReadFile(filepath.Join(c.workspaceRepo.WorkTree, "a.txt"))
	if err != nil || string(got) != contents {
		t.Fatalf("workspace bytes = %q, err=%v, want %q", got, err, contents)
	}
}

func TestSwitchWorkspaceBranchRefusesOtherPaneTurns(t *testing.T) {
	for _, asynchronous := range []bool{false, true} {
		for _, location := range []string{"root", "subdirectory", "symlink"} {
			t.Run(fmt.Sprintf("async=%t/%s", asynchronous, location), func(t *testing.T) {
				c, dir := branchFixture(t)
				peerRoot := dir
				if location == "subdirectory" {
					peerRoot = filepath.Join(dir, "sub")
					if err := os.Mkdir(peerRoot, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if location == "symlink" {
					peerRoot = filepath.Join(testenv.TempDir(t), "alias")
					if err := os.Symlink(dir, peerRoot); err != nil {
						t.Skip(err)
					}
				}
				runner := &gatedTurnRunner{started: make(chan struct{}), release: make(chan struct{})}
				release := sync.OnceFunc(func() { close(runner.release) })
				t.Cleanup(release)
				peer := branchPeer(t, peerRoot, runner, nil)
				done := make(chan error, 1)
				if asynchronous {
					peer.Send("hold workspace")
				} else {
					go func() { done <- peer.RunTurn(t.Context(), "hold workspace") }()
				}
				branchSignal(t, runner.started)
				if _, _, err := c.SwitchWorkspaceBranch(t.Context(), "next"); !errors.Is(err, ErrTurnRunning) {
					t.Errorf("switch during other pane's turn = %v, want ErrTurnRunning", err)
				}
				assertWorkspaceBranch(t, c, "main", "one\n")
				release()
				if asynchronous {
					if err := peer.waitTurnIdle(t.Context()); err != nil {
						t.Fatal(err)
					}
					peer.autosaveWG.Wait()
				} else if err := <-done; err != nil {
					t.Fatal(err)
				}
				if _, _, err := c.SwitchWorkspaceBranch(t.Context(), "next"); err != nil {
					t.Fatal(err)
				}
				assertWorkspaceBranch(t, c, "next", "two\n")
			})
		}
	}
}

func TestSwitchWorkspaceBranchWaitsForBackgroundWorkToExit(t *testing.T) {
	c, dir := branchFixture(t)
	manager := jobs.NewManager(event.Discard)
	peer := branchPeer(t, dir, nil, manager)
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	var jobID string
	err := peer.runSynchronousTurn(t.Context(), nil, func(context.Context) error {
		job := manager.Start("bash", "hold workspace", func(context.Context, io.Writer) (string, error) { close(started); <-release; return "", nil })
		jobID = job.ID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	branchSignal(t, started)
	for _, switcher := range []*Controller{c, peer} {
		if _, _, err := switcher.SwitchWorkspaceBranch(t.Context(), "next"); !errors.Is(err, ErrJobsRunning) || errors.Is(err, ErrTurnRunning) {
			t.Fatalf("running background job refusal = %v", err)
		}
	}
	manager.Kill(jobID)
	if _, _, err := c.SwitchWorkspaceBranch(t.Context(), "next"); !errors.Is(err, ErrJobsRunning) {
		t.Errorf("switch while cancelled job still runs = %v, want ErrJobsRunning", err)
	}
	assertWorkspaceBranch(t, c, "main", "one\n")
	unblock()
	manager.Wait(t.Context(), []string{jobID}, jobs.WaitOptions{})
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, _, err := c.SwitchWorkspaceBranch(t.Context(), "next")
		if err == nil {
			break
		}
		if !errors.Is(err, ErrJobsRunning) || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	assertWorkspaceBranch(t, c, "next", "two\n")
}

func TestSwitchWorkspaceBranchExcludesConcurrentTurnAdmission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX executable shim")
	}
	c, dir := branchFixture(t)
	runner := &gatedTurnRunner{started: make(chan struct{}), release: make(chan struct{})}
	releaseTurn := sync.OnceFunc(func() { close(runner.release) })
	t.Cleanup(releaseTurn)
	peer := branchPeer(t, dir, runner, nil)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	shimDir := testenv.TempDir(t)
	entered, release := filepath.Join(shimDir, "entered"), filepath.Join(shimDir, "release")
	script := fmt.Sprintf("#!/bin/sh\nfor arg in \"$@\"; do\nif [ \"$arg\" = switch ]; then\n: > %q\nwhile [ ! -e %q ]; do sleep 0.01; done\nbreak\nfi\ndone\nexec %q \"$@\"\n", entered, release, realGit)
	if err := os.WriteFile(filepath.Join(shimDir, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	unblock := sync.OnceFunc(func() {
		if err := os.WriteFile(release, nil, 0600); err != nil {
			t.Error(err)
		}
	})
	t.Cleanup(unblock)
	switched := make(chan error, 1)
	go func() { _, _, err := c.SwitchWorkspaceBranch(t.Context(), "next"); switched <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(entered); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("switch did not reach git")
		}
		time.Sleep(time.Millisecond)
	}

	if err := peer.beginRotation(); !errors.Is(err, errTurnRunningRotation) {
		t.Fatalf("rotation during checkout = %v", err)
	}
	type pendingPeer struct {
		c      *Controller
		runner *inboxDispatchRunner
		action string
	}
	var pending []pendingPeer
	for _, action := range []string{"cancel direct", "close direct", "cancel queued", "close queued", "keep queued"} {
		r := &inboxDispatchRunner{inputs: make(chan string, 8)}
		pendingController := New(Options{Runner: r, WorkspaceRoot: dir, WorkspaceRepo: c.workspaceRepo,
			SessionDir: testenv.TempDir(t), SessionPath: filepath.Join(testenv.TempDir(t), "session.jsonl"), Sink: event.Discard})
		t.Cleanup(func() { pendingController.Close(); pendingController.autosaveWG.Wait() })
		pending = append(pending, pendingPeer{pendingController, r, action})
		returned := make(chan error, 1)
		if strings.HasSuffix(action, "direct") {
			go func() { pendingController.Send(action); returned <- nil }()
		} else {
			queued, err := pendingController.EnqueueInbox(InboxRequest{Submit: action, Source: "desktop", Intent: sessioninbox.IntentFollowup})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				receipt, err := pendingController.TrySubmitInboxItem(queued.ItemID)
				if err == nil && receipt.Disposition != sessioninbox.DispositionRejectedBusy {
					err = fmt.Errorf("checkout admission = %s", receipt.Disposition)
				}
				returned <- err
			}()
			select {
			case err := <-returned:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				unblock()
				<-returned
				t.Fatal("durable admission blocked behind checkout")
			}
			meta, _, err := pendingController.ReadInboxItem(queued.ItemID)
			if err != nil || meta.State != sessioninbox.StateQueued {
				t.Fatalf("checkout must leave input queued: %+v %v", meta, err)
			}
			if action == "keep queued" {
				for range 3 {
					receipt, err := pendingController.TrySubmitInboxItem(queued.ItemID)
					if err != nil || receipt.Disposition != sessioninbox.DispositionRejectedBusy {
						t.Fatalf("repeated queued admission = %+v %v", receipt, err)
					}
				}
			}
			if action == "cancel queued" {
				if err := pendingController.CancelWithInboxItems([]string{queued.ItemID}, "desktop"); err != nil {
					t.Fatal(err)
				}
			}
			if action == "close queued" {
				pendingController.Close()
			}
			continue
		}
		select {
		case err := <-returned:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			unblock()
			<-returned
			t.Fatal("direct Send blocked behind checkout")
		}
		if action == "cancel direct" {
			pendingController.Cancel()
		} else {
			pendingController.Close()
		}
	}
	canceled, cancel := context.WithCancel(t.Context())
	canceledTurn := make(chan error, 1)
	go func() { canceledTurn <- peer.RunTurn(canceled, "cancel before admission") }()
	key, err := workspacelease.CanonicalWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		workspaceActivities.Lock()
		entry := workspaceActivities.entries[key]
		waiting := entry != nil && entry.references >= 2
		workspaceActivities.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not wait for workspace admission")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-canceledTurn:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled admission = %v", err)
		}
	case <-time.After(time.Second):
		unblock()
		<-canceledTurn
		t.Fatal("canceled turn admission remained blocked behind checkout")
	}
	done := make(chan error, 1)
	go func() { done <- peer.RunTurn(t.Context(), "hold workspace") }()
	select {
	case <-runner.started:
		t.Error("turn started while git switch was rewriting the workspace")
	case <-time.After(100 * time.Millisecond):
	}
	unblock()
	if err := <-switched; err != nil {
		t.Fatal(err)
	}
	branchSignal(t, runner.started)
	assertWorkspaceBranch(t, c, "next", "two\n")
	releaseTurn()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, waiting := range pending {
		if waiting.action == "keep queued" {
			if input := waitForInboxDispatch(t, waiting.runner); input != waiting.action {
				t.Fatalf("resumed input = %q", input)
			}
			if err := waiting.c.waitTurnIdle(t.Context()); err != nil {
				t.Fatal(err)
			}
			select {
			case input := <-waiting.runner.inputs:
				t.Fatalf("duplicate queued dispatch: %q", input)
			default:
			}
			continue
		}
		select {
		case input := <-waiting.runner.inputs:
			t.Errorf("%s unexpectedly ran after checkout: %q", waiting.action, input)
		case <-time.After(30 * time.Millisecond):
		}
	}
	assertWorkspaceGuardReleased(t, c)
}

func TestSwitchWorkspaceBranchGuardSpansFinishingAndParkedTurns(t *testing.T) {
	c, dir := branchFixture(t)
	finishing, releaseFinishing := make(chan struct{}, 1), make(chan struct{})
	releaseFirst := sync.OnceFunc(func() { close(releaseFinishing) })
	t.Cleanup(releaseFirst)
	peer := New(Options{WorkspaceRoot: dir, WorkspaceRepo: c.workspaceRepo, Sink: holdFinishingWindow(releaseFinishing, finishing, nil)})
	t.Cleanup(peer.Close)
	if got := peer.runGuarded(func(context.Context) error { return nil }); got != turnStarted {
		t.Fatal(got)
	}
	branchSignal(t, finishing)
	if _, _, err := c.SwitchWorkspaceBranch(t.Context(), "next"); !errors.Is(err, ErrTurnRunning) {
		t.Fatalf("switch while other pane finishes = %v", err)
	}
	started, releaseParked := make(chan struct{}), make(chan struct{})
	releaseSecond := sync.OnceFunc(func() { close(releaseParked) })
	t.Cleanup(releaseSecond)
	if got := peer.runGuardedOrPark(func(context.Context) error { close(started); <-releaseParked; return nil }); got != turnParked {
		t.Fatal(got)
	}
	releaseFirst()
	branchSignal(t, started)
	if _, _, err := c.SwitchWorkspaceBranch(t.Context(), "next"); !errors.Is(err, ErrTurnRunning) {
		t.Fatalf("switch during parked turn = %v", err)
	}
	releaseSecond()
	if err := peer.waitTurnIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	peer.autosaveWG.Wait()
	if _, _, err := c.SwitchWorkspaceBranch(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceGuardReleased(t, c)
}

func assertWorkspaceGuardReleased(t *testing.T, c *Controller) {
	t.Helper()
	key, err := workspacelease.CanonicalWorkspace(c.workspaceRepo.WorkTree)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		workspaceActivities.Lock()
		_, held := workspaceActivities.entries[key]
		workspaceActivities.Unlock()
		if !held {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("workspace guard registry retained an idle workspace")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSwitchWorkspaceBranchGuardReleasesOnErrorsAndRotation(t *testing.T) {
	c, dir := branchFixture(t)
	peer := branchPeer(t, dir, nil, nil)
	refusal := errors.New("admission refused")
	if err := peer.runSynchronousTurn(t.Context(), func() error { return refusal }, func(context.Context) error { t.Fatal("refused body ran"); return nil }); !errors.Is(err, refusal) {
		t.Fatal(err)
	}
	assertWorkspaceGuardReleased(t, c)
	if err := peer.beginRotation(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.SwitchWorkspaceBranch(t.Context(), "next"); !errors.Is(err, ErrTurnRunning) {
		t.Fatalf("switch during another controller's rotation = %v", err)
	}
	peer.endRotation()
	assertWorkspaceGuardReleased(t, c)
	if _, _, err := c.SwitchWorkspaceBranch(t.Context(), "missing"); !errors.Is(err, gitstatus.ErrBranchUnknown) {
		t.Fatal(err)
	}
	assertWorkspaceGuardReleased(t, c)
	if _, _, err := c.SwitchWorkspaceBranch(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceGuardReleased(t, c)
}

func TestSwitchWorkspaceBranchHonorsExistingWriterLease(t *testing.T) {
	c, dir := branchFixture(t)
	owner, err := workspacelease.New(dir, config.WorkspaceLeaseDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	owner.BeginRun()
	release := sync.OnceFunc(owner.EndRun)
	t.Cleanup(release)
	if err := owner.AcquirePaths(t.Context(), []string{filepath.Join(dir, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.SwitchWorkspaceBranch(t.Context(), "next"); !errors.Is(err, ErrWorkspaceBusy) || !errors.Is(err, workspacelease.ErrConflict) {
		t.Fatalf("switch must respect an existing workspace writer: %v", err)
	}
	assertWorkspaceBranch(t, c, "main", "one\n")
	assertWorkspaceGuardReleased(t, c)
	release()
	if _, _, err := c.SwitchWorkspaceBranch(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceGuardReleased(t, c)
}

func TestSwitchWorkspaceBranchDoesNotBlockOtherWorkspaces(t *testing.T) {
	c, _ := branchFixture(t)
	other, otherDir := branchFixture(t)
	runner := &gatedTurnRunner{started: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(runner.release) })
	t.Cleanup(release)
	peer := branchPeer(t, otherDir, runner, nil)
	done := make(chan error, 1)
	go func() { done <- peer.RunTurn(t.Context(), "other workspace") }()
	branchSignal(t, runner.started)
	if _, _, err := c.SwitchWorkspaceBranch(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceBranch(t, c, "next", "two\n")
	assertWorkspaceBranch(t, other, "main", "one\n")
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	assertWorkspaceGuardReleased(t, c)
	assertWorkspaceGuardReleased(t, other)
}

func TestCancelWithInboxItemsRestartsSurvivorAfterCheckoutWake(t *testing.T) {
	c, dir := branchFixture(t)
	runner := &inboxDispatchRunner{inputs: make(chan string, 8)}
	peer := New(Options{Runner: runner, WorkspaceRoot: dir, WorkspaceRepo: c.workspaceRepo,
		SessionDir: testenv.TempDir(t), SessionPath: filepath.Join(testenv.TempDir(t), "session.jsonl"), Sink: event.Discard})
	t.Cleanup(func() { peer.Close(); peer.autosaveWG.Wait() })
	releaseCheckout, err := c.excludeWorkspaceActivity()
	if err != nil {
		t.Fatal(err)
	}
	finishCheckout := sync.OnceFunc(releaseCheckout)
	t.Cleanup(finishCheckout)
	cancelled, err := peer.EnqueueInbox(InboxRequest{Submit: "cancel mine", Source: "desktop", Intent: sessioninbox.IntentFollowup})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := peer.EnqueueInbox(InboxRequest{Submit: "keep unrelated", Source: "other", Intent: sessioninbox.IntentFollowup}); err != nil {
		t.Fatal(err)
	}
	receipt, err := peer.TrySubmitInboxItem(cancelled.ItemID)
	if err != nil || receipt.Disposition != sessioninbox.DispositionRejectedBusy {
		t.Fatalf("checkout admission = %+v %v", receipt, err)
	}
	scanned := make(chan struct{}, 1)
	peer.inbox.mu.Lock()
	peer.inbox.afterDispatchScan = func(found bool) {
		if !found {
			select {
			case scanned <- struct{}{}:
			default:
			}
		}
	}
	peer.inbox.mu.Unlock()
	// Hold Cancel at its normal goal-state read, after it pauses the inbox.
	peer.goals.mu.Lock()
	releaseCancel := sync.OnceFunc(peer.goals.mu.Unlock)
	t.Cleanup(releaseCancel)
	cancelledDone := make(chan error, 1)
	go func() { cancelledDone <- peer.CancelWithInboxItems([]string{cancelled.ItemID}, "desktop") }()
	deadline := time.Now().Add(5 * time.Second)
	for !peer.InboxSnapshot().Paused {
		if time.Now().After(deadline) {
			t.Fatal("cancellation did not pause the inbox")
		}
		time.Sleep(time.Millisecond)
	}
	finishCheckout()
	branchSignal(t, scanned)
	releaseCancel()
	if err := <-cancelledDone; err != nil {
		t.Fatal(err)
	}
	if input := waitForInboxDispatch(t, runner); input != "keep unrelated" {
		t.Fatalf("resumed input = %q", input)
	}
	if err := peer.waitTurnIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case input := <-runner.inputs:
		t.Fatalf("unexpected additional dispatch: %q", input)
	default:
	}
	assertWorkspaceGuardReleased(t, c)
}

type branchLeaseWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *branchLeaseWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestSwitchWorkspaceLeaseWaitKeepsAdmissionOpen(t *testing.T) {
	c, dir := branchFixture(t)
	owner, err := workspacelease.New(dir, config.WorkspaceLeaseDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	owner.BeginRun()
	defer owner.EndRun()
	if err := owner.AcquireWrite(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	observed := &branchLeaseWaitContext{Context: ctx, waiting: make(chan struct{})}
	result := make(chan error, 1)
	go func() { _, _, err := c.SwitchWorkspaceBranch(observed, "next"); result <- err }()
	branchSignal(t, observed.waiting)
	peer := branchPeer(t, dir, nil, nil)
	running, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	admitted := peer.runGuarded(func(context.Context) error { close(running); <-release; return nil })
	if admitted != turnStarted {
		t.Errorf("lease contention blocked another pane's input: %v", admitted)
	} else {
		branchSignal(t, running)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrWorkspaceBusy) {
			t.Errorf("lease refusal must be internally bounded, got %v", err)
		}
	case <-time.After(time.Second):
		cancel()
		<-result
		t.Error("branch switch waited indefinitely for the workspace lease")
	}
	assertWorkspaceBranch(t, c, "main", "one\n")
	unblock()
	if err := peer.waitTurnIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	peer.autosaveWG.Wait()
	assertWorkspaceGuardReleased(t, c)
}

func TestSwitchWorkspaceBranchPreservesCallerCancellation(t *testing.T) {
	c, _ := branchFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := c.SwitchWorkspaceBranch(ctx, "next"); !errors.Is(err, context.Canceled) || errors.Is(err, ErrWorkspaceBusy) {
		t.Fatalf("canceled request = %v", err)
	}
	assertWorkspaceBranch(t, c, "main", "one\n")
	assertWorkspaceGuardReleased(t, c)
}
