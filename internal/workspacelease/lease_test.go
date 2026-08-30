package workspacelease

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

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
	o.BeginRun()
	if err := o.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
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
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "workspace-link")
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
	repo := t.TempDir()
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
	parent := t.TempDir()
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

func TestWorkspaceIdentityHelpersPreserveCanonicalRoot(t *testing.T) {
	owner, err := New(t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ancestors := ancestorDirectories(owner.canonical)
	if len(ancestors) == 0 || ancestors[len(ancestors)-1] != owner.canonical {
		t.Fatalf("ancestor chain = %q, want canonical root %q last", ancestors, owner.canonical)
	}
	if got := workspaceLockPath(owner.lockDir, owner.compatibility); got != owner.lockPath {
		t.Fatalf("compatibility root lock = %q, want owner lock %q", got, owner.lockPath)
	}
	chain := pathChain(owner.canonical, filepath.Join(owner.canonical, "nested", "file.go"))
	if len(chain) == 0 || chain[0] != owner.canonical {
		t.Fatalf("path chain = %q, want canonical root %q first", chain, owner.canonical)
	}
}

func TestRepositoryRootAndSubdirectoryOwnersSerialize(t *testing.T) {
	repo, locks := t.TempDir(), t.TempDir()
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

func TestOwnersSerializeSameWorkspaceAndNotifyOnce(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	first, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	var notices atomic.Int32
	second, err := New(root, locks, func() { notices.Add(1) })
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
	if got := notices.Load(); got != 1 {
		t.Fatalf("wait notices = %d, want 1", got)
	}
	second.EndRun()
}

func TestStateReportsWaitingAndAcquiredWithoutIdentity(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	first, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan struct{}, 1)
	second, err := New(root, locks, func() { waiting <- struct{}{} })
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
	locks := t.TempDir()
	first, err := New(t.TempDir(), locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(t.TempDir(), locks, nil)
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
	root, locks := t.TempDir(), t.TempDir()
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
	o, err := New(t.TempDir(), t.TempDir(), nil)
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
	root, locks := t.TempDir(), t.TempDir()
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
	root, locks := t.TempDir(), t.TempDir()
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

func TestBackgroundRetentionOutlivesRun(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	first, _ := New(root, locks, nil)
	second, _ := New(root, locks, nil)
	first.BeginRun()
	second.BeginRun()
	if err := first.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	first.RetainUntil(done)
	first.EndRun()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	if err := second.AcquireWrite(ctx); !errors.Is(err, context.DeadlineExceeded) {
		cancel()
		t.Fatalf("second acquired while background job was running: %v", err)
	}
	cancel()
	close(done)
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := second.AcquireWrite(ctx); err != nil {
		t.Fatal(err)
	}
	second.EndRun()
}

func TestLeaseWaitsForEveryRetainedBackgroundJob(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	first, _ := New(root, locks, nil)
	second, _ := New(root, locks, nil)
	first.BeginRun()
	second.BeginRun()
	if err := first.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	one, two := make(chan struct{}), make(chan struct{})
	first.RetainUntil(one)
	first.RetainUntil(two)
	first.EndRun()
	close(one)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	if err := second.AcquireWrite(ctx); !errors.Is(err, context.DeadlineExceeded) {
		cancel()
		t.Fatalf("lease released before final background job: %v", err)
	}
	cancel()
	close(two)
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := second.AcquireWrite(ctx); err != nil {
		t.Fatal(err)
	}
	second.EndRun()
}

func TestNestedRepoPathWritesRunInParallel(t *testing.T) {
	parent, locks := t.TempDir(), t.TempDir()
	repoA := filepath.Join(parent, "A")
	repoB := filepath.Join(parent, "B")
	for _, repo := range []string{repoA, repoB} {
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	first, err := New(parent, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(parent, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.BeginRun()
	second.BeginRun()
	if err := first.AcquireWriteForPath(context.Background(), filepath.Join(repoA, "a.go")); err != nil {
		t.Fatal(err)
	}
	if err := second.AcquireWriteForPath(context.Background(), filepath.Join(repoB, "b.go")); err != nil {
		t.Fatal(err)
	}
	first.EndRun()
	second.EndRun()
}

func TestExclusiveWorkspaceWriteBlocksNestedRepoPath(t *testing.T) {
	parent, locks := t.TempDir(), t.TempDir()
	repoB := filepath.Join(parent, "B")
	if err := os.MkdirAll(filepath.Join(repoB, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := New(parent, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(parent, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.BeginRun()
	second.BeginRun()
	if err := first.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	if err := second.AcquireWriteForPath(ctx, filepath.Join(repoB, "b.go")); !errors.Is(err, context.DeadlineExceeded) {
		cancel()
		t.Fatalf("nested path write acquired under exclusive workspace: %v", err)
	}
	cancel()
	first.EndRun()
	if err := second.AcquireWriteForPath(context.Background(), filepath.Join(repoB, "b.go")); err != nil {
		t.Fatal(err)
	}
	second.EndRun()
}

func TestSameRepoDifferentFilesRunInParallel(t *testing.T) {
	repo, locks := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := New(repo, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(repo, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.BeginRun()
	second.BeginRun()
	if err := first.AcquireWriteForPath(context.Background(), filepath.Join(repo, "a.go")); err != nil {
		t.Fatal(err)
	}
	if err := second.AcquireWriteForPath(context.Background(), filepath.Join(repo, "b.go")); err != nil {
		t.Fatal(err)
	}
	first.EndRun()
	second.EndRun()
}

func TestSameFilePathWritesStillSerialize(t *testing.T) {
	repo, locks := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := New(repo, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(repo, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.BeginRun()
	second.BeginRun()
	path := filepath.Join(repo, "a.go")
	if err := first.AcquireWriteForPath(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	if err := second.AcquireWriteForPath(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		cancel()
		t.Fatalf("same file must still serialize: %v", err)
	}
	cancel()
	first.EndRun()
	if err := second.AcquireWriteForPath(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	second.EndRun()
}

func TestHoldWriteReleasesBeforeEndRun(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	first, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.BeginRun()
	second.BeginRun()
	release, err := first.HoldWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	if err := second.AcquireWrite(ctx); !errors.Is(err, context.DeadlineExceeded) {
		cancel()
		t.Fatalf("held write should block: %v", err)
	}
	cancel()
	release()
	if err := second.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.EndRun()
	second.EndRun()
}

func TestPathWriteUpgradeToExclusiveBlocksOtherRepo(t *testing.T) {
	parent, locks := t.TempDir(), t.TempDir()
	repoA := filepath.Join(parent, "A")
	repoB := filepath.Join(parent, "B")
	for _, repo := range []string{repoA, repoB} {
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	first, err := New(parent, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(parent, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.BeginRun()
	second.BeginRun()
	releasePath, err := first.HoldWriteForPath(context.Background(), filepath.Join(repoA, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	upgraded := make(chan error, 1)
	go func() {
		release, upgradeErr := first.HoldWrite(context.Background())
		if upgradeErr == nil {
			release()
		}
		upgraded <- upgradeErr
	}()
	waitForOwnerAcquisition(t, first)
	releasePath()
	if err := <-upgraded; err != nil {
		t.Fatal(err)
	}
	release, err := first.HoldWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	if err := second.AcquireWriteForPath(ctx, filepath.Join(repoB, "b.go")); !errors.Is(err, context.DeadlineExceeded) {
		cancel()
		t.Fatalf("upgrade to exclusive should block other repo: %v", err)
	}
	cancel()
	release()
	first.EndRun()
	if err := second.AcquireWriteForPath(context.Background(), filepath.Join(repoB, "b.go")); err != nil {
		t.Fatal(err)
	}
	second.EndRun()
}

func TestRetainWithoutWriteDoesNotBlockReaders(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	reader, _ := New(root, locks, nil)
	writer, _ := New(root, locks, nil)
	reader.BeginRun()
	done := make(chan struct{})
	reader.RetainUntil(done)
	reader.EndRun()
	writer.BeginRun()
	if err := writer.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	writer.EndRun()
	close(done)
}

func TestCrossProcessLeaseBlocksAndCrashReleases(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	ready := filepath.Join(t.TempDir(), "ready")
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
