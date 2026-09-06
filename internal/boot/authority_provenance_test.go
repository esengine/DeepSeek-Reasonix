package boot

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/sandbox"
)

// shortSocket listens on a path short enough for the ~104-byte sun_path limit,
// answering every connection with a marker.
func shortSocket(t *testing.T, marker string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rxp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Write([]byte(marker))
			c.Close()
		}
	}()
	return ln.Addr().String()
}

// TestProjectEnvCannotRedirectAuthorityEndpoints attacks the provenance of the
// endpoints a grant denotes. If a repository's .env could move DOCKER_HOST,
// "docker" would stop meaning the daemon and start meaning whatever socket the
// repository picked — a grant of one host capability silently becoming a grant
// of an arbitrary one.
func TestProjectEnvCannotRedirectAuthorityEndpoints(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no OS-level sandbox on Windows")
	}
	if !sandbox.Available() {
		t.Skip("no usable OS sandbox backend")
	}
	host := shortSocket(t, "HOST-DAEMON")
	attacker := shortSocket(t, "ATTACKER")

	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("DOCKER_HOST", "unix://"+host)
	env := "DOCKER_HOST=unix://" + attacker + "\nSSH_AUTH_SOCK=" + attacker + "\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DOCKER_HOST"); got != "unix://"+host {
		t.Fatalf("a project .env moved the process environment to %q; authority endpoints are no longer host-owned", got)
	}

	spec := sandbox.Spec{Mode: "enforce", WriteRoots: []string{root}, Network: true,
		HostAuthorities: sandbox.ParseAuthorities(cfg.Sandbox.HostAuthorities)}
	argv, wrapped := sandbox.Command(spec, sandbox.Shell{}, "true")
	if !wrapped {
		t.Fatal("spec did not wrap; the assertions below would measure nothing")
	}
	joined := strings.Join(argv, " ")
	// Positive control first: docker is denied by default, so the host daemon
	// must appear in the enforcement. Without it, "attacker absent" would hold
	// on a sandbox that governed nothing at all.
	if !strings.Contains(joined, host) {
		t.Fatalf("the host daemon socket is not governed at all, so this test proves nothing: %s", joined)
	}
	if strings.Contains(joined, attacker) {
		t.Errorf("a project .env chose an endpoint the sandbox acted on: %s", joined)
	}
}

// TestDeniedAuthorityIsNotInheritedThroughADescriptor attacks the other way
// past a path mask: a connection opened before the sandbox starts is already
// past every rule about opening one. Masking the path is only the whole
// boundary if no equivalent authority rides in on an open descriptor.
func TestDeniedAuthorityIsNotInheritedThroughADescriptor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no OS-level sandbox on Windows")
	}
	if !sandbox.Available() {
		t.Skip("no usable OS sandbox backend")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	sock := shortSocket(t, "AGENT-OK")
	t.Setenv("SSH_AUTH_SOCK", sock)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	file, err := conn.(*net.UnixConn).File()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	// The scan keeps looking: one instant's read races the peer's write, and a
	// miss then is the attacker having been early, not a boundary holding.
	scan := `import os,stat,time
deadline = time.time() + 5
while time.time() < deadline:
    for fd in range(3, 64):
        try: st = os.fstat(fd)
        except OSError: continue
        if not stat.S_ISSOCK(st.st_mode): continue
        try:
            os.set_blocking(fd, False)
            data = os.read(fd, 32)
            if data:
                print("FD-DATA", data.decode(errors="replace"))
                raise SystemExit(0)
        except OSError: pass
    time.sleep(0.05)`

	// Positive control: the same scan, unconfined, with the descriptor handed
	// over deliberately. It has to find the marker, or a clean result below
	// would only mean the scanner cannot see anything either way.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	control := exec.CommandContext(ctx, "python3", "-c", scan)
	control.ExtraFiles = []*os.File{file}
	if out, _ := control.CombinedOutput(); !strings.Contains(string(out), "AGENT-OK") {
		t.Fatalf("the descriptor scan cannot detect an inherited socket, so the check below is vacuous: %q", out)
	}

	spec := sandbox.Spec{Mode: "enforce", WriteRoots: []string{t.TempDir()}, Network: true}
	argv, wrapped := sandbox.CommandArgs(spec, []string{"python3", "-c", scan})
	if !wrapped {
		t.Fatal("spec did not wrap")
	}
	out, _ := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	if strings.Contains(string(out), "AGENT-OK") {
		t.Errorf("a denied authority was reachable through an inherited descriptor: %q", out)
	}
}
