//go:build linux

package checkenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"reasonix/internal/base/proc"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/shellrun"
)

func prepareScopeTest(t *testing.T, command string) (*Invocation, []string) {
	t.Helper()
	spec := runtimeFixture(t)
	i := New(spec, command, "execution", "call")
	t.Cleanup(i.Close)
	argv, err := i.Prepare(t.Context(), tool.ControlledShellRequest{Command: command, Directory: spec.Directory})
	if err != nil {
		t.Fatal(err)
	}
	return i, argv
}

func TestLinuxFailedIdentityHandshakeNeverStartsPayload(t *testing.T) {
	const command = "printf PAYLOAD-RAN"
	i, argv := prepareScopeTest(t, command)
	var observed int
	i.scope.bind = func(pid int, namespace uint64, monitor *os.Process) (int, error) {
		fd, err := bindNamespacePID(pid, namespace, monitor)
		if err != nil {
			t.Error(err)
			return -1, err
		}
		observed, err = unix.PidfdOpen(pid, 0)
		if err != nil {
			t.Error(err)
		}
		return fd, errors.New("injected namespace identity mismatch")
	}
	result := shellrun.RunForeground(tool.WithControlledShell(t.Context(), i), shellrun.Request{Argv: argv, Dir: i.spec.Directory, Command: command, Track: true})
	if !errors.Is(result.Err, ErrUnsupported) || strings.Contains(result.Combined, "PAYLOAD-RAN") {
		t.Fatalf("failed setup executed payload or lost error: %+v", result)
	}
	select {
	case <-result.Tracked.WaitCompleted():
	default:
		t.Fatal("confirmed namespace failure did not release ownership")
	}
	if inputs := i.Inputs(); len(inputs) != 1 || !slices.Equal(inputs[0].Gaps, []string{"controlled_namespace_unbound"}) {
		t.Fatalf("namespace identity failure lost its cause: %+v", inputs)
	}
	i.Close()
	if i.state.directory != "" {
		t.Fatal("confirmed terminated environment was retained")
	}
	if observed <= 0 {
		t.Fatal("test did not capture the real namespace identity")
	}
	defer unix.Close(observed)
	poll := []unix.PollFd{{Fd: int32(observed), Events: unix.POLLIN}}
	if count, err := unix.Poll(poll, 5000); err != nil || count != 1 || poll[0].Revents&unix.POLLIN == 0 {
		t.Fatalf("failed namespace survived monitor termination: %v %v", poll, err)
	}
}

func TestLinuxRetiredMonitorIdentityCannotAuthorizeReusedParentPID(t *testing.T) {
	retired := exec.Command("/bin/true")
	if err := retired.Run(); err != nil {
		t.Fatal(err)
	}
	retired.Process.Pid = os.Getpid()
	child := exec.Command("/bin/bash", "-c", "while :; do :; done")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	fd, err := bindNamespacePID(child.Process.Pid, 0, retired.Process)
	if fd >= 0 {
		unix.Close(fd)
		t.Fatal("retired monitor's reused numeric PID authorized an unrelated child")
	}
	if !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("monitor handle was not checked: %v", err)
	}
}

func TestLinuxFailedCancellationSignalWaitsForNamespace(t *testing.T) {
	const command = "printf ready; while :; do :; done"
	i, argv := prepareScopeTest(t, command)
	i.scope.signal = func(int) error { return unix.EPERM }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := shellrun.RunForeground(tool.WithControlledShell(ctx, i), shellrun.Request{Argv: argv, Dir: i.spec.Directory, Command: command, Track: true,
		Progress: func(chunk string) {
			if strings.Contains(chunk, "ready") {
				cancel()
			}
		}})
	if result.Err == nil || !i.scope.completed() {
		t.Fatalf("failed signal did not await namespace termination: %+v", result)
	}
	for _, input := range i.Inputs() {
		if Verify(input, "execution") {
			t.Fatal("failed cancellation minted a receipt")
		}
	}
	i.Close()
	if i.state.directory != "" {
		t.Fatal("proven terminated environment was not cleaned")
	}
}

func assertGaps(t *testing.T, i *Invocation, want ...string) {
	t.Helper()
	if inputs := i.Inputs(); len(inputs) != 1 || !slices.Equal(inputs[0].Gaps, want) || Verify(inputs[0], "execution") {
		t.Fatalf("gaps = %+v, want %v", inputs, want)
	}
}

func TestLinuxRejectedLauncherReleasesEnvironmentWithCause(t *testing.T) {
	const command = "printf PAYLOAD-RAN"
	i, argv := prepareScopeTest(t, command)
	rejected := append([]string{argv[0], "--reasonix-unsupported-option"}, argv[1:]...)
	i.state.argv = rejected
	directory := i.state.directory
	result := shellrun.RunForeground(tool.WithControlledShell(t.Context(), i), shellrun.Request{Argv: rejected, Dir: i.spec.Directory, Command: command, Track: true})
	if !errors.Is(result.Err, ErrUnsupported) || !errors.Is(result.Err, errNamespaceUnreported) ||
		errors.Is(result.Err, proc.ErrScopeCompletionUnknown) || strings.Contains(result.Combined, "PAYLOAD-RAN") {
		t.Fatalf("launcher that never reported a namespace was not settled with its cause: %+v", result)
	}
	select {
	case <-result.Tracked.WaitCompleted():
	default:
		t.Fatal("a launcher no reader survives was left owning its scope")
	}
	assertGaps(t, i, "controlled_namespace_unreported")
	i.Close()
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Fatalf("unreported namespace leaked its frozen environment: %v", err)
	}
}

func TestLinuxSurvivingBlockReaderIsNeverReleased(t *testing.T) {
	grace := namespaceReleaseGrace
	namespaceReleaseGrace = 200 * time.Millisecond
	t.Cleanup(func() { namespaceReleaseGrace = grace })
	const command = "printf PAYLOAD-RAN"
	i, _ := prepareScopeTest(t, command)
	scratch := t.TempDir()
	released, readerPID := filepath.Join(scratch, "released"), filepath.Join(scratch, "reader.pid")
	script := fmt.Sprintf(`(exec 1>/dev/null 2>&1 3>&- 4>&-; read -r -n 1 <&5; : > %q) & printf %%s "$!" > %q; exit 0`, released, readerPID)
	launcher := []string{"/bin/bash", "-c", script}
	i.state.argv = launcher
	directory := i.state.directory
	result := shellrun.RunForeground(tool.WithControlledShell(t.Context(), i), shellrun.Request{Argv: launcher, Dir: i.spec.Directory, Command: command, Track: true})
	raw, err := os.ReadFile(readerPID)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Kill(pid, unix.SIGKILL) })
	if !errors.Is(result.Err, proc.ErrScopeCompletionUnknown) || !errors.Is(result.Err, errNamespaceUnreported) {
		t.Fatalf("an unproven scope was reported as settled or lost its cause: %+v", result)
	}
	assertGaps(t, i, "controlled_namespace_unreported")
	i.Close()
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Fatalf("frozen environment retained although deleting it cannot release the reader: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Lstat(released); !os.IsNotExist(err) {
		t.Fatal("closing the environment released a surviving block reader")
	}
	if err := unix.Kill(pid, 0); err != nil {
		t.Fatalf("block reader exited: %v", err)
	}
}
