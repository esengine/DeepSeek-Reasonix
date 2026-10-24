package gitpush

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if address := os.Getenv("REASONIX_GIT_REPACK_PROBE"); address != "" && (filepath.Base(os.Args[0]) == "git" || filepath.Base(os.Args[0]) == "git.exe") {
		if slices.Contains(os.Args[1:], "pack-objects") {
			conn, err := net.Dial("tcp", address)
			if err != nil {
				os.Exit(90)
			}
			_, _ = io.Copy(io.Discard, conn)
			os.Exit(91)
		}
		cmd := exec.Command(os.Getenv("REASONIX_REAL_GIT"), os.Args[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				os.Exit(exit.ExitCode())
			}
			os.Exit(92)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestPushCancellationDuringObjectMaterializationReleasesScope(t *testing.T) {
	f := newFixture(t)
	plan, err := Preview(t.Context(), f.repo)
	if err != nil {
		t.Fatal(err)
	}
	before := f.remoteHead(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatal(err)
	}
	installRepackProbe(t, listener.Addr().String())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	released := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := PushWithScope(ctx, f.repo, plan, func(context.Context) (func(), error) {
			return func() { close(released) }, nil
		})
		done <- err
	}()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled repack lost cancellation identity: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled repack did not finish")
	}
	select {
	case <-released:
	default:
		t.Fatal("cancelled preparation retained source scope")
	}
	if f.remoteHead(t) != before {
		t.Fatal("cancelled preparation reached the remote")
	}
}

func installRepackProbe(t *testing.T, address string) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	dir, name := t.TempDir(), "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(target, source)
	if err := errors.Join(copyErr, target.Close()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_REAL_GIT", realGit)
	t.Setenv("REASONIX_GIT_REPACK_PROBE", address)
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
