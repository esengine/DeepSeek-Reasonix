package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/service"
)

type runnerCall struct {
	Name string
	Args []string
}

type fakeRunner struct {
	mu      sync.Mutex
	calls   []runnerCall
	handler func(string, []string) (CommandOutput, error)
}

func (runner *fakeRunner) Run(ctx context.Context, name string, args ...string) (CommandOutput, error) {
	if err := ctx.Err(); err != nil {
		return CommandOutput{}, err
	}
	runner.mu.Lock()
	runner.calls = append(runner.calls, runnerCall{Name: name, Args: append([]string(nil), args...)})
	handler := runner.handler
	runner.mu.Unlock()
	if handler != nil {
		return handler(name, args)
	}
	return CommandOutput{}, nil
}

func (runner *fakeRunner) Calls() []runnerCall {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	calls := make([]runnerCall, len(runner.calls))
	for index := range runner.calls {
		calls[index] = runnerCall{Name: runner.calls[index].Name, Args: append([]string(nil), runner.calls[index].Args...)}
	}
	return calls
}

func (runner *fakeRunner) Reset() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.calls = nil
}

type testProbe struct {
	result DaemonProbeResult
	err    error
}

func (probe testProbe) Probe(context.Context) (DaemonProbeResult, error) {
	return probe.result, probe.err
}

type managerFixture struct {
	root    string
	home    string
	unit    string
	socket  string
	source  string
	manager *SystemdManager
	runner  *fakeRunner
	buildID protocol.BuildID
}

func newManagerFixture(t *testing.T, revision byte) *managerFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "reasonix-home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "current-reasonix")
	if err := os.WriteFile(source, []byte("reasonix-current-"+string(revision)), 0o755); err != nil {
		t.Fatal(err)
	}
	buildID := lifecycleTestBuildID(t, revision)
	runner := &fakeRunner{}
	manager, err := newSystemd(Options{
		ReasonixHome:   home,
		UnitPath:       filepath.Join(root, "config", "systemd", "user", service.UnitName),
		SocketPath:     filepath.Join(root, "runtime", service.SocketDirName, service.SocketFileName),
		ExecutablePath: source,
		CLIBuildID:     buildID,
		Runner:         runner,
	}, currentUID())
	if err != nil {
		t.Fatal(err)
	}
	runner.handler = statusHandler(manager, "inactive", true)
	return &managerFixture{
		root: root, home: home, unit: manager.unitPath, socket: manager.socketPath,
		source: source, manager: manager, runner: runner, buildID: buildID,
	}
}

