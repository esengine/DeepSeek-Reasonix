//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"reasonix/internal/remote/protocol"
)

func TestInstallRejectsWritableReasonixHomeBoundary(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if err := os.Chmod(fixture.home, 0o777); err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	if _, err := fixture.manager.Install(context.Background()); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("Install error = %v, want ErrUnsafeArtifact", err)
	}
	if calls := fixture.runner.Calls(); len(calls) != 0 {
		t.Fatalf("unsafe Reasonix Home executed commands: %#v", calls)
	}
	if _, err := os.Lstat(fixture.manager.managedRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe Reasonix Home received managed artifacts, err=%v", err)
	}
}

func TestInstallRejectsIntermediateReasonixHomeSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real-parent")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked-parent")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(link, "reasonix-home")
	if err := os.Mkdir(filepath.Join(target, "reasonix-home"), 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "reasonix")
	if err := os.WriteFile(source, []byte("source"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	manager, err := newSystemd(Options{
		ReasonixHome:   home,
		UnitPath:       filepath.Join(root, "config", "reasonix-remote.service"),
		SocketPath:     filepath.Join(root, "runtime", "remote.sock"),
		ExecutablePath: source, CLIBuildID: lifecycleTestBuildID(t, 'a'), Runner: runner,
	}, currentUID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(context.Background()); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("Install error = %v, want ErrUnsafeArtifact", err)
	}
	if _, err := os.Lstat(filepath.Join(target, "reasonix-home", "remote")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink target received managed artifacts, err=%v", err)
	}
}

func TestMutationLockRejectsSymlinkedUnitParent(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	realParent := filepath.Join(fixture.root, "real-unit-parent")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(fixture.root, "linked-unit-parent")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	manager, err := newSystemd(Options{
		ReasonixHome: fixture.home,
		UnitPath:     filepath.Join(linkedParent, "reasonix-remote.service"),
		SocketPath:   fixture.socket, ExecutablePath: fixture.source,
		CLIBuildID: fixture.buildID, Runner: fixture.runner,
	}, currentUID())
	if err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	if _, err := manager.Install(context.Background()); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("Install error = %v, want ErrUnsafeArtifact", err)
	}
	if entries, err := os.ReadDir(realParent); err != nil || len(entries) != 0 {
		t.Fatalf("symlinked unit parent was modified: entries=%v err=%v", entries, err)
	}
}

func TestUninstallRejectsIntermediateBinSymlinkBeforeServiceMutation(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(fixture.root, "bin-target")
	if err := os.Rename(fixture.manager.managedDir, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, fixture.manager.managedDir); err != nil {
		t.Fatal(err)
	}
	wantBinary, err := os.ReadFile(filepath.Join(target, "reasonix"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	if _, err := fixture.manager.Uninstall(context.Background()); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("Uninstall error = %v, want ErrUnsafeArtifact", err)
	}
	if calls := fixture.runner.Calls(); len(calls) != 0 {
		t.Fatalf("unsafe bin path mutated service state: %#v", calls)
	}
	assertFileContents(t, filepath.Join(target, "reasonix"), wantBinary)
	if _, err := os.Stat(fixture.unit); err != nil {
		t.Fatalf("unit was removed before path rejection: %v", err)
	}
}

func TestDirFDRemovalRejectsBinRebindWithoutTouchingEitherTarget(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(fixture.manager.managedRoot, "bin-original")
	target := filepath.Join(fixture.root, "attacker-target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(target, "sentinel")
	if err := os.WriteFile(sentinel, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.manager.removeManagedArtifactsWithHook(func() {
		if renameErr := os.Rename(fixture.manager.managedDir, original); renameErr != nil {
			t.Errorf("rebind rename: %v", renameErr)
			return
		}
		if symlinkErr := os.Symlink(target, fixture.manager.managedDir); symlinkErr != nil {
			t.Errorf("rebind symlink: %v", symlinkErr)
		}
	})
	if !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("removal error = %v, want ErrUnsafeArtifact", err)
	}
	assertFileContents(t, sentinel, []byte("untouched"))
	if _, err := os.Stat(filepath.Join(original, "reasonix")); err != nil {
		t.Fatalf("original managed inode was modified: %v", err)
	}
}

func TestUnitRemovalRejectsReplacementAfterExactRead(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, err := fixture.manager.readUnit()
	if err != nil {
		t.Fatal(err)
	}
	original := fixture.unit + ".original"
	if err := os.Rename(fixture.unit, original); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.unit, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.prepareUnitRemoval(record.Identity); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("prepareUnitRemoval error = %v, want ErrUnsafeArtifact", err)
	}
	assertFileContents(t, fixture.unit, []byte("replacement"))
	if _, err := os.Stat(original); err != nil {
		t.Fatalf("validated original unit was modified: %v", err)
	}
}

func TestUnitRemovalRejectsParentRebindBeforeUnlink(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, err := fixture.manager.readUnit()
	if err != nil {
		t.Fatal(err)
	}
	removal, err := fixture.manager.prepareUnitRemoval(record.Identity)
	if err != nil {
		t.Fatal(err)
	}
	defer removal.Close()
	parent := filepath.Dir(fixture.unit)
	originalParent := parent + ".original"
	replacement := []byte("replacement-unit")
	removal.beforeUnlink = func() {
		if err := os.Rename(parent, originalParent); err != nil {
			t.Errorf("rename unit parent: %v", err)
			return
		}
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Errorf("create replacement unit parent: %v", err)
			return
		}
		if err := os.WriteFile(fixture.unit, replacement, 0o600); err != nil {
			t.Errorf("write replacement unit: %v", err)
		}
	}
	if err := removal.Remove(); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("unit removal error = %v, want ErrUnsafeArtifact", err)
	}
	assertFileContents(t, fixture.unit, replacement)
	if _, err := os.Stat(filepath.Join(originalParent, filepath.Base(fixture.unit))); err != nil {
		t.Fatalf("original unit was removed: %v", err)
	}
}

