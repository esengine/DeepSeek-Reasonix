//go:build linux

package checkenv

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/shellrun"
)

func runtimeFixture(t *testing.T) Spec {
	t.Helper()
	image, source, storage := testenv.VerificationRuntime(t), t.TempDir(), t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	testenv.InstallVerificationBinary(t, image, executable, "/bin/probe")
	if err := os.WriteFile(filepath.Join(source, "input"), []byte("source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return Spec{RuntimeRoot: image, SourceRoot: source, Directory: source, StorageRoot: storage}
}

func runControlled(t *testing.T, ctx context.Context, spec Spec, command string, denied []string, environment map[string]string, progress func(string)) (*Invocation, shellrun.Result) {
	t.Helper()
	i := New(spec, command, "host-execution", "host-call")
	t.Cleanup(i.Close)
	argv, err := i.Prepare(ctx, tool.ControlledShellRequest{Command: command, Directory: spec.Directory, ReadDenies: denied, DeclaredEnvironment: environment})
	if err != nil {
		t.Fatal(err)
	}
	result := shellrun.RunForeground(tool.WithControlledShell(ctx, i), shellrun.Request{Argv: argv, Dir: spec.Directory,
		Command: command, ShellPath: "/bin/bash", Track: true, Progress: progress})
	return i, result
}

func TestLinuxControlledEnvironmentCoversOnlyFrozenDeclaredInputs(t *testing.T) {
	spec := runtimeFixture(t)
	hostFile := filepath.Join(t.TempDir(), "host-only")
	if err := os.WriteFile(hostFile, []byte("host secret"), 0600); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(t.TempDir(), "service.sock")
	unix, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	private := filepath.Join(spec.SourceRoot, "private")
	if err := os.WriteFile(private, []byte("forbidden"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHECKENV_HOST_SECRET", "must-not-leak")
	environment := map[string]string{"CHECKENV_PROBE": "inputs", "CHECKENV_HOST_FILE": hostFile, "CHECKENV_SOCKET": socketPath, "CHECKENV_TCP": tcp.Addr().String()}
	command := `/bin/probe -test.run=^TestControlledChildProbe$; result=$?; if printf overwrite > input; then exit 99; fi; exit "$result"`
	i, result := runControlled(t, t.Context(), spec, command, []string{private}, environment, nil)
	if result.Err != nil {
		t.Fatalf("controlled check: %v %s", result.Err, result.Combined)
	}
	inputs := i.Inputs()
	if len(inputs) != 1 || !inputs[0].Completed || !inputs[0].Stable || !Verify(inputs[0], "host-execution") {
		t.Fatalf("no owned environment receipt: %+v", inputs)
	}
	if Verify(inputs[0], "different-execution") {
		t.Fatal("receipt crossed execution identity")
	}
	forged := inputs[0]
	forged.EnvironmentHash = "changed"
	if Verify(forged, "host-execution") {
		t.Fatal("modified receipt accepted")
	}
	if raw, err := os.ReadFile(filepath.Join(spec.SourceRoot, "input")); err != nil || string(raw) != "source\n" {
		t.Fatalf("verification wrote source: %q %v", raw, err)
	}
	i.Close()
	if !Verify(inputs[0], "host-execution") {
		t.Fatal("completed receipt depended on live mutable worktree")
	}
}

func TestControlledChildProbe(t *testing.T) {
	if os.Getenv("CHECKENV_PROBE") != "inputs" {
		return
	}
	if os.Getenv("CHECKENV_HOST_SECRET") != "" {
		t.Fatal("inherited host environment")
	}
	if _, err := os.ReadFile(os.Getenv("CHECKENV_HOST_FILE")); err == nil {
		t.Fatal("read undeclared host file")
	}
	if _, err := os.ReadFile("private"); err == nil {
		t.Fatal("read forbidden source file")
	}
	for _, target := range []struct{ network, address string }{{"unix", os.Getenv("CHECKENV_SOCKET")}, {"tcp", os.Getenv("CHECKENV_TCP")}} {
		if connection, err := net.DialTimeout(target.network, target.address, time.Second); err == nil {
			connection.Close()
			t.Fatalf("connected to host %s service", target.network)
		}
	}
}

func TestLinuxCancelledVerificationNeverMintsAReceipt(t *testing.T) {
	spec := runtimeFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	i, result := runControlled(t, ctx, spec, `printf ready; while :; do :; done`, nil, nil, func(chunk string) {
		if strings.Contains(chunk, "ready") {
			cancel()
		}
	})
	if result.Err == nil {
		t.Fatal("cancelled verification reported success")
	}
	for _, input := range i.Inputs() {
		if Verify(input, "host-execution") {
			t.Fatal("cancelled process minted evidence")
		}
	}
}

func TestLinuxVerificationReapsDetachedNamespaceDescendants(t *testing.T) {
	spec := runtimeFixture(t)
	token := "reasonix-check-daemon-" + rand.Text()
	command := fmt.Sprintf(`/bin/probe -test.run=^TestControlledDaemonProbe$ %s & until [[ -e /tmp/checkenv-daemon-ready ]]; do [[ $SECONDS -lt 5 ]] || exit 1; done; exit 0`, token)
	i, result := runControlled(t, t.Context(), spec, command, nil, map[string]string{"CHECKENV_PROBE": "daemon"}, nil)
	if result.Err != nil {
		t.Fatalf("daemon check failed: %v %s", result.Err, result.Combined)
	}
	for _, input := range i.Inputs() {
		if !Verify(input, "host-execution") {
			t.Fatal("namespace completion not proven")
		}
	}
	processes, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, path := range processes {
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), token) {
			t.Fatal("descendant survived namespace completion", path)
		}
	}
}

func TestControlledDaemonProbe(t *testing.T) {
	if os.Getenv("CHECKENV_PROBE") != "daemon" {
		return
	}
	if err := detachProbe(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/tmp/checkenv-daemon-ready", nil, 0600); err != nil {
		t.Fatal(err)
	}
	os.Stdout.Close()
	os.Stderr.Close()
	for {
		time.Sleep(time.Hour)
	}
}

func TestLinuxVerificationCommandOwnsNamespaceLifecycle(t *testing.T) {
	spec := runtimeFixture(t)
	i, result := runControlled(t, t.Context(), spec, `[[ $$ == 1 ]] || exit 97`, nil, nil, nil)
	if result.Err != nil {
		t.Fatalf("verification command is not namespace init: %v %s", result.Err, result.Combined)
	}
	if inputs := i.Inputs(); len(inputs) != 1 || !Verify(inputs[0], "host-execution") {
		t.Fatalf("namespace owner did not mint receipt: %+v", inputs)
	}
}

func TestLinuxChangedLaunchCannotMintReceipt(t *testing.T) {
	for _, field := range []string{"path", "arguments", "environment", "directory"} {
		t.Run(field, func(t *testing.T) {
			spec := runtimeFixture(t)
			i := New(spec, "exit 0", "execution", "call")
			defer i.Close()
			argv, err := i.Prepare(t.Context(), tool.ControlledShellRequest{Command: "exit 0", Directory: spec.Directory})
			if err != nil {
				t.Fatal(err)
			}
			cmd, finish, err := i.Command(t.Context(), tool.ShellLaunch{Command: "exit 0", Directory: spec.Directory, Arguments: argv})
			if err != nil {
				t.Fatal(err)
			}
			switch field {
			case "path":
				cmd.Path = "/bin/true"
			case "arguments":
				cmd.Args = append([]string(nil), cmd.Args...)
				cmd.Args[len(cmd.Args)-1] = "exit 1"
			case "environment":
				cmd.Env = append(cmd.Env, "UNDECLARED=value")
			case "directory":
				cmd.Dir = spec.SourceRoot
			}
			if err := cmd.Start(); err == nil {
				i.Started(cmd)
				_ = cmd.Wait()
				i.WaitScope()
			}
			finish()
			for _, input := range i.Inputs() {
				if Verify(input, "execution") {
					t.Fatal("changed launch minted receipt")
				}
			}
		})
	}
}
