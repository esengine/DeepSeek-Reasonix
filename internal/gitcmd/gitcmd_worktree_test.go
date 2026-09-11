package gitcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestLocalFilterDriversReadsRealLinkedWorktreeConfigs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	main := t.TempDir()
	linked := filepath.Join(t.TempDir(), "linked")
	run := func(dir string, args ...string) {
		t.Helper()
		cmdArgs := append([]string{"-C", dir}, args...)
		if out, err := exec.Command("git", cmdArgs...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run(main, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(main, "tracked.txt"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(main, "add", "tracked.txt")
	run(main, "-c", "user.name=Reasonix Test", "-c", "user.email=reasonix@example.invalid", "commit", "--quiet", "-m", "initial")
	run(main, "config", "extensions.worktreeConfig", "true")
	run(main, "config", "filter.common.process", "malicious-process")
	run(main, "worktree", "add", "--quiet", "--detach", linked, "HEAD")
	run(linked, "config", "--worktree", "filter.local.clean", "malicious-clean")

	want := []string{"common", "local"}
	if got := localFilterDrivers(linked); !slices.Equal(got, want) {
		t.Fatalf("localFilterDrivers(real linked worktree) = %v, want %v", got, want)
	}
	args := argsFor("linux", linked, nil, "diff", "HEAD")
	for _, cfg := range []string{"filter.common.process=", "filter.local.process="} {
		if !hasConfig(args, cfg) {
			t.Fatalf("linked worktree diff args = %v, want -c %s", args, cfg)
		}
	}
}