func lifecycleTestBuildID(t *testing.T, revision byte) protocol.BuildID {
	t.Helper()
	id, err := protocol.NewBuildID("v-lifecycle-test", strings.Repeat(string(revision), 40))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestInstallWritesVerifiedManagedBinaryAndExactUnitBeforeStarting(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	result, err := fixture.manager.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("Install did not report a change")
	}
	wantCalls := []runnerCall{
		{Name: "systemctl", Args: []string{"--user", "daemon-reload"}},
		systemdShowCall(),
		{Name: "systemctl", Args: []string{"--user", "enable", "--now", service.UnitName}},
	}
	if got := fixture.runner.Calls(); !reflect.DeepEqual(got, wantCalls) {
		t.Fatalf("command order = %#v, want %#v", got, wantCalls)
	}

	source, err := os.ReadFile(fixture.source)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := os.ReadFile(fixture.manager.binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(managed, source) {
		t.Fatalf("managed binary = %q, want %q", managed, source)
	}
	assertMode(t, fixture.manager.managedRoot, 0o700)
	assertMode(t, fixture.manager.managedDir, 0o700)
	assertMode(t, fixture.manager.binaryPath, 0o700)
	assertMode(t, fixture.manager.manifestPath, 0o600)
	assertMode(t, fixture.unit, 0o600)
	identity, err := fixture.manager.inspectInstalledIdentity()
	if err != nil || !identity.Valid || identity.BuildID == nil {
		t.Fatalf("installed identity = %+v, err = %v", identity, err)
	}
	if err := protocol.CompareBuildID(fixture.buildID, *identity.BuildID); err != nil {
		t.Fatal(err)
	}

	unit, err := os.ReadFile(fixture.unit)
	if err != nil {
		t.Fatal(err)
	}
	binaryArgument, err := systemdQuote(fixture.manager.binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	homeEnvironment, err := systemdEnvironmentQuote("REASONIX_HOME=" + fixture.home)
	if err != nil {
		t.Fatal(err)
	}
	text := string(unit)
	for _, exact := range []string{
		"ExecStart=" + binaryArgument + " remote serve",
		"Environment=" + homeEnvironment,
		"Restart=on-failure",
		"UMask=0077",
		unitProfilePrefix,
	} {
		if !strings.Contains(text, exact) {
			t.Fatalf("unit does not contain %q:\n%s", exact, text)
		}
	}
	for _, forbidden := range []string{"/usr/bin/env", "sh -c", "bash -c", "PATH="} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unit contains forbidden resolution %q:\n%s", forbidden, text)
		}
	}
	entries, err := os.ReadDir(fixture.manager.managedDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := entryNames(entries), []string{"reasonix", ManifestName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("managed directory entries = %v, want %v", got, want)
	}
}

func TestInstallDoesNotEnableWhenDaemonReloadFails(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	boom := errors.New("daemon reload failed")
	fixture.runner.handler = func(name string, args []string) (CommandOutput, error) {
		if name == "systemctl" && reflect.DeepEqual(args, []string{"--user", "daemon-reload"}) {
			return CommandOutput{Stderr: "reload failed", ExitCode: 1}, boom
		}
		return CommandOutput{}, nil
	}
	if _, err := fixture.manager.Install(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("Install error = %v, want %v", err, boom)
	}
	wantCalls := []runnerCall{{Name: "systemctl", Args: []string{"--user", "daemon-reload"}}}
	if got := fixture.runner.Calls(); !reflect.DeepEqual(got, wantCalls) {
		t.Fatalf("commands after daemon-reload failure = %#v", got)
	}
	if identity, err := fixture.manager.inspectInstalledIdentity(); err != nil || !identity.Valid {
		t.Fatalf("prepared installed identity = %+v, err = %v", identity, err)
	}
	if _, err := fixture.manager.readUnit(); err != nil {
		t.Fatalf("prepared unit is invalid: %v", err)
	}
}

func TestInstallReportsRunningDaemonBuildMismatchWithoutRestartingIt(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	fixture.manager.probe = testProbe{result: DaemonProbeResult{BuildID: lifecycleTestBuildID(t, 'b')}}
	result, err := fixture.manager.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(result.Diagnostics, "active_daemon_requires_restart") {
		t.Fatalf("Install diagnostics = %+v", result.Diagnostics)
	}
	for _, call := range fixture.runner.Calls() {
		if containsArg(call.Args, "restart") {
			t.Fatalf("Install restarted an existing daemon: %+v", call)
		}
	}
}

func TestInstallCopiesResolvedExecutableButCreatesNoManagedSymlink(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	realSource := fixture.source
	linkSource := filepath.Join(fixture.root, "reasonix-symlink")
	if err := os.Symlink(realSource, linkSource); err != nil {
		t.Fatal(err)
	}
	manager, err := newSystemd(Options{
		ReasonixHome: fixture.home, UnitPath: fixture.unit, SocketPath: fixture.socket,
		ExecutablePath: linkSource, CLIBuildID: fixture.buildID, Runner: fixture.runner,
	}, currentUID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(manager.binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("managed binary mode = %v", info.Mode())
	}
	want, err := os.ReadFile(realSource)
	if err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, manager.binaryPath, want)
}

func TestMutatingCommandsRefuseReasonixHomeProfileSwitch(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()

	otherHome := filepath.Join(fixture.root, "different-home")
	if err := os.Mkdir(otherHome, 0o700); err != nil {
		t.Fatal(err)
	}
	other, err := newSystemd(Options{
		ReasonixHome: otherHome, UnitPath: fixture.unit, SocketPath: fixture.socket,
		ExecutablePath: fixture.source, CLIBuildID: fixture.buildID, Runner: fixture.runner,
	}, currentUID())
	if err != nil {
		t.Fatal(err)
	}
	commands := map[string]func() error{
		"install":   func() error { _, err := other.Install(context.Background()); return err },
		"start":     func() error { _, err := other.Start(context.Background()); return err },
		"stop":      func() error { _, err := other.Stop(context.Background()); return err },
		"restart":   func() error { _, err := other.Restart(context.Background()); return err },
		"uninstall": func() error { _, err := other.Uninstall(context.Background()); return err },
		"logs":      func() error { _, err := other.Logs(context.Background(), LogsOptions{Lines: 10}); return err },
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			before, err := treeDigest(fixture.root)
			if err != nil {
				t.Fatal(err)
			}
			if err := command(); !errors.Is(err, ErrProfileMismatch) {
				t.Fatalf("error = %v, want ErrProfileMismatch", err)
			}
			after, err := treeDigest(fixture.root)
			if err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatalf("%s changed files despite profile mismatch", name)
			}
		})
	}
	if calls := fixture.runner.Calls(); len(calls) != 0 {
		t.Fatalf("profile mismatch executed commands: %#v", calls)
	}
	if _, err := os.Stat(filepath.Join(otherHome, "remote")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("other profile managed directory exists, err = %v", err)
	}
	status, err := other.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(status.Diagnostics, "install_profile_mismatch") {
		t.Fatalf("Status did not report profile mismatch: %+v", status.Diagnostics)
	}
}

