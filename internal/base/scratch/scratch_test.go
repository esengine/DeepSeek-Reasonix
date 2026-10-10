package scratch

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const holderEnv = "REASONIX_SCRATCH_HOLDER"

func TestMain(m *testing.M) {
	if prefix := os.Getenv(holderEnv); prefix != "" {
		// The claim lives in d's file descriptor, which the runtime closes once d is unreachable.
		d, err := Create(prefix)
		if err != nil {
			os.Exit(2)
		}
		_ = os.WriteFile(filepath.Join(d.Path(), "payload"), []byte("x"), 0o600)
		os.Stdout.WriteString(d.Path() + "\n")
		for {
			runtime.GC()
			runtime.KeepAlive(d)
			time.Sleep(10 * time.Millisecond)
		}
	}
	os.Exit(m.Run())
}

func isolatedTemp(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	t.Setenv("TMP", root)
	t.Setenv("TEMP", root)
	return root
}

func age(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-2 * creationGrace)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func startHolder(t *testing.T, prefix string) (*exec.Cmd, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), holderEnv+"="+prefix)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("holder did not report its directory: %v", err)
	}
	return cmd, strings.TrimSpace(line)
}

func TestKilledOwnerLeavesDirectoryAndSweepCollectsIt(t *testing.T) {
	root := isolatedTemp(t)
	cmd, dir := startHolder(t, "scratch-kill-")
	age(t, dir)

	if got := Sweep(root, "scratch-kill-"); got != 0 {
		t.Fatalf("sweep removed %d directories while the owner was alive", got)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("live owner's directory is gone: %v", err)
	}

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("a killed owner cannot clean up, yet the directory vanished: %v", err)
	}
	age(t, dir)
	if got := Sweep(root, "scratch-kill-"); got != 1 {
		t.Fatalf("sweep removed %d directories, want the one abandoned by the killed owner", got)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("abandoned directory still present: %v", err)
	}
}

func TestSweepKeepsOwnedFreshAndForeignEntries(t *testing.T) {
	root := isolatedTemp(t)
	owned, err := Create("scratch-keep-")
	if err != nil {
		t.Fatal(err)
	}
	age(t, owned.Path())

	fresh := filepath.Join(root, "scratch-keep-fresh")
	if err := os.Mkdir(fresh, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "other-abandoned")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	age(t, foreign)
	file := filepath.Join(root, "scratch-keep-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	age(t, file)

	if got := Sweep(root, "scratch-keep-"); got != 0 {
		t.Fatalf("sweep removed %d entries, want 0", got)
	}
	for _, p := range []string{owned.Path(), fresh, foreign, file} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s was removed: %v", p, err)
		}
	}
	if err := owned.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(owned.Path()); !os.IsNotExist(err) {
		t.Fatalf("Remove left the directory: %v", err)
	}
	if err := owned.Remove(); err != nil {
		t.Fatalf("second Remove: %v", err)
	}
}

func TestCreateSweepsAbandonedSiblingsOncePerPrefix(t *testing.T) {
	root := isolatedTemp(t)
	stale := filepath.Join(root, "scratch-once-dead")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	age(t, stale)

	d, err := Create("scratch-once-")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Remove()
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("Create did not sweep the abandoned sibling: %v", err)
	}
}
