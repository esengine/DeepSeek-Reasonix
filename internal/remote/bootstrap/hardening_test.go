package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/remote"
)

func TestEnsureServeRejectsStalePortFile(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
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
			return ok("/usr/bin/reasonix\nreasonix v9.9.0\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "nohup"):
			if strings.Contains(cmd, "rm -f "+shellQuote(paths.PortFile)) {
				_ = os.Remove(paths.PortFile) // model the generated launch command
			}
			return ok("12345\n") // the new serve never publishes a port
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
	root := t.TempDir()
	paths := pathsFor(root, root)
	var launches atomic.Int32
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("/usr/bin/reasonix\nreasonix v9.9.0\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "nohup"):
			launches.Add(1)
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:45123\n"), 0o600)
			return ok("321\n")
		case strings.Contains(cmd, "readlink /proc/321/exe"):
			return ok("yes\n")
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
	root := t.TempDir()
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
	_, _, err := ensureBinary(context.Background(), conn, conn.fs, Options{Install: InstallAuto}, root, "linux", "amd64", pathsFor(root, root))
	if err == nil {
		t.Fatal("auto install unexpectedly succeeded")
	}
	message := err.Error()
	if !strings.Contains(message, "npm install failed: permission denied") || !strings.Contains(message, "no local Reasonix CLI") {
		t.Fatalf("auto install hid the actionable failures: %v", err)
	}
}

func TestAutoInstallDownloadsVerifiedCrossPlatformBinaryAfterNPMFailure(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	uploaded := uploadedBinPath(root)
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "npm i -g reasonix"):
			return remote.ExecResult{Stdout: []byte("npm: command not found"), ExitCode: 127}, nil
		case strings.Contains(cmd, "BIN=; if [ -x "+shellQuote(uploaded)):
			return ok(uploaded + "\nreasonix v1.2.3\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("\n")
		default:
			return ok("")
		}
	})
	fetched := false
	bin, _, err := ensureBinary(context.Background(), conn, conn.fs, Options{
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

func TestUploadInstallProbesFreshBinaryBeforeStalePathCandidate(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	uploaded := uploadedBinPath(root)
	local := filepath.Join(root, "local-reasonix")
	if err := os.WriteFile(local, []byte("fresh-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	probedUpload := false
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("/usr/bin/reasonix\nreasonix v1.0.0\nportfile:yes\nsessionevents:no\ndetachedheal:no\ncaps:no\n")
		case strings.Contains(cmd, "BIN=; if [ -x "+shellQuote(uploaded)):
			probedUpload = true
			if _, err := os.Stat(uploaded); err != nil {
				t.Fatalf("uploaded probe ran before binary write: %v", err)
			}
			return ok(uploaded + "\nreasonix v9.9.0\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		default:
			return ok("")
		}
	})
	bin, _, err := ensureBinary(context.Background(), conn, conn.fs, Options{
		Install: InstallUpload, LocalBinary: local, LocalGOOS: "linux", LocalGOARCH: "amd64",
	}, root, "linux", "amd64", pathsFor(root, root))
	if err != nil {
		t.Fatal(err)
	}
	if !probedUpload || bin != uploaded {
		t.Fatalf("fresh upload result = bin:%q probed:%v, want %q/true", bin, probedUpload, uploaded)
	}
}

func TestNPMInstallProbesFreshGlobalBinaryBeforeStalePathCandidate(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	const installed = "/opt/npm/bin/reasonix"
	probedGlobal := false
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("/usr/bin/reasonix\nreasonix v1.0.0\nportfile:yes\nsessionevents:no\ndetachedheal:no\ncaps:no\n")
		case strings.Contains(cmd, "npm i -g reasonix"):
			return ok("installed\n")
		case strings.Contains(cmd, `BIN=; P="$(npm prefix -g 2>/dev/null)"`):
			probedGlobal = true
			return ok(installed + "\nreasonix v9.9.0\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		default:
			return ok("")
		}
	})
	bin, _, err := ensureBinary(context.Background(), conn, conn.fs, Options{Install: InstallNPM}, root, "linux", "amd64", pathsFor(root, root))
	if err != nil {
		t.Fatal(err)
	}
	if !probedGlobal || bin != installed {
		t.Fatalf("fresh npm result = bin:%q probed:%v, want %q/true", bin, probedGlobal, installed)
	}
}
