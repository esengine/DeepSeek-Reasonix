package sessiontemp

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestAcquireSharesGeneration(t *testing.T) {
	m := newForTest(t.TempDir())
	m.Retain()
	defer m.Release()

	a, err := m.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if a.Dir() == "" || a.Dir() != b.Dir() {
		t.Fatalf("dirs = %q, %q; want same non-empty dir", a.Dir(), b.Dir())
	}
	info, err := os.Stat(a.Dir())
	if err != nil || !info.IsDir() {
		t.Fatalf("dir stat: %v", err)
	}
	// Windows does not expose POSIX directory permission bits. The
	// cross-platform contract is that the manager creates a private directory;
	// the exact 0700 mode is meaningful only on Unix-like systems.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("dir perm = %o, want 0700", perm)
		}
	}
	a.Release()
	b.Release()
	if _, err := os.Stat(a.Dir()); err != nil {
		t.Fatalf("active generation should remain while manager owned: %v", err)
	}
}

func TestRotateIsolatesNewCommands(t *testing.T) {
	m := newForTest(t.TempDir())
	m.Retain()
	defer m.Release()

	old, err := m.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	oldDir := old.Dir()
	if err := os.WriteFile(filepath.Join(oldDir, "keep.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	m.Rotate()
	fresh, err := m.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Dir() == oldDir {
		t.Fatal("rotate should create a new directory")
	}
	if _, err := os.Stat(filepath.Join(fresh.Dir(), "keep.txt")); !os.IsNotExist(err) {
		t.Fatalf("new generation must not see old files: %v", err)
	}
	// Old generation remains while leased.
	if _, err := os.Stat(filepath.Join(oldDir, "keep.txt")); err != nil {
		t.Fatalf("leased old generation deleted early: %v", err)
	}
	old.Release()
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old generation should be removed after last lease: %v", err)
	}
	fresh.Release()
}

func TestLastLeaseDeletesRetiredGeneration(t *testing.T) {
	m := newForTest(t.TempDir())
	m.Retain()

	lease, err := m.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	dir := lease.Dir()
	m.Release() // last controller owner retires current generation
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("retired generation with live lease must remain: %v", err)
	}
	lease.Release()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory should be deleted after last lease: %v", err)
	}
}

func TestHotRebuildRetainRelease(t *testing.T) {
	m := newForTest(t.TempDir())
	m.Retain() // old controller
	lease, err := m.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	dir := lease.Dir()
	lease.Release()

	m.Retain()  // replacement controller
	m.Release() // old controller closes — must not delete while new owns
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("hot rebuild must keep generation: %v", err)
	}

	again, err := m.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if again.Dir() != dir {
		t.Fatalf("hot rebuild should reuse generation: got %q want %q", again.Dir(), dir)
	}
	again.Release()
	m.Release()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("final release should delete: %v", err)
	}
}

func TestAcquireCreateFailureDoesNotFallback(t *testing.T) {
	m := newForTest(t.TempDir())
	m.Retain()
	defer m.Release()
	m.mkDir = func(string) (string, error) {
		return "", os.ErrPermission
	}
	if _, err := m.Acquire(); err == nil {
		t.Fatal("want create failure")
	}
}

func TestAcquireAfterLastOwnerReleaseIsSealed(t *testing.T) {
	m := newForTest(t.TempDir())
	m.Retain()
	lease, err := m.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	dir := lease.Dir()

	// Force Release → delayed Acquire ordering with a channel barrier.
	released := make(chan struct{})
	acquired := make(chan error, 1)
	go func() {
		<-released
		_, err := m.Acquire()
		acquired <- err
	}()

	m.Release() // last owner — seals
	close(released)
	err = <-acquired
	if err == nil {
		t.Fatal("Acquire after last Release must fail closed")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if !m.Sealed() {
		t.Fatal("manager should be sealed")
	}

	// Live lease still pins the directory until it releases.
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("leased generation deleted while sealed: %v", err)
	}
	lease.Release()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("generation should delete after last lease on sealed manager: %v", err)
	}

	// Retain after seal must not reopen.
	m.Retain()
	if _, err := m.Acquire(); err == nil {
		t.Fatal("Retain after seal must not reopen Acquire")
	}
}

func TestAcquireWithoutOwnerFailsClosed(t *testing.T) {
	m := newForTest(t.TempDir())
	if _, err := m.Acquire(); err == nil {
		t.Fatal("Acquire with zero owners must fail")
	}
}

func TestProcessCleanupRunsOncePerRoot(t *testing.T) {
	resetProcessCleanupForTest()
	root := t.TempDir()
	// Plant a stale dir that would be eligible if cleanup ran with an old now.
	// We only count whether cleanupStaleOnce marks the root done.
	cleanupStaleOnce(root, time.Now)
	cleanupStaleOnce(root, time.Now)
	processCleanup.Lock()
	key := canonicalTempRoot(root)
	_, ok := processCleanup.done[key]
	n := len(processCleanup.done)
	processCleanup.Unlock()
	if !ok {
		t.Fatal("root not marked cleaned")
	}
	if n != 1 {
		t.Fatalf("cleanup map size = %d, want 1 entry for one root", n)
	}
	// A second New against the real TempDir should not panic; first process
	// New still uses process-level once.
	_ = New()
	_ = New()
}

func TestConcurrentAcquireRotateReleaseRace(t *testing.T) {
	m := newForTest(t.TempDir())
	m.Retain()
	defer m.Release()

	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for j := range 50 {
				lease, err := m.Acquire()
				if err != nil {
					t.Errorf("acquire: %v", err)
					return
				}
				_ = os.WriteFile(filepath.Join(lease.Dir(), "x"), []byte("1"), 0o600)
				if j%7 == 0 {
					m.Rotate()
				}
				lease.Release()
			}
		})
	}
	wg.Wait()
}

func TestStaleCleanup(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	// Fresh dir — skip.
	fresh, err := os.MkdirTemp(root, dirPrefix)
	if err != nil {
		t.Fatal(err)
	}

	// Active locked dir older than 24h — skip.
	active, err := os.MkdirTemp(root, dirPrefix)
	if err != nil {
		t.Fatal(err)
	}
	release, err := filelockAcquire(filepath.Join(active, ownerLockName))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	// Chtimes after lock creation: writing the lock file refreshes dir mtime.
	if err := os.Chtimes(active, now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Stale unlocked dir — delete.
	stale, err := os.MkdirTemp(root, dirPrefix)
	if err != nil {
		t.Fatal(err)
	}
	// Create and release lock file so TryAcquire can succeed, then age the dir.
	lockPath := filepath.Join(stale, ownerLockName)
	r, err := filelockAcquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	r()
	if err := os.Chtimes(stale, now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Unrelated directory — skip.
	other := filepath.Join(root, "not-reasonix")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(other, now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Symlink to a foreign target — remove only the symlink entry, not the target.
	foreign := filepath.Join(t.TempDir(), "foreign-target")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(foreign, "marker")
	if err := os.WriteFile(marker, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, dirPrefix+"link")
	if err := os.Symlink(foreign, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(link, now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
		// Some platforms cannot chtimes symlinks; fall back to cleaning with a
		// forced old now so age check uses Lstat mtime of the link when set.
		_ = err
	}

	cleanupStale(root, func() time.Time { return now })

	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh dir removed: %v", err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatalf("active locked dir removed: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale dir should be removed: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("unrelated dir removed: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("symlink cleanup deleted foreign target: %v", err)
	}
}

func filelockAcquire(path string) (func(), error) {
	// Local import shim for tests in this package.
	return tryLockForTest(path)
}
