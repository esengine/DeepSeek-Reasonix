package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"reasonix/internal/remote"
	"reasonix/internal/remote/protocol"
)

func testWorkbenchBuild(t *testing.T) protocol.BuildID {
	t.Helper()
	id, err := protocol.NewBuildID("v1.2.3", strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestEnsureWorkbenchCLIAutoDownloadsCrossPlatformRelease(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	expected := testWorkbenchBuild(t)
	encoded, _ := json.Marshal(expected)
	managed := workbenchBinPath(root, expected)
	fetched := false
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname -sm"):
			return ok("Linux aarch64\n")
		case strings.Contains(cmd, "workbench-build-id") && strings.Contains(cmd, managed):
			if _, err := os.Stat(managed); err == nil {
				return ok(managed + "\n" + string(encoded) + "\n")
			}
			return ok("\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("bin/reasonix\n" + string(encoded) + "\n")
		default:
			return ok("")
		}
	})
	result, err := EnsureWorkbenchCLI(context.Background(), conn, WorkbenchOptions{
		Install: InstallAuto, LocalBinary: "/local/reasonix", LocalGOOS: "darwin", LocalGOARCH: "arm64",
		ExpectedBuild: expected,
		FetchBinary: func(_ context.Context, version, goos, goarch string) ([]byte, error) {
			fetched = true
			if version != "v1.2.3" || goos != "linux" || goarch != "arm64" {
				t.Fatalf("fetch target = %s %s/%s", version, goos, goarch)
			}
			return []byte("linux-arm64-cli"), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !fetched || !result.Installed || result.Path != managed {
		t.Fatalf("result=%+v fetched=%v", result, fetched)
	}
	data, err := os.ReadFile(managed)
	if err != nil || string(data) != "linux-arm64-cli" {
		t.Fatalf("managed binary = %q err=%v", data, err)
	}
}

func TestEnsureWorkbenchCLIReusesExactPATHBuild(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	expected := testWorkbenchBuild(t)
	encoded, _ := json.Marshal(expected)
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname -sm"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("/usr/local/bin/reasonix\n" + string(encoded) + "\n")
		default:
			return ok("\n")
		}
	})
	result, err := EnsureWorkbenchCLI(context.Background(), conn, WorkbenchOptions{Install: InstallNever, ExpectedBuild: expected})
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != "/usr/local/bin/reasonix" || result.Installed {
		t.Fatalf("result = %+v", result)
	}
}

func TestEnsureWorkbenchCLIReusesExactPATHBuildWithoutSFTP(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	expected := testWorkbenchBuild(t)
	encoded, _ := json.Marshal(expected)
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		if strings.Contains(cmd, "command -v reasonix") {
			return ok("/usr/local/bin/reasonix\n" + string(encoded) + "\n")
		}
		return ok("\n")
	})
	conn.sftpErr = errors.New("SFTP subsystem is disabled")

	result, err := EnsureWorkbenchCLI(context.Background(), conn, WorkbenchOptions{Install: InstallNever, ExpectedBuild: expected})
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != "/usr/local/bin/reasonix" || result.Installed {
		t.Fatalf("result = %+v", result)
	}
	if conn.ranContaining("uname -sm") {
		t.Fatal("exact PATH reuse unexpectedly probed the remote platform")
	}
}

func TestEnsureWorkbenchCLIRejectsStalePATHBuildWhenInstallNever(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	expected := testWorkbenchBuild(t)
	stale, _ := protocol.NewBuildID("v1.2.2", strings.Repeat("b", 40))
	encoded, _ := json.Marshal(stale)
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname -sm"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("/usr/bin/reasonix\n" + string(encoded) + "\n")
		default:
			return ok("\n")
		}
	})
	_, err := EnsureWorkbenchCLI(context.Background(), conn, WorkbenchOptions{Install: InstallNever, ExpectedBuild: expected})
	if err == nil || !strings.Contains(err.Error(), "serve_install = never") || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("error = %v", err)
	}
}

func TestEnsureWorkbenchCLIRejectsRelativePATHBuild(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	expected := testWorkbenchBuild(t)
	encoded, _ := json.Marshal(expected)
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		if strings.Contains(cmd, "command -v reasonix") {
			return ok("bin/reasonix\n" + string(encoded) + "\n")
		}
		return ok("\n")
	})

	_, err := EnsureWorkbenchCLI(context.Background(), conn, WorkbenchOptions{Install: InstallNever, ExpectedBuild: expected})
	if err == nil || !strings.Contains(err.Error(), "serve_install = never") || !strings.Contains(err.Error(), "non-absolute path") {
		t.Fatalf("error = %v", err)
	}
}

func TestWorkbenchProbeCommandQuotesManagedPath(t *testing.T) {
	cmd := WorkbenchProbeCommand("/tmp/a b/'quoted'/reasonix")
	if strings.Contains(cmd, "BIN=/tmp/a b") || !strings.Contains(cmd, `'\''`) {
		t.Fatalf("probe command is not safely quoted: %s", cmd)
	}
}
