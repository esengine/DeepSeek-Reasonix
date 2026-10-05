package boot

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
	"reasonix/internal/safety/sandbox"
)

func TestRuntimeForbidReadRootsCoverServeLaunchTokens(t *testing.T) {
	t.Setenv("REASONIX_HOME", filepath.Join(isolateConfigHome(t), "reasonix-home"))
	tokenFile := filepath.Join(robustTempDir(t), "serve.token")
	secrets.RegisterHostSecretPath(tokenFile)

	got := RuntimeForbidReadRoots(config.Default(), ".")
	for _, want := range []string{config.RemoteStateDir(), tokenFile} {
		if !pathListContains(got, want) {
			t.Fatalf("runtime forbid roots = %v, missing %s", got, want)
		}
	}
}

func TestSandboxedShellCannotReadServeLaunchTokens(t *testing.T) {
	if runtime.GOOS == "windows" || !sandbox.Available() {
		t.Skip("OS sandbox unavailable")
	}
	isolateConfigHome(t)
	// Every file sits inside the write root: Linux masks /tmp with a tmpfs, so a
	// file anywhere else under it would read as missing whether denied or not.
	work := robustTempDir(t)
	t.Setenv("REASONIX_HOME", filepath.Join(work, "reasonix-home"))
	remoteDir := config.RemoteStateDir()
	if err := os.MkdirAll(remoteDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const secret = "launch-token-sentinel"
	remoteToken := filepath.Join(remoteDir, "serve-demo.token")
	flagToken := filepath.Join(work, "serve.token")
	visible := filepath.Join(work, "visible.txt")
	for _, path := range []string{remoteToken, flagToken, visible} {
		if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	secrets.RegisterHostSecretPath(flagToken)

	spec := sandbox.Spec{Mode: "enforce", WriteRoots: []string{work}, ForbidReadRoots: RuntimeForbidReadRoots(config.Default(), work), Network: true}
	read := func(path string) (string, error) {
		argv, wrapped := sandbox.Command(spec, sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}, "cat "+path)
		if !wrapped {
			t.Fatal("sandbox did not wrap the command")
		}
		out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		return string(out), err
	}
	if out, err := read(visible); err != nil || !strings.Contains(out, secret) {
		t.Fatalf("control read = %q, %v; the sandbox must still read ordinary files", out, err)
	}
	for _, path := range []string{remoteToken, flagToken} {
		if out, err := read(path); err == nil || strings.Contains(out, secret) {
			t.Fatalf("sandboxed read of %s = %q, %v; want denied", path, out, err)
		}
	}
}
