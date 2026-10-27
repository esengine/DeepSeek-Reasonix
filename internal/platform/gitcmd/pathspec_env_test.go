package gitcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestHostPathspecsDoNotInheritSelectionOverrides(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := testenv.TempDir(t)
	if out, err := Command(t.Context(), dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "lower.txt"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := Command(t.Context(), dir, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("stage: %v %s", err, out)
	}
	for _, setting := range []string{"GIT_LITERAL_PATHSPECS", "GIT_GLOB_PATHSPECS", "GIT_NOGLOB_PATHSPECS", "GIT_ICASE_PATHSPECS"} {
		t.Run(setting, func(t *testing.T) {
			t.Setenv(setting, "1")
			out, err := Command(t.Context(), dir, "ls-files", "-z", "--", ":(literal)lower.txt").Output()
			if err != nil || string(out) != "lower.txt\x00" {
				t.Fatalf("inherited override changed host literal selection: %q %v", out, err)
			}
			out, err = Command(t.Context(), dir, "ls-files", "-z", "--", ":(literal)LOWER.txt").Output()
			if err != nil || len(out) != 0 {
				t.Fatalf("inherited override broadened an exact host selection: %q %v", out, err)
			}
			out, err = Command(t.Context(), dir, "--icase-pathspecs", "ls-files", "-z", "--", ":(literal)LOWER.txt").Output()
			if err != nil || string(out) != "lower.txt\x00" {
				t.Fatalf("explicit host-selected syntax stopped working: %q %v", out, err)
			}
		})
	}
}