func TestStartActiveOnlyDiagnosesVersionMismatch(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := treeDigest(fixture.home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.source, []byte("new-cli-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	newBuild := lifecycleTestBuildID(t, 'b')
	manager, err := newSystemd(Options{
		ReasonixHome: fixture.home, UnitPath: fixture.unit, SocketPath: fixture.socket,
		ExecutablePath: fixture.source, CLIBuildID: newBuild, Runner: fixture.runner,
	}, currentUID())
	if err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	fixture.runner.handler = activeStateHandler(manager, "active")
	result, err := manager.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || !hasDiagnostic(result.Diagnostics, "active_daemon_requires_restart") {
		t.Fatalf("Start result = %+v", result)
	}
	after, err := treeDigest(fixture.home)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("active Start modified managed files")
	}
	want := []runnerCall{systemdShowCall()}
	if got := fixture.runner.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("active Start commands = %#v, want %#v", got, want)
	}
}

func TestStartInactiveSynchronizesThenStarts(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	newContents := []byte("new-cli-binary")
	if err := os.WriteFile(fixture.source, newContents, 0o755); err != nil {
		t.Fatal(err)
	}
	newBuild := lifecycleTestBuildID(t, 'b')
	manager, err := newSystemd(Options{
		ReasonixHome: fixture.home, UnitPath: fixture.unit, SocketPath: fixture.socket,
		ExecutablePath: fixture.source, CLIBuildID: newBuild, Runner: fixture.runner,
	}, currentUID())
	if err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	fixture.runner.handler = activeStateHandler(manager, "inactive")
	result, err := manager.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("inactive Start did not report a change")
	}
	want := []runnerCall{
		systemdShowCall(),
		{Name: "systemctl", Args: []string{"--user", "daemon-reload"}},
		systemdShowCall(),
		{Name: "systemctl", Args: []string{"--user", "start", service.UnitName}},
	}
	if got := fixture.runner.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("inactive Start commands = %#v, want %#v", got, want)
	}
	managed, err := os.ReadFile(manager.binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(managed, newContents) {
		t.Fatalf("managed binary = %q, want %q", managed, newContents)
	}
	identity, err := manager.inspectInstalledIdentity()
	if err != nil || identity.BuildID == nil || protocol.CompareBuildID(newBuild, *identity.BuildID) != nil {
		t.Fatalf("identity after Start = %+v, err = %v", identity, err)
	}
}

func TestStartActiveRefusesAlteredExecStartWithoutExecutingSystemd(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(fixture.unit)
	if err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), " remote serve", " remote serve --unexpected", 1))
	if err := os.WriteFile(fixture.unit, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	fixture.runner.handler = activeStateHandler(fixture.manager, "active")
	if _, err := fixture.manager.Start(context.Background()); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("Start error = %v, want ErrUnsafeArtifact", err)
	}
	if calls := fixture.runner.Calls(); len(calls) != 0 {
		t.Fatalf("altered ExecStart executed commands: %#v", calls)
	}
}

func TestStartRefusesMatchingProfileMarkerWithTamperedUnitContent(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(fixture.unit)
	if err != nil {
		t.Fatal(err)
	}
	contents = append(contents, []byte("ExecStartPre=/tmp/untrusted-helper\n")...)
	if err := os.WriteFile(fixture.unit, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	if _, err := fixture.manager.Start(context.Background()); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("Start error = %v, want ErrUnsafeArtifact", err)
	}
	if calls := fixture.runner.Calls(); len(calls) != 0 {
		t.Fatalf("tampered unit executed commands: %#v", calls)
	}
	fixture.runner.handler = statusHandler(fixture.manager, "inactive", false)
	status, err := fixture.manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Unit.ContentExact || !hasDiagnostic(status.Diagnostics, "unit_content_mismatch") {
		t.Fatalf("Status accepted tampered unit: %+v", status.Unit)
	}
}

func TestStartRefusesStaleLoadedExecStartWithoutSynchronizingBinary(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := treeDigest(fixture.home)
	if err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	fixture.runner.handler = func(name string, args []string) (CommandOutput, error) {
		if name == "systemctl" && containsArg(args, "--property=ActiveState") {
			return CommandOutput{Stdout: "ActiveState=active\nExecStart=/tmp/wrong-reasonix remote serve\n"}, nil
		}
		return CommandOutput{}, nil
	}
	if _, err := fixture.manager.Start(context.Background()); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("Start error = %v, want ErrUnsafeArtifact", err)
	}
	after, err := treeDigest(fixture.home)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("Start modified managed files despite stale loaded ExecStart")
	}
	if got := fixture.runner.Calls(); len(got) != 1 || !containsArg(got[0].Args, "--property=ExecStart") {
		t.Fatalf("Start calls = %#v", got)
	}
}

