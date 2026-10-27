package filelock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
)

func lockRoot(t *testing.T) (string, *os.Root) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return dir, root
}

func TestRootedLocksShareTheLegacyOSOwner(t *testing.T) {
	for _, rootFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy_first", true: "rooted_first"}[rootFirst], func(t *testing.T) {
			dir, root := lockRoot(t)
			var release func()
			var err error
			if rootFirst {
				release, err = TryAcquireRoot(root, "owner.lock")
			} else {
				release, err = TryAcquire(filepath.Join(dir, "owner.lock"))
			}
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if other, err := TryAcquireRoot(root, "owner.lock"); !errors.Is(err, ErrHeld) {
				if other != nil {
					other()
				}
				t.Fatalf("rooted contender = %v, want ErrHeld", err)
			}
			if other, err := TryAcquire(filepath.Join(dir, "owner.lock")); !errors.Is(err, ErrHeld) {
				if other != nil {
					other()
				}
				t.Fatalf("legacy contender = %v, want ErrHeld", err)
			}
			other, err := TryAcquireRoot(root, "disjoint.lock")
			if err != nil {
				t.Fatalf("unrelated lock blocked: %v", err)
			}
			other()
			release()
			successor, err := TryAcquireRoot(root, "owner.lock")
			if err != nil {
				t.Fatal(err)
			}
			defer successor()
			release()
			if extra, err := TryAcquireRoot(root, "owner.lock"); !errors.Is(err, ErrHeld) {
				if extra != nil {
					extra()
				}
				t.Fatalf("old cleanup released the successor: %v", err)
			}
		})
	}
}

func TestRootedLockWaitCancellationPreservesOwner(t *testing.T) {
	dir, root := lockRoot(t)
	release, err := TryAcquireRoot(root, "owner.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			other, err := AcquireRoot(ctx, root, "owner.lock")
			if other != nil {
				other()
			}
			done <- err
		}()
		synctest.Wait()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled wait returned %v", err)
		}
	})
	if other, err := TryAcquire(filepath.Join(dir, "owner.lock")); !errors.Is(err, ErrHeld) {
		if other != nil {
			other()
		}
		t.Fatalf("wait cancellation released another owner: %v", err)
	}
	release()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	other, err := AcquireRoot(ctx, root, "owner.lock")
	if err != nil {
		t.Fatalf("context bounds waiting, not a free admission: %v", err)
	}
	other()
}

func TestRootedLockNamesAndEscapesAreRefused(t *testing.T) {
	dir, root := lockRoot(t)
	outside := t.TempDir()
	for _, name := range []string{"", ".", "..", "../escaped.lock", filepath.Join(outside, "absolute.lock")} {
		if release, err := TryAcquireRoot(root, name); err == nil {
			release()
			t.Errorf("invalid rooted name admitted: %q", name)
		}
	}
	for _, name := range []string{"absolute.lock", "escaped.lock"} {
		if _, err := os.Stat(filepath.Join(outside, name)); !os.IsNotExist(err) {
			t.Fatalf("invalid name created outside file: %v", err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "directory.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if release, err := TryAcquireRoot(root, "directory.lock"); err == nil {
		release()
		t.Fatal("directory accepted as a lock file")
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if release, err := AcquireRoot(t.Context(), root, "closed.lock"); err == nil {
		release()
		t.Fatal("closed root accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "closed.lock")); !os.IsNotExist(err) {
		t.Fatalf("closed root created a file: %v", err)
	}
}

func TestRootedLockDoesNotFollowOutsideSymlinks(t *testing.T) {
	dir, root := lockRoot(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "target.lock")
	if err := os.WriteFile(target, []byte("retain bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, target, lock string }{
		{"file", target, "file"},
		{"directory", outside, "directory/new.lock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.Symlink(tc.target, filepath.Join(dir, tc.name)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if release, err := TryAcquireRoot(root, tc.lock); err == nil {
				release()
				t.Fatal("outside symlink escaped the opened root")
			}
		})
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "retain bytes" {
		t.Fatalf("outside bytes changed: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.lock")); !os.IsNotExist(err) {
		t.Fatalf("outside link created a file: %v", err)
	}
}

func TestRootedLockFollowsTheOpenedDirectoryAfterRename(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "original")
	moved := filepath.Join(parent, "moved")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer parentRoot.Close()
	// Match resultLockRoot: a relative child handle permits Windows rename.
	root, err := parentRoot.OpenRoot("original")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	release, err := TryAcquireRoot(root, "owner.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := os.Stat(filepath.Join(moved, "owner.lock")); err != nil {
		t.Fatalf("opened directory lost its identity: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "owner.lock")); !os.IsNotExist(err) {
		t.Fatalf("reopened replacement pathname: %v", err)
	}
}

func TestRootedLockSurvivesClosingTheDirectory(t *testing.T) {
	dir, root := lockRoot(t)
	release, err := TryAcquireRoot(root, "owner.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if other, err := TryAcquire(filepath.Join(dir, "owner.lock")); !errors.Is(err, ErrHeld) {
		if other != nil {
			other()
		}
		t.Fatalf("directory close released the file lease: %v", err)
	}
	release()
	other, err := TryAcquire(filepath.Join(dir, "owner.lock"))
	if err != nil {
		t.Fatal(err)
	}
	other()
}

func TestRootedLockCoordinatesHardlinkAliases(t *testing.T) {
	dir, root := lockRoot(t)
	release, err := TryAcquireRoot(root, "owner.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := os.Link(filepath.Join(dir, "owner.lock"), filepath.Join(dir, "alias.lock")); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	if other, err := TryAcquireRoot(root, "alias.lock"); !errors.Is(err, ErrHeld) {
		if other != nil {
			other()
		}
		t.Fatalf("hardlink alias bypassed the inode lock: %v", err)
	}
}

func TestRootedLockPreservesExistingBytesAndMode(t *testing.T) {
	dir, root := lockRoot(t)
	path := filepath.Join(dir, "owner.lock")
	if err := os.WriteFile(path, []byte("existing lock bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	release, err := AcquireRoot(t.Context(), root, "owner.lock")
	if err != nil {
		t.Fatal(err)
	}
	release()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "existing lock bytes" {
		t.Fatalf("lock changed file content: %q %v", data, err)
	}
	after, err := os.Stat(path)
	if err != nil || after.Mode() != before.Mode() {
		t.Fatalf("lock changed file mode: %v", err)
	}
}

func TestRootedLockErrorsKeepTheirIdentity(t *testing.T) {
	_, root := lockRoot(t)
	if _, err := TryAcquireRoot(nil, "owner.lock"); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("missing root error = %v", err)
	}
	if _, err := TryAcquireRoot(root, "../owner.lock"); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("nonlocal name error = %v", err)
	}
	if _, err := AcquireRoot(t.Context(), root, "missing/owner.lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing directory error = %v", err)
	}
}
