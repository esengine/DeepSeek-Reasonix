package bootstrap

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remote"
)

func TestEnsureServeRejectsStalePortFile(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PortFile, []byte("127.0.0.1:49999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("bin /usr/bin/reasonix\nver reasonix v9.9.0\n" + allFlagsYes())
		case strings.Contains(cmd, "nohup"):
			return ok("12345\n") // the new serve never publishes a port
		case strings.Contains(cmd, "ps -p 12345"):
			return ok("1\n")
		default:
			return ok("")
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if res, err := EnsureServe(ctx, conn, Options{Workspace: "~"}); err == nil {
		t.Fatalf("accepted a stale port as a successful launch: %+v", res.State)
	}
}

func TestEnsureServeSerializesConcurrentClients(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	var launches atomic.Int32
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("bin /usr/bin/reasonix\nver reasonix v9.9.0\n" + allFlagsYes())
		case strings.Contains(cmd, "nohup"):
			launches.Add(1)
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:45123\n"), 0o600)
			return ok("321\n")
		case strings.Contains(cmd, "ps -p 321"):
			return ok("1\n")
		default:
			return ok("")
		}
	})
	type outcome struct {
		res Result
		err error
	}
	start := make(chan struct{})
	out := make(chan outcome, 2)
	for range 2 {
		go func() {
			<-start
			res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~"})
			out <- outcome{res: res, err: err}
		}()
	}
	close(start)
	var reused int
	for range 2 {
		got := <-out
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.res.Reused {
			reused++
		}
	}
	if got := launches.Load(); got != 1 || reused != 1 {
		t.Fatalf("launches=%d reused=%d, want 1/1", got, reused)
	}
}

func TestAutoInstallPreservesNPMFailureWhenNoUploadBinaryExists(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("\n")
		case strings.Contains(cmd, "npm i -g reasonix"):
			return remote.ExecResult{Stdout: []byte("permission denied"), ExitCode: 1}, nil
		default:
			return ok("")
		}
	})
	_, _, err := ensureBinary(context.Background(), conn, loginEnv{}, posixShell{}, conn.fs, Options{Install: InstallAuto}, root, "linux", "amd64", pathsFor(root, root))
	if err == nil {
		t.Fatal("auto install unexpectedly succeeded")
	}
	if !errors.Is(err, ErrNoInstallPath) {
		t.Fatalf("auto install did not say that no route is left: %v", err)
	}
	// Which routes failed survives the join: "there is no Node.js over there"
	// and "your binary is for another platform" are different next moves, and
	// a reader who only learns that both failed has been told nothing.
	if !errors.Is(err, ErrNPMUnavailable) || !errors.Is(err, ErrPlatformMismatch) {
		t.Fatalf("auto install lost which routes failed: %v", err)
	}
	// The remote's own complaint still rides along, for the log.
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("the remote's own words were dropped: %v", err)
	}
}

func TestAutoInstallDownloadsVerifiedCrossPlatformBinaryAfterNPMFailure(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	uploaded := uploadedBinPath(root, "reasonix")
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "npm i -g reasonix"):
			return remote.ExecResult{Stdout: []byte("npm: command not found"), ExitCode: 127}, nil
		case strings.Contains(cmd, "command -v reasonix"):
			if _, err := os.Stat(uploaded); err == nil {
				return ok("bin " + uploaded + "\nver reasonix v1.2.3\n" + allFlagsYes())
			}
			return ok("\n")
		default:
			return ok("")
		}
	})
	fetched := false
	bin, _, err := ensureBinary(context.Background(), conn, loginEnv{}, posixShell{}, conn.fs, Options{
		Install: InstallAuto, LocalBinary: "/local/reasonix", LocalGOOS: "darwin", LocalGOARCH: "arm64",
		ProductVersion: "v1.2.3",
		FetchBinary: func(_ context.Context, version, goos, goarch string) ([]byte, error) {
			fetched = true
			if version != "v1.2.3" || goos != "linux" || goarch != "amd64" {
				t.Fatalf("fetch target = %s %s/%s", version, goos, goarch)
			}
			return []byte("linux-amd64-cli"), nil
		},
	}, root, "linux", "amd64", pathsFor(root, root))
	if err != nil {
		t.Fatal(err)
	}
	if !fetched || bin != uploaded {
		t.Fatalf("bin=%q fetched=%v", bin, fetched)
	}
}