func TestRestartSyncFailureLeavesOldDaemonAndManagedBinaryUntouched(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	beforeBinary, err := os.ReadFile(fixture.manager.binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeManifest, err := os.ReadFile(fixture.manager.manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := newSystemd(Options{
		ReasonixHome: fixture.home, UnitPath: fixture.unit, SocketPath: fixture.socket,
		ExecutablePath: filepath.Join(fixture.root, "missing-cli"), CLIBuildID: lifecycleTestBuildID(t, 'b'),
		Runner: fixture.runner,
	}, currentUID())
	if err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	if _, err := manager.Restart(context.Background()); err == nil {
		t.Fatal("Restart succeeded with missing current executable")
	}
	if calls := fixture.runner.Calls(); len(calls) != 0 {
		t.Fatalf("sync failure executed service command: %#v", calls)
	}
	assertFileContents(t, manager.binaryPath, beforeBinary)
	assertFileContents(t, manager.manifestPath, beforeManifest)
}

func TestRestartReloadsUnitOnlyAfterSuccessfulSyncThenRestarts(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.source, []byte("replacement-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager, err := newSystemd(Options{
		ReasonixHome: fixture.home, UnitPath: fixture.unit, SocketPath: fixture.socket,
		ExecutablePath: fixture.source, CLIBuildID: lifecycleTestBuildID(t, 'b'),
		Runner: fixture.runner,
	}, currentUID())
	if err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	if _, err := manager.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []runnerCall{
		{Name: "systemctl", Args: []string{"--user", "daemon-reload"}},
		systemdShowCall(),
		{Name: "systemctl", Args: []string{"--user", "restart", service.UnitName}},
	}
	if got := fixture.runner.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Restart commands = %#v, want %#v", got, want)
	}
}

func TestInactiveStartReloadFailureNeverStarts(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.source, []byte("synchronized-before-reload"), 0o700); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("daemon-reload failed")
	fixture.runner.Reset()
	fixture.runner.handler = func(name string, args []string) (CommandOutput, error) {
		if name == "systemctl" && containsArg(args, "--property=LoadState") {
			return CommandOutput{Stdout: loadedUnitOutput(fixture.manager, "inactive")}, nil
		}
		if name == "systemctl" && reflect.DeepEqual(args, []string{"--user", "daemon-reload"}) {
			return CommandOutput{Stderr: "reload failed", ExitCode: 1}, boom
		}
		return CommandOutput{}, nil
	}
	if _, err := fixture.manager.Start(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("Start error = %v, want daemon-reload failure", err)
	}
	want := []runnerCall{
		systemdShowCall(),
		{Name: "systemctl", Args: []string{"--user", "daemon-reload"}},
	}
	if got := fixture.runner.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Start calls after reload failure = %#v, want %#v", got, want)
	}
	assertFileContents(t, fixture.manager.binaryPath, []byte("synchronized-before-reload"))
}

func TestInactiveStartRechecksLoadedDefinitionAfterReload(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	showCount := 0
	fixture.runner.handler = func(name string, args []string) (CommandOutput, error) {
		if name == "systemctl" && containsArg(args, "--property=LoadState") {
			showCount++
			output := loadedUnitOutput(fixture.manager, "inactive")
			if showCount > 1 {
				output = replaceLoadedProperty(output, "DropInPaths", "/tmp/unsafe.conf")
			}
			return CommandOutput{Stdout: output}, nil
		}
		return CommandOutput{}, nil
	}
	if _, err := fixture.manager.Start(context.Background()); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("Start error = %v, want ErrUnsafeArtifact", err)
	}
	want := []runnerCall{
		systemdShowCall(),
		{Name: "systemctl", Args: []string{"--user", "daemon-reload"}},
		systemdShowCall(),
	}
	if got := fixture.runner.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Start calls with post-reload drift = %#v, want %#v", got, want)
	}
}

func TestInstallDoesNotEnableLoadedUnitWithDropIn(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	fixture.runner.Reset()
	fixture.runner.handler = func(name string, args []string) (CommandOutput, error) {
		if name == "systemctl" && containsArg(args, "--property=LoadState") {
			return CommandOutput{Stdout: replaceLoadedProperty(loadedUnitOutput(fixture.manager, "inactive"), "DropInPaths", "/tmp/unsafe.conf")}, nil
		}
		return CommandOutput{}, nil
	}
	if _, err := fixture.manager.Install(context.Background()); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("Install error = %v, want ErrUnsafeArtifact", err)
	}
	want := []runnerCall{
		{Name: "systemctl", Args: []string{"--user", "daemon-reload"}},
		systemdShowCall(),
	}
	if got := fixture.runner.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Install calls with drop-in = %#v, want %#v", got, want)
	}
}

