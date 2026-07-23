//go:build windows

package main

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestResolveRemoteSSHExecutableUsesSystemOpenSSH(t *testing.T) {
	systemRoot := strings.TrimSpace(os.Getenv("SystemRoot"))
	if systemRoot == "" {
		t.Fatal("SystemRoot is not set")
	}
	want := filepath.Join(systemRoot, "System32", "OpenSSH", "ssh.exe")

	got, err := resolveRemoteSSHExecutable("")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("resolved OpenSSH path is not absolute: %q", got)
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(want)) {
		t.Fatalf("resolved OpenSSH path = %q, want system OpenSSH %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat resolved OpenSSH path %q: %v", got, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("resolved OpenSSH path is not a regular file: %q", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, got, "-V").CombinedOutput()
	if err != nil {
		t.Fatalf("start %q -V: %v (output %q)", got, err, strings.TrimSpace(string(output)))
	}
	if !strings.Contains(strings.ToLower(string(output)), "openssh") {
		t.Fatalf("unexpected %q -V output: %q", got, strings.TrimSpace(string(output)))
	}
}

func TestRemoteSSHProcessHidesConsoleWindow(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit", "0")
	newRemoteSSHProcess(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("OpenSSH SysProcAttr is nil")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("OpenSSH child is not marked HideWindow")
	}
	const createNoWindow = 0x08000000
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("OpenSSH child is missing CREATE_NO_WINDOW: flags=%#x", cmd.SysProcAttr.CreationFlags)
	}
}

func TestRemoteSSHProcessHasNoConsoleAtRuntime(t *testing.T) {
	// A hidden CREATE_NEW_CONSOLE child is the positive control: it must own a
	// real console handle without showing a window to the interactive desktop.
	if !runWindowsConsoleProbe(t, false) {
		t.Fatal("hidden positive-control child did not receive a console handle")
	}
	// The production Remote OpenSSH start path must preserve CREATE_NO_WINDOW
	// while it adds CREATE_SUSPENDED and adopts the child into a Job Object.
	if runWindowsConsoleProbe(t, true) {
		t.Fatal("production Remote OpenSSH child received a console handle")
	}
}

func runWindowsConsoleProbe(t *testing.T, production bool) bool {
	t.Helper()
	t.Setenv("GO_WANT_REMOTE_SSH_CONSOLE_PROBE", "1")
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestRemoteSSHConsoleProbeProcess$")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr

	var process *remoteSSHProcess
	if production {
		process = newRemoteSSHProcess(cmd)
		if err := process.start(cmd); err != nil {
			t.Fatal(err)
		}
	} else {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: windows.CREATE_NEW_CONSOLE,
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}

	line, readErr := bufio.NewReader(stdout).ReadString('\n')
	var waitErr error
	if process != nil {
		waitErr = process.wait(cmd)
	} else {
		waitErr = cmd.Wait()
	}
	if readErr != nil {
		t.Fatalf("read console probe: %v", readErr)
	}
	if waitErr != nil {
		t.Fatalf("wait console probe: %v", waitErr)
	}
	handle, err := strconv.ParseUint(strings.TrimSpace(line), 10, 64)
	if err != nil {
		t.Fatalf("parse console probe result: %v", err)
	}
	return handle != 0
}

func TestRemoteSSHConsoleProbeProcess(t *testing.T) {
	if os.Getenv("GO_WANT_REMOTE_SSH_CONSOLE_PROBE") != "1" {
		return
	}
	getConsoleWindow := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")
	handle, _, _ := getConsoleWindow.Call()
	_, _ = os.Stdout.WriteString(strconv.FormatUint(uint64(handle), 10) + "\n")
	os.Exit(0)
}

func TestRemoteSSHTransportContextCancellationStopsAndReapsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	transport, descendant := startWindowsRemoteSSHProcessTree(t, ctx)
	cancel()

	assertWindowsRemoteSSHWait(t, transport, "context cancellation")
	assertWindowsProcessHandleExited(t, descendant, "OpenSSH descendant after context cancellation")

	if !transport.processExited.Load() {
		t.Fatal("OpenSSH wait goroutine did not observe process exit")
	}
	if state := transport.cmd.ProcessState; state == nil || !state.Exited() {
		t.Fatalf("OpenSSH process was not reaped: %#v", state)
	}
	if err := transport.Wait(); err == nil {
		t.Fatal("repeated Wait lost the cached cancellation failure")
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("close reaped OpenSSH transport: %v", err)
	}
}

func TestRemoteSSHTransportLiveCloseStopsAndReapsProcessTree(t *testing.T) {
	transport, descendant := startWindowsRemoteSSHProcessTree(t, context.Background())
	if err := transport.Close(); err != nil {
		t.Fatalf("live Close: %v", err)
	}
	assertWindowsRemoteSSHWait(t, transport, "live Close")
	assertWindowsProcessHandleExited(t, descendant, "OpenSSH descendant after live Close")
	if state := transport.cmd.ProcessState; state == nil || !state.Exited() {
		t.Fatalf("OpenSSH parent was not reaped after live Close: %#v", state)
	}
}

