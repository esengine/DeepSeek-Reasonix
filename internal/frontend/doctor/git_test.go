package doctor

import (
	"os/exec"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// No git is a supported configuration, so the report states it as a fact and
// names what degrades. An empty Degraded list would leave the user guessing
// which half of the product just stopped working.
func TestCollectGitNamesWhatDegradesWhenAbsent(t *testing.T) {
	t.Setenv("PATH", testenv.TempDir(t))

	got := collectGit()
	if got.Available || got.Version != "" {
		t.Fatalf("git report = %+v, want unavailable", got)
	}
	if len(got.Degraded) == 0 {
		t.Fatal("no git and nothing named as degraded")
	}
	for _, item := range got.Degraded {
		if strings.TrimSpace(item) == "" {
			t.Fatalf("blank degraded entry in %+v", got)
		}
	}
}

// The version is rendered on its own line, so the "git version " prefix git
// prints would read as "version git version 2.50.1".
func TestCollectGitStripsTheVersionPrefix(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on this machine")
	}
	got := collectGit()
	if !got.Available || got.Version == "" {
		t.Fatalf("git report = %+v, want an available git with a version", got)
	}
	if strings.HasPrefix(got.Version, "git version") {
		t.Fatalf("version = %q, want the number without git's own prefix", got.Version)
	}
	if len(got.Degraded) != 0 {
		t.Fatalf("git is present yet %v is reported as degraded", got.Degraded)
	}
}
