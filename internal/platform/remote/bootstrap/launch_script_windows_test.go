package bootstrap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func init() {
	if os.Getenv("REASONIX_TEST_REMOTE_SERVE") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--port-file" {
			_ = os.WriteFile(os.Args[i+1], []byte("127.0.0.1:44321"), 0o600)
		}
	}
	fmt.Fprintln(os.Stderr, "fixture diagnostic")
	fmt.Println("fixture served")
	os.Exit(0)
}

func TestWindowsLaunchScriptRunsUnicodePaths(t *testing.T) {
	root := filepath.Join(testenv.TempDir(t), "中文 O'Hara & $literal %TEMP%")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "serve fixture.exe")
	if err := os.WriteFile(bin, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	paths := StatePaths{PortFile: filepath.Join(root, "serve.port"), PidFile: filepath.Join(root, "serve.pid"), LogFile: filepath.Join(root, "serve.log"), TokenFile: filepath.Join(root, "serve.token")}
	name, body := windowsShell{}.LaunchScript(LaunchSpec{Bin: bin, Workspace: root}, paths)
	if err := os.WriteFile(name, body, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", name)
	cmd.Env = append(os.Environ(), "REASONIX_TEST_REMOTE_SERVE=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script failed: %v: %s", err, out)
	}
	port, err := os.ReadFile(paths.PortFile)
	if err != nil || string(port) != "127.0.0.1:44321" {
		t.Fatalf("script did not publish the port at its literal path: %q, %v", port, err)
	}
	log, err := os.ReadFile(paths.LogFile)
	if err != nil || !strings.Contains(strings.ReplaceAll(string(log), "\x00", ""), "fixture diagnostic") || !strings.Contains(strings.ReplaceAll(string(log), "\x00", ""), "fixture served") {
		// Windows PowerShell redirects native streams as UTF-16LE.
		t.Fatalf("script did not capture both output streams: %q, %v", log, err)
	}
}