func TestStopAndUninstallRefuseLoadedDefinitionDrift(t *testing.T) {
	commands := map[string]func(*SystemdManager) error{
		"stop":      func(manager *SystemdManager) error { _, err := manager.Stop(context.Background()); return err },
		"uninstall": func(manager *SystemdManager) error { _, err := manager.Uninstall(context.Background()); return err },
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			fixture := newManagerFixture(t, 'a')
			if _, err := fixture.manager.Install(context.Background()); err != nil {
				t.Fatal(err)
			}
			fixture.runner.Reset()
			fixture.runner.handler = func(name string, args []string) (CommandOutput, error) {
				if name == "systemctl" && containsArg(args, "--property=LoadState") {
					return CommandOutput{Stdout: replaceLoadedProperty(loadedUnitOutput(fixture.manager, "active"), "NeedDaemonReload", "yes")}, nil
				}
				return CommandOutput{}, nil
			}
			if err := command(fixture.manager); !errors.Is(err, ErrUnsafeArtifact) {
				t.Fatalf("error = %v, want ErrUnsafeArtifact", err)
			}
			if got := fixture.runner.Calls(); !reflect.DeepEqual(got, []runnerCall{systemdShowCall()}) {
				t.Fatalf("drifted %s calls = %#v", name, got)
			}
			if _, err := os.Stat(fixture.unit); err != nil {
				t.Fatalf("drifted %s removed unit: %v", name, err)
			}
		})
	}
}

func TestStopAndUninstallRefuseTamperedDiskUnit(t *testing.T) {
	commands := map[string]func(*SystemdManager) error{
		"stop":      func(manager *SystemdManager) error { _, err := manager.Stop(context.Background()); return err },
		"uninstall": func(manager *SystemdManager) error { _, err := manager.Uninstall(context.Background()); return err },
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			fixture := newManagerFixture(t, 'a')
			if _, err := fixture.manager.Install(context.Background()); err != nil {
				t.Fatal(err)
			}
			unit, err := os.ReadFile(fixture.unit)
			if err != nil {
				t.Fatal(err)
			}
			unit = append(unit, []byte("ExecStop=/tmp/untrusted\n")...)
			if err := os.WriteFile(fixture.unit, unit, 0o600); err != nil {
				t.Fatal(err)
			}
			fixture.runner.Reset()
			if err := command(fixture.manager); !errors.Is(err, ErrUnsafeArtifact) {
				t.Fatalf("error = %v, want ErrUnsafeArtifact", err)
			}
			if calls := fixture.runner.Calls(); len(calls) != 0 {
				t.Fatalf("tampered disk %s executed commands: %#v", name, calls)
			}
		})
	}
}

func TestLoadedDefinitionStrictlyRejectsMissingDuplicateAndDriftedFields(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	validOutput := loadedUnitOutput(fixture.manager, "inactive")
	valid := parseProperties(validOutput)
	if exact, detail := fixture.manager.loadedDefinitionExact(valid); !exact {
		t.Fatalf("valid loaded definition rejected: %s", detail)
	}
	tests := map[string]string{
		"missing-drop-ins":     strings.Replace(validOutput, "DropInPaths=\n", "", 1),
		"duplicate-property":   validOutput + "Restart=on-failure\n",
		"drop-in":              replaceLoadedProperty(validOutput, "DropInPaths", "/tmp/unsafe.conf"),
		"environment":          replaceLoadedProperty(validOutput, "Environment", "REASONIX_HOME=/wrong"),
		"umask":                replaceLoadedProperty(validOutput, "UMask", "0022"),
		"restart":              replaceLoadedProperty(validOutput, "Restart", "always"),
		"type":                 replaceLoadedProperty(validOutput, "Type", "notify"),
		"reload-needed":        replaceLoadedProperty(validOutput, "NeedDaemonReload", "yes"),
		"transient":            replaceLoadedProperty(validOutput, "Transient", "yes"),
		"ignore-errors":        strings.Replace(validOutput, "ignore_errors=no", "ignore_errors=yes", 1),
		"multiple-exec":        strings.Replace(validOutput, "\nFragmentPath=", " { path="+fixture.manager.binaryPath+" ; argv[]="+fixture.manager.binaryPath+" remote serve ; ignore_errors=no ; }\nFragmentPath=", 1),
		"duplicate-exec-field": strings.Replace(validOutput, "ignore_errors=no", "path="+fixture.manager.binaryPath+" ; ignore_errors=no", 1),
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			if exact, detail := fixture.manager.loadedDefinitionExact(parseProperties(output)); exact {
				t.Fatalf("unsafe loaded definition accepted (%s):\n%s", detail, output)
			}
		})
	}
}