func TestRemoteSSHTransportNormalWaitFinishesJobAndReapsDescendant(t *testing.T) {
	transport, descendant := startWindowsRemoteSSHProcessTreeMode(t, context.Background(), "tree-exit")
	waitDone := make(chan error, 1)
	go func() { waitDone <- transport.Wait() }()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("Wait after normal parent exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Windows SSH normal exit was not reaped")
	}
	assertWindowsProcessHandleExited(t, descendant, "OpenSSH descendant after normal parent exit")
	transport.process.mu.Lock()
	job := transport.process.job
	transport.process.mu.Unlock()
	if job != 0 {
		t.Fatalf("normal Wait retained Windows Job Object handle %d", job)
	}
}

func TestRemoteSSHNaturalExitLinearizesBeforeConcurrentCloseAndCancel(t *testing.T) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "cmd", "/c", "exit", "0")
	process := newRemoteSSHProcess(cmd)
	if err := process.start(cmd); err != nil {
		t.Fatal(err)
	}

	transport := &RemoteSSHTransport{cmd: cmd, process: process}
	reapEntered := make(chan struct{})
	releaseReap := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseReap) }) }
	t.Cleanup(release)
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- process.waitWithReaper(cmd, func() error {
			close(reapEntered)
			<-releaseReap
			return cmd.Wait()
		})
	}()

	select {
	case <-reapEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("natural OpenSSH exit was not observed")
	}
	if cmd.ProcessState != nil {
		t.Fatalf("test seam reaped OpenSSH before the Close race: %#v", cmd.ProcessState)
	}
	if transport.processExited.Load() {
		t.Fatal("test requires the transport exit flag to remain false before reap")
	}

	startRace := make(chan struct{})
	closeDone := make(chan error, 1)
	cancelDone := make(chan error, 1)
	go func() {
		<-startRace
		closeDone <- transport.Close()
	}()
	go func() {
		<-startRace
		cancelDone <- cmd.Cancel()
	}()
	close(startRace)

	if err := <-closeDone; err != nil {
		t.Fatalf("Close during pre-reap window: %v", err)
	}
	if err := <-cancelDone; !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("context cancel during pre-reap window = %v, want os.ErrProcessDone", err)
	}
	process.mu.Lock()
	finished := process.finished
	killRequested := process.killRequested
	job := process.job
	process.mu.Unlock()
	if !finished {
		t.Fatal("natural exit was not linearized as finished before exec.Cmd reap")
	}
	if killRequested {
		t.Fatal("Close/context cancellation took taskkill ownership after natural exit")
	}
	if job != 0 {
		t.Fatalf("natural exit retained Job Object handle %d before reap", job)
	}

	release()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("wait after natural exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exec.Cmd reap did not complete")
	}
	if state := cmd.ProcessState; state == nil || !state.Exited() {
		t.Fatalf("naturally exited OpenSSH was not reaped: %#v", state)
	}
}

func startWindowsRemoteSSHProcessTree(t *testing.T, ctx context.Context) (*RemoteSSHTransport, windows.Handle) {
	t.Helper()
	return startWindowsRemoteSSHProcessTreeMode(t, ctx, "tree")
}

func startWindowsRemoteSSHProcessTreeMode(t *testing.T, ctx context.Context, mode string) (*RemoteSSHTransport, windows.Handle) {
	t.Helper()
	t.Setenv("GO_WANT_REMOTE_SSH_FAKE", "1")
	t.Setenv("REMOTE_SSH_FAKE_MODE", mode)
	factory := &RemoteSSHTransportFactory{commandContext: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return remoteSSHFakeCommand(ctx)
	}}
	transport, err := factory.Start(ctx, "host")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = transport.Close() })

	line, err := bufio.NewReader(transport).ReadString('\n')
	if err != nil {
		t.Fatalf("read process-tree readiness: %v", err)
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "ready" {
		t.Fatalf("process-tree readiness = %q", line)
	}
	pid, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil || pid == 0 {
		t.Fatalf("descendant PID in readiness %q: %v", line, err)
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		t.Fatalf("open OpenSSH descendant %d: %v", pid, err)
	}
	transport.process.mu.Lock()
	job := transport.process.job
	transport.process.mu.Unlock()
	if job == 0 {
		t.Fatal("OpenSSH process tree was not adopted into a Windows Job Object")
	}
	t.Cleanup(func() {
		_ = windows.TerminateProcess(handle, 1)
		_ = windows.CloseHandle(handle)
	})
	return transport, handle
}

func assertWindowsRemoteSSHWait(t *testing.T, transport *RemoteSSHTransport, action string) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- transport.Wait() }()
	select {
	case err := <-done:
		var failure *RemoteSSHConnectionFailure
		if !errors.As(err, &failure) || failure.Code != RemoteSSHTransportLost || failure.Stage != RemoteSSHStageTransport {
			t.Fatalf("Wait after %s = %#v, want TRANSPORT_LOST/transport", action, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Windows SSH process tree survived %s", action)
	}
}

func assertWindowsProcessHandleExited(t *testing.T, handle windows.Handle, label string) {
	t.Helper()
	result, err := windows.WaitForSingleObject(handle, uint32((5*time.Second)/time.Millisecond))
	if err != nil {
		t.Fatalf("wait for %s: %v", label, err)
	}
	if result != windows.WAIT_OBJECT_0 {
		t.Fatalf("%s is still alive (wait result %d)", label, result)
	}
}
