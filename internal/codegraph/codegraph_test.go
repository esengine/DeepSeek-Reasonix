package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// writeExec writes an executable file at path with the given content and +x.
func writeExec(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestResolveOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("override path test uses a unix +x bit")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "codegraph")
	writeExec(t, bin, "#!/bin/sh\nexit 0\n")

	got, ok := Resolve(bin)
	if !ok || got != bin {
		t.Fatalf("Resolve(%q) = %q, %v; want %q, true", bin, got, ok, bin)
	}
}

func TestResolveOverrideMissingFallsThrough(t *testing.T) {
	// A non-existent override must not resolve to itself; with no bundle/PATH
	// match either, ok is false (a real codegraph on PATH could make this true,
	// so only assert the override itself is not returned).
	missing := filepath.Join(t.TempDir(), "nope")
	if got, _ := Resolve(missing); got == missing {
		t.Fatalf("Resolve returned the missing override path %q", got)
	}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := expand("~/foo/bar"); got != filepath.Join(home, "foo", "bar") {
		t.Fatalf("expand(~/foo/bar) = %q", got)
	}
}

func TestEnsureInitSkipsWhenPresent(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	// bin points at nothing runnable; EnsureInit must short-circuit before exec.
	if err := EnsureInit(context.Background(), filepath.Join(root, "no-such-bin"), root); err != nil {
		t.Fatalf("EnsureInit with existing .codegraph should be a no-op, got %v", err)
	}
}

func TestEnsureInitRunsWhenAbsent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake launcher is a POSIX-sh script")
	}
	root := t.TempDir()
	// A fake codegraph that creates .codegraph in its working directory — EnsureInit
	// runs it with cmd.Dir = root, so this is independent of the exact arguments.
	bin := filepath.Join(t.TempDir(), "fakecg")
	writeExec(t, bin, "#!/bin/sh\nmkdir -p .codegraph\n")

	if err := EnsureInit(context.Background(), bin, root); err != nil {
		t.Fatalf("EnsureInit = %v", err)
	}
	if fi, err := os.Stat(filepath.Join(root, ".codegraph")); err != nil || !fi.IsDir() {
		t.Fatalf(".codegraph not created: err=%v", err)
	}
}

func TestEnsureInitPropagatesFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake launcher is a POSIX-sh script")
	}
	root := t.TempDir()
	bin := filepath.Join(t.TempDir(), "failcg")
	writeExec(t, bin, "#!/bin/sh\necho boom 1>&2\nexit 3\n")

	if err := EnsureInit(context.Background(), bin, root); err == nil {
		t.Fatal("EnsureInit should return the init failure")
	}
}

func TestDaemonPIDNoLock(t *testing.T) {
	root := t.TempDir()
	pid, ok := DaemonPID(root)
	if ok || pid != 0 {
		t.Fatalf("DaemonPID with no lock = %d, %v; want 0, false", pid, ok)
	}
}

func TestDaemonPIDEmptyLock(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codegraph", "daemon.pid"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	pid, ok := DaemonPID(root)
	if ok || pid != 0 {
		t.Fatalf("DaemonPID with empty lock = %d, %v; want 0, false", pid, ok)
	}
}

func TestDaemonPIDRejectsLegacyPlainPID(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codegraph", "daemon.pid"), []byte("12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pid, ok := DaemonPID(root)
	if ok || pid != 0 {
		t.Fatalf("DaemonPID with legacy plain PID = %d, %v; want 0, false", pid, ok)
	}
}

func TestDaemonPIDStructuredLock(t *testing.T) {
	root := t.TempDir()
	writeDaemonInfo(t, root, daemonInfo{
		PID:        12345,
		Version:    "0.9.7",
		SocketPath: filepath.Join(root, ".codegraph", "daemon.sock"),
		StartedAt:  123456789,
	})
	pid, ok := DaemonPID(root)
	if !ok || pid != 12345 {
		t.Fatalf("DaemonPID = %d, %v; want 12345, true", pid, ok)
	}
}

func TestDaemonPIDRejectsPartialLock(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codegraph", "daemon.pid"), []byte(`{"pid":42}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pid, ok := DaemonPID(root)
	if ok || pid != 0 {
		t.Fatalf("DaemonPID with partial lock = %d, %v; want 0, false", pid, ok)
	}
}

func TestKillDaemonNoLock(t *testing.T) {
	// Must not panic or error when there is no .codegraph directory at all.
	root := t.TempDir()
	KillDaemon(root) // no-op
}

func TestKillDaemonRequiresMatchingSocketHello(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix process/signal semantics only")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep command not available")
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		if processExists(cmd.Process.Pid) {
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})

	root := t.TempDir()
	writeDaemonInfo(t, root, daemonInfo{
		PID:        cmd.Process.Pid,
		Version:    "0.9.7",
		SocketPath: filepath.Join(root, ".codegraph", "missing.sock"),
		StartedAt:  123456789,
	})

	KillDaemon(root)
	if !processExists(cmd.Process.Pid) {
		t.Fatal("KillDaemon killed a process without a matching daemon socket hello")
	}
}

func TestKillDaemonKillsMatchingSocketDaemon(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket/process semantics only")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep command not available")
	}
	root := shortTempDir(t)
	cgDir := filepath.Join(root, ".codegraph")
	if err := os.Mkdir(cgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(cgDir, "daemon.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		if processExists(cmd.Process.Pid) {
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})
	writeDaemonInfo(t, root, daemonInfo{
		PID:        cmd.Process.Pid,
		Version:    "0.9.7",
		SocketPath: socketPath,
		StartedAt:  123456789,
	})

	accepted := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprintf(conn, `{"codegraph":"0.9.7","pid":%d,"socketPath":%q,"protocol":1}`+"\n", cmd.Process.Pid, socketPath)
		close(accepted)
	}()

	KillDaemon(root)
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("KillDaemon did not probe the daemon socket")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("KillDaemon did not kill the matching daemon process")
	}
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "reasonix-codegraph-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func writeDaemonInfo(t *testing.T, root string, info daemonInfo) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codegraph", "daemon.pid"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIndexableRootRejectsFilesystemRoots(t *testing.T) {
	if got := IndexableRoot(t.TempDir()); !got {
		t.Fatal("a real project dir must be indexable")
	}
	for _, root := range []string{"", "   "} {
		if IndexableRoot(root) {
			t.Fatalf("IndexableRoot(%q) = true; want false", root)
		}
	}
	var roots []string
	if runtime.GOOS == "windows" {
		roots = []string{`C:\`, `c:\`, `\\server\share`}
	} else {
		roots = []string{"/"}
	}
	for _, root := range roots {
		if IndexableRoot(root) {
			t.Fatalf("IndexableRoot(%q) = true; want false (filesystem root)", root)
		}
	}
}