func TestManagedRemovalWithoutBinRemovesOnlyEmptyRoot(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "unknown-content"}[unknown], func(t *testing.T) {
			fixture := newManagerFixture(t, 'a')
			if err := os.Mkdir(fixture.manager.managedRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			unknownPath := filepath.Join(fixture.manager.managedRoot, "operator-owned")
			if unknown {
				if err := os.WriteFile(unknownPath, []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := fixture.manager.removeManagedArtifacts()
			if err != nil {
				t.Fatal(err)
			}
			if unknown {
				if changed {
					t.Fatal("unknown-only managed root reported destructive change")
				}
				assertFileContents(t, unknownPath, []byte("keep"))
			} else {
				if !changed {
					t.Fatal("empty managed root was not removed")
				}
				if _, err := os.Lstat(fixture.manager.managedRoot); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("empty managed root still exists: %v", err)
				}
			}
		})
	}
}

func TestMutationLockHonorsContextAndReadOnlyCommandsDoNotBlock(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.manager.probe = testProbe{result: DaemonProbeResult{BuildID: fixture.buildID}}
	lock, err := fixture.manager.acquireMutationLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	fixture.runner.Reset()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := fixture.manager.Restart(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked Restart error = %v, want deadline", err)
	}
	if calls := fixture.runner.Calls(); len(calls) != 0 {
		t.Fatalf("blocked mutation executed commands: %#v", calls)
	}

	readCtx, readCancel := context.WithTimeout(context.Background(), time.Second)
	defer readCancel()
	if _, err := fixture.manager.Status(readCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Doctor(readCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Logs(readCtx, LogsOptions{Lines: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestMutationLockIs0600UnderRestrictiveUmask(t *testing.T) {
	old := unix.Umask(0o077)
	defer unix.Umask(old)
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertMode(t, fixture.manager.lockPath, 0o600)
}

func TestSharedUnitLockSerializesDifferentHomesAndBuilds(t *testing.T) {
	root := t.TempDir()
	unit := filepath.Join(root, "config", "reasonix-remote.service")
	socket := filepath.Join(root, "runtime", "remote.sock")
	newManager := func(label string, revision byte) *SystemdManager {
		home := filepath.Join(root, "home-"+label)
		if err := os.Mkdir(home, 0o700); err != nil {
			t.Fatal(err)
		}
		source := filepath.Join(root, "reasonix-"+label)
		if err := os.WriteFile(source, []byte("binary-"+label), 0o700); err != nil {
			t.Fatal(err)
		}
		manager, err := newSystemd(Options{
			ReasonixHome: home, UnitPath: unit, SocketPath: socket, ExecutablePath: source,
			CLIBuildID: lifecycleTestBuildID(t, revision),
		}, currentUID())
		if err != nil {
			t.Fatal(err)
		}
		return manager
	}
	managerA := newManager("a", 'a')
	managerB := newManager("b", 'b')
	runner := &fakeRunner{}
	managerA.runner = runner
	managerB.runner = runner
	entered := make(chan struct{})
	release := make(chan struct{})
	var blockOnce sync.Once
	runner.handler = func(name string, args []string) (CommandOutput, error) {
		if name == "systemctl" && reflect.DeepEqual(args, []string{"--user", "daemon-reload"}) {
			blockOnce.Do(func() {
				close(entered)
				<-release
			})
		}
		if name == "systemctl" && containsArg(args, "--property=LoadState") {
			return CommandOutput{Stdout: loadedUnitOutput(managerA, "inactive")}, nil
		}
		return CommandOutput{}, nil
	}

	resultA := make(chan error, 1)
	resultB := make(chan error, 1)
	go func() { _, err := managerA.Install(context.Background()); resultA <- err }()
	<-entered
	go func() { _, err := managerB.Install(context.Background()); resultB <- err }()
	select {
	case err := <-resultB:
		t.Fatalf("second profile escaped shared mutation lock early: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	close(release)
	if err := <-resultA; err != nil {
		t.Fatal(err)
	}
	if err := <-resultB; !errors.Is(err, ErrProfileMismatch) {
		t.Fatalf("second profile error = %v, want ErrProfileMismatch", err)
	}
	identity, err := managerA.inspectInstalledIdentity()
	if err != nil || identity.BuildID == nil || protocol.CompareBuildID(managerA.cliBuildID, *identity.BuildID) != nil {
		t.Fatalf("final installed identity = %+v, err=%v", identity, err)
	}
	if _, err := os.Lstat(managerB.managedRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("losing profile wrote managed artifacts, err=%v", err)
	}
	assertMode(t, managerA.lockPath, 0o600)
	record, err := managerA.readUnit()
	if err != nil || !sameInstallProfile(record.Profile, managerA.profile) {
		t.Fatalf("final unit profile = %+v, err=%v", record.Profile, err)
	}
	enableCalls := 0
	for _, call := range runner.Calls() {
		if call.Name == "systemctl" && containsArg(call.Args, "enable") {
			enableCalls++
		}
	}
	if enableCalls != 1 {
		t.Fatalf("enable calls = %d, want one committed build: %#v", enableCalls, runner.Calls())
	}
	lock, err := managerA.acquireMutationLock(context.Background())
	if err != nil {
		t.Fatalf("profile mismatch path leaked shared lock: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenedExecutableInodeCannotDriftToReplacementPath(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	originalBytes, err := os.ReadFile(fixture.source)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := os.Open(fixture.source)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	fixture.manager.sourceOpener = func() (*os.File, os.FileInfo, error) {
		fd, err := unix.Dup(int(opened.Fd()))
		if err != nil {
			return nil, nil, err
		}
		file := os.NewFile(uintptr(fd), "opened-current-inode")
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, nil, err
		}
		return file, info, nil
	}
	oldPath := fixture.source + ".old"
	if err := os.Rename(fixture.source, oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.source, []byte("replacement-path-binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, fixture.manager.binaryPath, originalBytes)
}

func TestProductionSourceOpenerBindsRunningProcExecutableInode(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	manager, err := newSystemd(Options{
		ReasonixHome: home, UnitPath: filepath.Join(root, "unit", "reasonix-remote.service"),
		SocketPath: filepath.Join(root, "runtime", "remote.sock"),
		CLIBuildID: lifecycleTestBuildID(t, 'a'), Runner: &fakeRunner{},
	}, currentUID())
	if err != nil {
		t.Fatal(err)
	}
	if manager.executable != "" {
		t.Fatalf("production manager retained pathname source %q", manager.executable)
	}
	file, info, err := manager.openExecutableSource()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	procInfo, err := os.Stat("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(info, procInfo) {
		t.Fatal("production source is not the running /proc/self/exe inode")
	}
}
