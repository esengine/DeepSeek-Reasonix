package skill

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// The same unchanged file asked about again — which is what typing in the slash
// menu does, once per keystroke — is the same diagnostic, and an interactive
// screen cannot afford it twice.
func TestAnUnchangedDiagnosticIsWrittenOncePerStore(t *testing.T) {
	home := testenv.TempDir(t)
	writeSkill(t, home, ".claude/skills/named.md", "---\nname: renamed\n---\nbody")
	var stderr bytes.Buffer
	st := New(Options{HomeDir: home, DisableBuiltins: true, Stderr: &stderr})
	for range 5 {
		_ = st.SlashList()
		_ = st.List()
	}
	if got := strings.Count(stderr.String(), "has no description"); got != 1 {
		t.Fatalf("the missing-description warning was written %d times, want once:\n%s", got, stderr.String())
	}
}

func TestADiagnosticAboutAnotherFileIsStillWritten(t *testing.T) {
	home := testenv.TempDir(t)
	writeSkill(t, home, ".claude/skills/first.md", "---\nname: first\n---\nbody")
	var stderr bytes.Buffer
	st := New(Options{HomeDir: home, DisableBuiltins: true, Stderr: &stderr})
	now := time.Unix(1000, 0)
	st.scans.now = func() time.Time { return now }
	_ = st.SlashList()
	writeSkill(t, home, ".claude/skills/second.md", "---\nname: second\n---\nbody")
	// A file added outside the store shows once the memo of the slash list has aged out.
	now = now.Add(scanTTL)
	_ = st.SlashList()
	_ = st.SlashList()
	got := stderr.String()
	if strings.Count(got, `"first"`) != 1 || strings.Count(got, `"second"`) != 1 {
		t.Fatalf("each file's warning should appear exactly once:\n%s", got)
	}
}

func TestEachStoreStartsWithAFreshMemoryOfWhatItSaid(t *testing.T) {
	home := testenv.TempDir(t)
	writeSkill(t, home, ".claude/skills/named.md", "---\nname: renamed\n---\nbody")
	var a, b bytes.Buffer
	_ = New(Options{HomeDir: home, DisableBuiltins: true, Stderr: &a}).List()
	_ = New(Options{HomeDir: home, DisableBuiltins: true, Stderr: &b}).List()
	if !strings.Contains(a.String(), "has no description") || !strings.Contains(b.String(), "has no description") {
		t.Fatalf("a second store was silenced by the first: %q / %q", a.String(), b.String())
	}
}

// A file below the top level of a skills root that has no description is never
// loaded, so it cannot claim that it "will load"; only a skill that is kept says so.
func TestAnUnkeptNestedFileIsNotWarnedAboutAsALoadedSkill(t *testing.T) {
	home := testenv.TempDir(t)
	writeSkill(t, home, ".claude/skills/top.md", "---\nname: top\n---\nbody")
	writeSkill(t, home, ".claude/skills/pack/README.md", "---\nname: readme\n---\nnotes")
	writeSkill(t, home, ".claude/skills/pack/deep/README.md", "---\nname: readme\n---\nnotes")
	var stderr bytes.Buffer
	st := New(Options{HomeDir: home, DisableBuiltins: true, Stderr: &stderr})
	_ = st.List()
	got := stderr.String()
	if strings.Count(got, "has no description") != 1 || !strings.Contains(got, `"top"`) {
		t.Fatalf("want exactly the top-level skill warned about:\n%s", got)
	}
}
