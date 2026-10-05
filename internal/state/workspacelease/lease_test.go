package workspacelease

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// withWaitGrace shortens the silence a contended acquisition keeps, so a test
// can watch both ends of a reported wait without waiting out the real one.
func withWaitGrace(t *testing.T, d time.Duration) {
	t.Helper()
	previous := waitNoticeGrace
	waitNoticeGrace = d
	t.Cleanup(func() { waitNoticeGrace = previous })
}

// waitLog records what a session was told about one contended acquisition.
type waitLog struct {
	mu   sync.Mutex
	seen []Wait
}

func (l *waitLog) note(w Wait) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, w)
}

func (l *waitLog) outcomes() []WaitOutcome {
	l.mu.Lock()
	defer l.mu.Unlock()
	got := make([]WaitOutcome, 0, len(l.seen))
	for _, w := range l.seen {
		got = append(got, w.Outcome)
	}
	return got
}

func (l *waitLog) sameAs(want ...WaitOutcome) bool {
	got := l.outcomes()
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestWorkspaceLeaseHelperProcess(t *testing.T) {
	if os.Getenv("REASONIX_WORKSPACE_LEASE_HELPER") != "1" {
		return
	}
	root := os.Getenv("REASONIX_WORKSPACE_LEASE_ROOT")
	locks := os.Getenv("REASONIX_WORKSPACE_LEASE_DIR")
	ready := os.Getenv("REASONIX_WORKSPACE_LEASE_READY")
	o, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	if name := os.Getenv("REASONIX_WORKSPACE_LEASE_HOLDER"); name != "" {
		o.SetHolder(func() string { return name })
	}
	o.BeginRun()
	var acquireErr error
	if path := os.Getenv("REASONIX_WORKSPACE_LEASE_PATH"); path != "" {
		acquireErr = o.AcquirePaths(context.Background(), []string{path})
	} else {
		acquireErr = o.AcquireWrite(context.Background())
	}
	if acquireErr != nil {
		t.Fatal(acquireErr)
	}
	if err := os.WriteFile(ready, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestCanonicalWorkspaceResolvesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on some Windows builders")
	}
	real := testenv.TempDir(t)
	link := filepath.Join(testenv.TempDir(t), "workspace-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	got, err := CanonicalWorkspace(filepath.Join(link, "."))
	if err != nil {
		t.Fatal(err)
	}
	want, err := CanonicalWorkspace(real)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("canonical identities differ: got %q want %q", got, want)
	}
}

func TestCanonicalWorkspaceFoldsRepositorySubdirectoriesWithoutGitBinary(t *testing.T) {
	repo := testenv.TempDir(t)
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(repo, "packages", "app")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	rootIdentity, err := CanonicalWorkspace(repo)
	if err != nil {
		t.Fatal(err)
	}
	subdirIdentity, err := CanonicalWorkspace(subdir)
	if err != nil {
		t.Fatal(err)
	}
	if subdirIdentity != rootIdentity {
		t.Fatalf("repository subdirectory identity = %q, want root identity %q", subdirIdentity, rootIdentity)
	}
}

func TestCanonicalWorkspaceKeepsLinkedWorktreesIndependent(t *testing.T) {
	parent := testenv.TempDir(t)
	first := filepath.Join(parent, "worktree-one")
	second := filepath.Join(parent, "worktree-two")
	for _, root := range []string{first, second} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ../common\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	firstIdentity, err := CanonicalWorkspace(first)
	if err != nil {
		t.Fatal(err)
	}
	secondIdentity, err := CanonicalWorkspace(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstIdentity == secondIdentity {
		t.Fatalf("linked worktrees shared identity %q", firstIdentity)
	}
}

func TestRepositoryRootAndSubdirectoryOwnersSerialize(t *testing.T) {
	repo, locks := testenv.TempDir(t), testenv.TempDir(t)
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(repo, "nested", "project")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	rootOwner, err := New(repo, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	subdirOwner, err := New(subdir, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	rootOwner.BeginRun()
	subdirOwner.BeginRun()
	if err := rootOwner.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	if err := subdirOwner.AcquireWrite(ctx); !errors.Is(err, context.DeadlineExceeded) {
		cancel()
		t.Fatalf("repository subdirectory owner acquired independently: %v", err)
	}
	cancel()
	rootOwner.EndRun()
	if err := subdirOwner.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	subdirOwner.EndRun()
}

func TestOwnersSerializeSameWorkspaceAndReportTheWaitAsAPair(t *testing.T) {
	withWaitGrace(t, 20*time.Millisecond)
	root, locks := testenv.TempDir(t), testenv.TempDir(t)
	first, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	var log waitLog
	second, err := New(root, locks, log.note)
	if err != nil {
		t.Fatal(err)
	}
	first.BeginRun()
	second.BeginRun()
	if err := first.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}

	acquired := make(chan error, 1)
	go func() { acquired <- second.AcquireWrite(context.Background()) }()
	select {
	case err := <-acquired:
		t.Fatalf("second owner acquired early: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	first.EndRun()
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second owner did not acquire after release")
	}
	if !log.sameAs(WaitBegan, WaitAcquired) {
		t.Fatalf("wait reports = %v, want one began closed by one acquired", log.outcomes())
	}
	second.EndRun()
}

// A wait shorter than the grace is one nobody experienced as a wait, and a
// permanent line about it outlives the condition it describes.
func TestWaitUnderTheGraceIsNeverReported(t *testing.T) {
	withWaitGrace(t, 5*time.Second)
	root, locks := testenv.TempDir(t), testenv.TempDir(t)
	first, _ := New(root, locks, nil)
	var log waitLog
	second, _ := New(root, locks, log.note)
	first.BeginRun()
	second.BeginRun()
	if err := first.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	acquired := make(chan error, 1)
	go func() { acquired <- second.AcquireWrite(context.Background()) }()
	time.Sleep(20 * time.Millisecond)
	first.EndRun()
	if err := <-acquired; err != nil {
		t.Fatal(err)
	}
	if got := log.outcomes(); len(got) != 0 {
		t.Fatalf("wait reports = %v, want none for a wait inside the grace", got)
	}
	second.EndRun()
}

// A wait the caller gives up on is still closed: the surface that was told the
// session is waiting has to be told it no longer is.
func TestAbandonedWaitIsClosedToo(t *testing.T) {
	withWaitGrace(t, 20*time.Millisecond)
	root, locks := testenv.TempDir(t), testenv.TempDir(t)
	first, _ := New(root, locks, nil)
	var log waitLog
	second, _ := New(root, locks, log.note)
	first.BeginRun()
	second.BeginRun()
	defer first.EndRun()
	defer second.EndRun()
	if err := first.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := second.AcquireWrite(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second owner acquired while first held: %v", err)
	}
	if !log.sameAs(WaitBegan, WaitAbandoned) {
		t.Fatalf("wait reports = %v, want one began closed by one abandoned", log.outcomes())
	}
}

func TestStateReportsWaitingAndAcquiredWithoutIdentity(t *testing.T) {
	withWaitGrace(t, 20*time.Millisecond)
	root, locks := testenv.TempDir(t), testenv.TempDir(t)
	first, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan struct{}, 1)
	second, err := New(root, locks, func(Wait) { waiting <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	first.BeginRun()
	second.BeginRun()
	if err := first.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := first.State(); !got.Acquired || got.Waiting {
		t.Fatalf("first owner state = %+v, want acquired and not waiting", got)
	}
	acquired := make(chan error, 1)
	go func() { acquired <- second.AcquireWrite(context.Background()) }()
	select {
	case <-waiting:
	case <-time.After(2 * time.Second):
		t.Fatal("second owner did not report waiting")
	}
	if got := second.State(); got.Acquired || !got.Waiting {
		t.Fatalf("second owner state = %+v, want waiting and not acquired", got)
	}
	first.EndRun()
	if err := <-acquired; err != nil {
		t.Fatal(err)
	}
	if got := second.State(); !got.Acquired || got.Waiting {
		t.Fatalf("second owner state after acquire = %+v, want acquired and not waiting", got)
	}
	second.EndRun()
}

func TestIndependentWorkspacesDoNotBlockEachOther(t *testing.T) {
	locks := testenv.TempDir(t)
	first, err := New(pathLeaseRoot(t), locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(pathLeaseRoot(t), locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.BeginRun()
	second.BeginRun()
	if err := first.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := second.AcquireWrite(ctx); err != nil {
		t.Fatalf("independent workspace was blocked: %v", err)
	}
	first.EndRun()
	second.EndRun()
}

func TestLeaseMetadataNeverDirtiesWorkspace(t *testing.T) {
	root, locks := testenv.TempDir(t), testenv.TempDir(t)
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	o, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	o.BeginRun()
	if err := o.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	o.EndRun()
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("workspace entries changed after lease: before=%d after=%d", len(before), len(after))
	}
}

func TestAcquireIsReentrantWithinOwner(t *testing.T) {
	o, err := New(testenv.TempDir(t), testenv.TempDir(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	o.BeginRun()
	if err := o.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := o.AcquireWrite(ctx); err != nil {
		t.Fatalf("re-entrant acquire failed: %v", err)
	}
	o.EndRun()
}

func TestCancelledWaitDoesNotLeakLocalLease(t *testing.T) {
	root, locks := testenv.TempDir(t), testenv.TempDir(t)
	first, _ := New(root, locks, nil)
	second, _ := New(root, locks, nil)
	third, _ := New(root, locks, nil)
	first.BeginRun()
	second.BeginRun()
	third.BeginRun()
	if err := first.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if err := second.AcquireWrite(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled acquire = %v, want deadline", err)
	}
	second.EndRun()
	first.EndRun()
	if err := third.AcquireWrite(context.Background()); err != nil {
		t.Fatalf("lease leaked after cancellation: %v", err)
	}
	third.EndRun()
}

func TestLeaseWaitsForLastRun(t *testing.T) {
	root, locks := testenv.TempDir(t), testenv.TempDir(t)
	first, _ := New(root, locks, nil)
	second, _ := New(root, locks, nil)
	first.BeginRun()
	first.BeginRun()
	second.BeginRun()
	if err := first.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.EndRun()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := second.AcquireWrite(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second acquired before final run ended: %v", err)
	}
	first.EndRun()
	if err := second.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	second.EndRun()
}

func TestCrossProcessLeaseBlocksAndCrashReleases(t *testing.T) {
	root, locks := testenv.TempDir(t), testenv.TempDir(t)
	ready := filepath.Join(testenv.TempDir(t), "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestWorkspaceLeaseHelperProcess$")
	cmd.Env = append(os.Environ(),
		"REASONIX_WORKSPACE_LEASE_HELPER=1",
		"REASONIX_WORKSPACE_LEASE_ROOT="+root,
		"REASONIX_WORKSPACE_LEASE_DIR="+locks,
		"REASONIX_WORKSPACE_LEASE_READY="+ready,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper process did not acquire lease")
		}
		time.Sleep(20 * time.Millisecond)
	}

	o, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	o.BeginRun()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	if err := o.AcquireWrite(ctx); !errors.Is(err, context.DeadlineExceeded) {
		cancel()
		t.Fatalf("cross-process acquire while helper lived = %v, want deadline", err)
	}
	cancel()

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = cmd.Process.Wait()
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := o.AcquireWrite(ctx); err != nil {
		t.Fatalf("OS lease did not release after helper crash: %v", err)
	}
	o.EndRun()
}

// A session waiting is told which one is writing, whether that one is in this
// process or another: "another session" alone leaves a person to guess.
func TestAWaitNamesTheSessionHoldingTheLease(t *testing.T) {
	withWaitGrace(t, 20*time.Millisecond)
	root, locks := testenv.TempDir(t), testenv.TempDir(t)
	first, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.SetHolder(func() string { return "tune the v2ray config" })
	var log waitLog
	second, err := New(root, locks, log.note)
	if err != nil {
		t.Fatal(err)
	}
	first.BeginRun()
	second.BeginRun()
	if err := first.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	acquired := make(chan error, 1)
	go func() { acquired <- second.AcquireWrite(context.Background()) }()
	time.Sleep(100 * time.Millisecond)
	first.EndRun()
	if err := <-acquired; err != nil {
		t.Fatal(err)
	}
	second.EndRun()
	log.mu.Lock()
	defer log.mu.Unlock()
	if len(log.seen) == 0 || log.seen[0].Holder != "tune the v2ray config" {
		t.Fatalf("wait reports = %+v, want the first one naming the holder", log.seen)
	}
	if _, err := os.Stat(first.holderPath()); !os.IsNotExist(err) {
		t.Fatalf("the holder note outlived the hold: %v", err)
	}
}

func TestACrossProcessWaitNamesTheHolder(t *testing.T) {
	withWaitGrace(t, 20*time.Millisecond)
	root, locks := testenv.TempDir(t), testenv.TempDir(t)
	ready := filepath.Join(testenv.TempDir(t), "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestWorkspaceLeaseHelperProcess$")
	cmd.Env = append(os.Environ(),
		"REASONIX_WORKSPACE_LEASE_HELPER=1",
		"REASONIX_WORKSPACE_LEASE_ROOT="+root,
		"REASONIX_WORKSPACE_LEASE_DIR="+locks,
		"REASONIX_WORKSPACE_LEASE_READY="+ready,
		"REASONIX_WORKSPACE_LEASE_HOLDER=the other window",
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper process did not acquire lease")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var log waitLog
	o, err := New(root, locks, log.note)
	if err != nil {
		t.Fatal(err)
	}
	o.BeginRun()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = o.AcquireWrite(ctx)
	log.mu.Lock()
	defer log.mu.Unlock()
	if len(log.seen) == 0 || log.seen[0].Holder != "the other window" {
		t.Fatalf("wait reports = %+v, want the first one naming the other process's session", log.seen)
	}
}