func TestStatusDistinguishesManagerFromNotFoundUnit(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	fixture.runner.handler = func(name string, args []string) (CommandOutput, error) {
		if name == "systemctl" && containsArg(args, "--property=LoadState") {
			return CommandOutput{Stdout: replaceLoadedProperty(loadedUnitOutput(fixture.manager, "inactive"), "LoadState", "not-found")}, nil
		}
		if name == "loginctl" {
			return CommandOutput{Stdout: "yes\n"}, nil
		}
		return CommandOutput{}, nil
	}
	status, err := fixture.manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Unit.ManagerAvailable || !status.Unit.Exists || status.Unit.LoadState != "not-found" || status.Unit.LoadedDefinitionExact {
		t.Fatalf("not-found status conflated manager and unit: %+v", status.Unit)
	}
	if !hasDiagnostic(status.Diagnostics, "loaded_unit_mismatch") {
		t.Fatalf("not-found status lacks loaded mismatch diagnostic: %+v", status.Diagnostics)
	}
}

func TestStopStatusDoctorAndLogsNeverModifyManagedBinary(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	listener := createTrustedSocket(t, fixture.socket)
	defer listener.Close()
	fixture.runner.Reset()
	fixture.runner.handler = statusHandler(fixture.manager, "active", true)
	fixture.manager.probe = testProbe{result: DaemonProbeResult{BuildID: fixture.buildID}}

	before, err := treeDigest(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	status, err := fixture.manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed.Valid || !status.Unit.ExecStartExact || !status.Unit.ContentExact || !status.Unit.LoadedExecStartExact || !status.Socket.Secure {
		t.Fatalf("Status did not validate installation: %+v", status)
	}
	if status.InstalledBuildID == nil || status.DaemonBuildID == nil || protocol.CompareBuildID(fixture.buildID, status.CLIBuildID) != nil {
		t.Fatalf("top-level Build IDs are incomplete: cli=%+v installed=%+v daemon=%+v", status.CLIBuildID, status.InstalledBuildID, status.DaemonBuildID)
	}
	statusJSON, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"cliBuildId":`, `"installedBuildId":`, `"daemonBuildId":`} {
		if !strings.Contains(string(statusJSON), field) {
			t.Fatalf("status JSON is missing %s: %s", field, statusJSON)
		}
	}
	report, err := fixture.manager.Doctor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Healthy {
		t.Fatalf("Doctor report is unhealthy: %+v", report.Checks)
	}
	logs, err := fixture.manager.Logs(context.Background(), LogsOptions{Lines: 25, Since: "1 hour ago"})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Output != "remote log\n" {
		t.Fatalf("logs output = %q", logs.Output)
	}
	if _, err := fixture.manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := treeDigest(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("read-only lifecycle commands or Stop modified files")
	}
	for _, call := range fixture.runner.Calls() {
		if call.Name != "systemctl" && call.Name != "loginctl" && call.Name != "journalctl" {
			t.Fatalf("unexpected executable: %+v", call)
		}
	}
}

func TestStatusDetectsManifestHashTamperingWithoutRepair(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.manager.binaryPath, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	fixture.runner.handler = statusHandler(fixture.manager, "inactive", false)
	before, err := treeDigest(fixture.home)
	if err != nil {
		t.Fatal(err)
	}
	status, err := fixture.manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Installed.Valid || !hasDiagnostic(status.Diagnostics, "installed_identity_invalid") {
		t.Fatalf("tampered status = %+v", status.Installed)
	}
	after, err := treeDigest(fixture.home)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("Status repaired or modified a tampered binary")
	}
}

func TestInstallRejectsSymlinkedManagedDirectory(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	target := filepath.Join(fixture.root, "attacker")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, fixture.manager.managedRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Install(context.Background()); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("Install error = %v, want ErrUnsafeArtifact", err)
	}
	if calls := fixture.runner.Calls(); len(calls) != 0 {
		t.Fatalf("unsafe directory executed commands: %#v", calls)
	}
	if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
		t.Fatalf("symlink target was modified: entries=%v err=%v", entries, err)
	}
}

func TestUninstallRemovesOnlyRemoteManagedArtifacts(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	configPath := filepath.Join(fixture.home, "config.toml")
	sessionPath := filepath.Join(fixture.home, "sessions", "kept-session.json")
	if err := os.WriteFile(configPath, []byte("config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Dir(sessionPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath, []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	result, err := fixture.manager.Uninstall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("Uninstall did not report a change")
	}
	want := []runnerCall{
		systemdShowCall(),
		{Name: "systemctl", Args: []string{"--user", "stop", service.UnitName}},
		{Name: "systemctl", Args: []string{"--user", "disable", service.UnitName}},
		{Name: "systemctl", Args: []string{"--user", "daemon-reload"}},
	}
	if got := fixture.runner.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Uninstall commands = %#v, want %#v", got, want)
	}
	for _, removed := range []string{fixture.unit, fixture.manager.binaryPath, fixture.manager.manifestPath, fixture.manager.managedDir, fixture.manager.managedRoot} {
		if _, err := os.Lstat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed path %q still exists, err=%v", removed, err)
		}
	}
	assertMode(t, fixture.manager.lockPath, 0o600)
	assertFileContents(t, configPath, []byte("config"))
	assertFileContents(t, sessionPath, []byte("session"))
}

func TestUninstallPreservesUnknownRemoteContentWithoutRecursiveRemoval(t *testing.T) {
	fixture := newManagerFixture(t, 'a')
	if _, err := fixture.manager.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(fixture.manager.managedDir, "operator-note.txt")
	if err := os.WriteFile(unknown, []byte("preserve me"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.runner.Reset()
	result, err := fixture.manager.Uninstall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("Uninstall did not report trusted removals")
	}
	assertFileContents(t, unknown, []byte("preserve me"))
	for _, removed := range []string{fixture.unit, fixture.manager.binaryPath, fixture.manager.manifestPath} {
		if _, err := os.Lstat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("trusted managed path %q still exists, err=%v", removed, err)
		}
	}
}

func TestSystemdQuoteEscapesSpecifiersAndRejectsControlCharacters(t *testing.T) {
	tests := map[string]string{
		"":                                       `""`,
		"plain":                                  `"plain"`,
		"space separated":                        `"space separated"`,
		`/home/user/reasonix % profile/"binary"`: `"/home/user/reasonix %% profile/\"binary\""`,
		`C:\reasonix\binary`:                     `"C:\\reasonix\\binary"`,
		`${REASONIX_BIN}/reasonix`:               `"$${REASONIX_BIN}/reasonix"`,
	}
	for input, wanted := range tests {
		quoted, err := systemdQuote(input)
		if err != nil {
			t.Fatalf("systemdQuote(%q): %v", input, err)
		}
		if quoted != wanted {
			t.Fatalf("systemdQuote(%q) = %q, want %q", input, quoted, wanted)
		}
	}
	if _, err := systemdQuote("bad\npath"); err == nil {
		t.Fatal("systemdQuote accepted a newline")
	}
	environment, err := systemdEnvironmentQuote("REASONIX_HOME=/srv/${LITERAL_HOME}")
	if err != nil {
		t.Fatal(err)
	}
	if environment != `"REASONIX_HOME=/srv/${LITERAL_HOME}"` {
		t.Fatalf("Environment quote expanded or doubled literal dollar: %q", environment)
	}
	if err := validateDirectArgument("since", "today\n--output=json"); err == nil {
		t.Fatal("Logs argument accepted a control character")
	}
}

func TestRenderedUnitKeepsLiteralDollarHomeFixed(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "${REMOTE_HOME} with space")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "reasonix")
	if err := os.WriteFile(source, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	manager, err := newSystemd(Options{
		ReasonixHome: home, UnitPath: filepath.Join(root, "unit", service.UnitName),
		SocketPath: filepath.Join(root, "runtime", service.SocketFileName), ExecutablePath: source,
		CLIBuildID: lifecycleTestBuildID(t, 'a'), Runner: &fakeRunner{},
	}, currentUID())
	if err != nil {
		t.Fatal(err)
	}
	unit, err := manager.renderUnit()
	if err != nil {
		t.Fatal(err)
	}
	text := string(unit)
	if !strings.Contains(text, `ExecStart="`+strings.ReplaceAll(manager.binaryPath, "$", "$$")+`" remote serve`) {
		t.Fatalf("ExecStart does not preserve literal dollar path:\n%s", text)
	}
	if !strings.Contains(text, `Environment="REASONIX_HOME=`+home+`"`) || strings.Contains(text, `Environment="REASONIX_HOME=`+strings.ReplaceAll(home, "$", "$$")+`"`) {
		t.Fatalf("Environment dollar semantics are wrong:\n%s", text)
	}
	loaded := loadedUnitOutput(manager, "inactive")
	loaded = replaceLoadedProperty(loaded, "Environment", `"REASONIX_HOME=`+home+`" "REASONIX_REMOTE_INSTALL_PROFILE=`+manager.profile.ID+`"`)
	if exact, detail := manager.loadedDefinitionExact(parseProperties(loaded)); !exact {
		t.Fatalf("quoted loaded Environment was not decoded exactly: %s\n%s", detail, loaded)
	}
}

func TestLifecycleBackendRejectsRootDaemonProfile(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "reasonix")
	if err := os.WriteFile(source, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := newSystemd(Options{
		ReasonixHome: home, UnitPath: filepath.Join(root, service.UnitName),
		SocketPath: filepath.Join(root, service.SocketFileName), ExecutablePath: source,
		CLIBuildID: lifecycleTestBuildID(t, 'a'), Runner: &fakeRunner{},
	}, 0)
	if !errors.Is(err, ErrRootUnsupported) {
		t.Fatalf("lifecycle backend error = %v, want ErrRootUnsupported", err)
	}
}

func TestNewRefusesNonLinuxWithoutFallingBack(t *testing.T) {
	for _, platform := range []string{"windows", "darwin", "freebsd"} {
		t.Run(platform, func(t *testing.T) {
			manager, err := newForPlatform(platform, Options{})
			if manager != nil || !errors.Is(err, ErrUnsupportedPlatform) {
				t.Fatalf("manager=%T err=%v, want explicit unsupported platform", manager, err)
			}
		})
	}
}

func activeStateHandler(manager *SystemdManager, state string) func(string, []string) (CommandOutput, error) {
	return func(name string, args []string) (CommandOutput, error) {
		if name == "systemctl" && containsArg(args, "--property=ActiveState") {
			return CommandOutput{Stdout: loadedUnitOutput(manager, state)}, nil
		}
		return CommandOutput{}, nil
	}
}

func statusHandler(manager *SystemdManager, active string, lingering bool) func(string, []string) (CommandOutput, error) {
	return func(name string, args []string) (CommandOutput, error) {
		switch name {
		case "systemctl":
			if containsArg(args, "--property=LoadState") {
				return CommandOutput{Stdout: loadedUnitOutput(manager, active)}, nil
			}
		case "loginctl":
			value := "no\n"
			if lingering {
				value = "yes\n"
			}
			return CommandOutput{Stdout: value}, nil
		case "journalctl":
			return CommandOutput{Stdout: "remote log\n"}, nil
		}
		return CommandOutput{}, nil
	}
}

func loadedUnitOutput(manager *SystemdManager, active string) string {
	sub := "running"
	if active != "active" {
		sub = "dead"
	}
	return strings.Join([]string{
		"LoadState=loaded",
		"UnitFileState=enabled",
		"ActiveState=" + active,
		"SubState=" + sub,
		"Result=success",
		"ExecStart={ path=" + manager.binaryPath + " ; argv[]=" + manager.binaryPath + " remote serve ; ignore_errors=no ; }",
		"FragmentPath=" + manager.unitPath,
		"DropInPaths=",
		"Environment=REASONIX_HOME=" + manager.profile.ReasonixHome + " REASONIX_REMOTE_INSTALL_PROFILE=" + manager.profile.ID,
		"UMask=0077",
		"Restart=on-failure",
		"Type=simple",
		"NeedDaemonReload=no",
		"Transient=no",
		"",
	}, "\n")
}

func replaceLoadedProperty(output, key, value string) string {
	prefix := key + "="
	lines := strings.Split(output, "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[index] = prefix + value
			return strings.Join(lines, "\n")
		}
	}
	return output + prefix + value + "\n"
}

func systemdShowCall() runnerCall {
	args := []string{"--user", "show", service.UnitName, "--no-pager"}
	for _, property := range systemdShowProperties {
		args = append(args, "--property="+property)
	}
	return runnerCall{Name: "systemctl", Args: args}
}

func createTrustedSocket(t *testing.T, path string) net.Listener {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		t.Fatal(err)
	}
	return listener
}

func containsArg(args []string, wanted string) bool {
	for _, arg := range args {
		if arg == wanted {
			return true
		}
	}
	return false
}

func hasDiagnostic(diagnostics []Diagnostic, code string) bool {
	for _, item := range diagnostics {
		if item.Code == code {
			return true
		}
	}
	return false
}

func assertMode(t *testing.T, path string, wanted fs.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != wanted.Perm() {
		t.Fatalf("%s mode = %04o, want %04o", path, got, wanted.Perm())
	}
}

func assertFileContents(t *testing.T, path string, wanted []byte) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(contents, wanted) {
		t.Fatalf("%s = %q, want %q", path, contents, wanted)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func treeDigest(root string) (string, error) {
	hasher := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(hasher, "%s\x00%d\x00%d\x00", relative, uint32(info.Mode()), info.Size())
		if info.Mode().IsRegular() {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, _ = hasher.Write(contents)
		} else if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, _ = hasher.Write([]byte(target))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
