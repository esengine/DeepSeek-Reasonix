package skillupdate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/skilllock"
)

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// install lays down a recorded copy the way an install does: same files in the
// source and in the skills folder, and a lock entry holding their digest.
func install(t *testing.T) (Updater, string, string) {
	t.Helper()
	home, src := testenv.TempDir(t), testenv.TempDir(t)
	installed := filepath.Join(home, "skills", "mine")
	for _, dir := range []string{src, installed} {
		put(t, filepath.Join(dir, "SKILL.md"), "---\nname: mine\ndescription: d\n---\nv1\n")
		put(t, filepath.Join(dir, "scripts", "run.sh"), "echo one\n")
	}
	digest, err := skilllock.Digest(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := skilllock.Record(home, skilllock.Entry{Name: "mine", Scope: "global", Source: src, Mode: "copy", Digest: digest, InstalledAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return Updater{Home: home}, src, installed
}

func changesByPath(p Plan) map[string]Change {
	m := map[string]Change{}
	for _, c := range p.Changes {
		m[c.Path] = c
	}
	return m
}

func TestAPlanListsWhatTheUpdateWouldAddChangeAndRemove(t *testing.T) {
	u, src, _ := install(t)
	put(t, filepath.Join(src, "SKILL.md"), "---\nname: mine\ndescription: d\n---\nv2\n")
	put(t, filepath.Join(src, "scripts", "lint.py"), "print('x')\n")
	if err := os.Remove(filepath.Join(src, "scripts", "run.sh")); err != nil {
		t.Fatal(err)
	}
	p, err := u.Plan("mine")
	if err != nil {
		t.Fatal(err)
	}
	got := changesByPath(p)
	if len(got) != 3 || got["SKILL.md"].Kind != Changed || got["scripts/lint.py"].Kind != Added || got["scripts/run.sh"].Kind != Removed {
		t.Fatalf("plan = %+v", p.Changes)
	}
	if got["SKILL.md"].Code || !got["scripts/lint.py"].Code || !got["scripts/run.sh"].Code {
		t.Fatalf("only the Markdown document is plain instructions: %+v", p.Changes)
	}
	if !strings.Contains(got["SKILL.md"].Diff, "+v2") || !strings.Contains(got["scripts/lint.py"].Diff, "print('x')") || !strings.Contains(got["scripts/run.sh"].Diff, "echo one") {
		t.Fatalf("the plan does not show the content: %+v", p.Changes)
	}
	if p.State != string(skilllock.SourceChanged) || p.OverwritesLocal || p.PlanID == "" {
		t.Fatalf("plan header = %+v", p)
	}
}

func TestApplyBringsTheCopyInLineWithTheSourceAndRecordsIt(t *testing.T) {
	u, src, installed := install(t)
	put(t, filepath.Join(src, "SKILL.md"), "---\nname: mine\ndescription: d\n---\nv2\n")
	put(t, filepath.Join(src, "scripts", "lint.py"), "print('x')\n")
	if err := os.Remove(filepath.Join(src, "scripts", "run.sh")); err != nil {
		t.Fatal(err)
	}
	p, _ := u.Plan("mine")
	if err := u.Apply("mine", p.PlanID, false); err != nil {
		t.Fatal(err)
	}
	a, _ := skilllock.Digest(src)
	b, _ := skilllock.Digest(installed)
	if a != b {
		t.Fatal("the copy still differs from the source")
	}
	if _, err := os.Stat(filepath.Join(installed, "scripts", "run.sh")); !os.IsNotExist(err) {
		t.Fatal("a file the source dropped is still installed")
	}
	entry, _, _ := skilllock.Find(u.Home, "mine", "global")
	if skilllock.Assess(entry, installed) != skilllock.Synced {
		t.Fatalf("after the update the skill reads %s", skilllock.Assess(entry, installed))
	}
	if leftovers, _ := os.ReadDir(filepath.Join(u.Home, ".skill-update")); len(leftovers) != 0 {
		t.Fatalf("working files were left behind: %v", leftovers)
	}
	if again, _ := u.Plan("mine"); len(again.Changes) != 0 {
		t.Fatalf("a second plan still finds changes: %+v", again.Changes)
	}
}

func TestAPlanGoesStaleWhenAnySideMovesAfterItWasShown(t *testing.T) {
	for name, move := range map[string]func(t *testing.T, u Updater, src, installed string){
		"the source moved again": func(t *testing.T, _ Updater, src, _ string) {
			put(t, filepath.Join(src, "scripts", "run.sh"), "echo three\n")
		},
		"the copy was edited": func(t *testing.T, _ Updater, _, installed string) {
			put(t, filepath.Join(installed, "SKILL.md"), "edited\n")
		},
		"the record was swapped to another folder": func(t *testing.T, u Updater, src, _ string) {
			other := testenv.TempDir(t)
			for _, f := range []string{"SKILL.md", "scripts/run.sh"} {
				put(t, filepath.Join(other, f), read(t, filepath.Join(src, f)))
			}
			put(t, filepath.Join(other, "SKILL.md"), "---\nname: mine\ndescription: d\n---\nv2\n")
			e, _, _ := skilllock.Find(u.Home, "mine", "global")
			e.Source = other
			if err := skilllock.Record(u.Home, e); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			u, src, installed := install(t)
			put(t, filepath.Join(src, "SKILL.md"), "---\nname: mine\ndescription: d\n---\nv2\n")
			p, _ := u.Plan("mine")
			move(t, u, src, installed)
			before := read(t, filepath.Join(installed, "SKILL.md"))
			if err := u.Apply("mine", p.PlanID, true); !errors.Is(err, ErrStale) {
				t.Fatalf("Apply = %v, want ErrStale", err)
			}
			if read(t, filepath.Join(installed, "SKILL.md")) != before {
				t.Fatal("a stale plan changed the installed copy")
			}
		})
	}
}

func TestOverwritingAnEditedCopyNeedsAnExplicitConfirmation(t *testing.T) {
	u, src, installed := install(t)
	put(t, filepath.Join(src, "SKILL.md"), "---\nname: mine\ndescription: d\n---\nv2\n")
	put(t, filepath.Join(installed, "scripts", "run.sh"), "echo local edit\n")
	p, _ := u.Plan("mine")
	if !p.OverwritesLocal || p.State != string(skilllock.Diverged) {
		t.Fatalf("plan = %+v, want it to say the copy was edited too", p)
	}
	if err := u.Apply("mine", p.PlanID, false); !errors.Is(err, ErrNeedsConfirm) {
		t.Fatalf("Apply without confirmation = %v", err)
	}
	if read(t, filepath.Join(installed, "scripts", "run.sh")) != "echo local edit\n" {
		t.Fatal("an unconfirmed update overwrote the local edit")
	}
	if err := u.Apply("mine", p.PlanID, true); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(installed, "scripts", "run.sh")) != "echo one\n" {
		t.Fatal("a confirmed update did not bring the copy to the source")
	}
}

// A failure while swapping the new copy in leaves the old copy exactly as it
// was, byte for byte, and no working files behind.
func TestAFailedSwapLeavesTheOldCopyIntact(t *testing.T) {
	u, src, installed := install(t)
	put(t, filepath.Join(src, "SKILL.md"), "---\nname: mine\ndescription: d\n---\nv2\n")
	put(t, filepath.Join(src, "scripts", "lint.py"), "print('x')\n")
	oldDigest, _ := skilllock.Digest(installed)
	oldSkill := read(t, filepath.Join(installed, "SKILL.md"))
	p, _ := u.Plan("mine")

	calls := 0
	rename = func(from, to string) error {
		calls++
		if calls == 2 {
			return errors.New("the volume went away")
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { rename = os.Rename })
	if err := u.Apply("mine", p.PlanID, false); !errors.Is(err, ErrFailed) {
		t.Fatalf("Apply = %v, want ErrFailed", err)
	}
	if got, _ := skilllock.Digest(installed); got != oldDigest || read(t, filepath.Join(installed, "SKILL.md")) != oldSkill {
		t.Fatal("the old copy was not left as it was")
	}
	if _, err := os.Stat(filepath.Join(installed, "scripts", "lint.py")); err == nil {
		t.Fatal("half of the new copy is installed")
	}
	if leftovers, _ := os.ReadDir(filepath.Join(u.Home, ".skill-update")); len(leftovers) != 0 {
		t.Fatalf("working files were left behind: %v", leftovers)
	}
	entry, _, _ := skilllock.Find(u.Home, "mine", "global")
	if skilllock.Assess(entry, installed) != skilllock.SourceChanged {
		t.Fatal("the record moved although nothing was installed")
	}
}

func TestOnlyACopyFromALocalFolderCanBeUpdated(t *testing.T) {
	u, _, _ := install(t)
	for name, want := range map[string]error{
		"nope":  ErrNotRecorded,
		"../x":  ErrBadName,
		"a/b":   ErrBadName,
		"":      ErrBadName,
		"mine ": ErrBadName,
	} {
		if _, err := u.Plan(name); !errors.Is(err, want) {
			t.Errorf("Plan(%q) = %v, want %v", name, err, want)
		}
	}
	e, _, _ := skilllock.Find(u.Home, "mine", "global")
	for name, mutate := range map[string]func(*skilllock.Entry){
		"a link install":    func(e *skilllock.Entry) { e.Mode = "link" },
		"a remote source":   func(e *skilllock.Entry) { e.Source = "https://example.com/s" },
		"a relative source": func(e *skilllock.Entry) { e.Source = "skills/mine" },
	} {
		c := e
		mutate(&c)
		if err := skilllock.Record(u.Home, c); err != nil {
			t.Fatal(err)
		}
		if _, err := u.Plan("mine"); !errors.Is(err, ErrNotLocal) {
			t.Errorf("%s: Plan = %v, want ErrNotLocal", name, err)
		}
	}
	e.Source = filepath.Join(testenv.TempDir(t), "gone")
	_ = skilllock.Record(u.Home, e)
	if _, err := u.Plan("mine"); !errors.Is(err, ErrSourceMissing) {
		t.Errorf("a vanished source: %v", err)
	}
}

func TestAnUpdateNeverCopiesWhatAnInstallWouldNot(t *testing.T) {
	u, src, installed := install(t)
	outside := testenv.TempDir(t)
	put(t, filepath.Join(outside, "secret"), "secret")
	put(t, filepath.Join(src, ".git", "HEAD"), "ref")
	if os.Symlink(filepath.Join(outside, "secret"), filepath.Join(src, "escape")) != nil {
		t.Skip("no symlinks here")
	}
	put(t, filepath.Join(src, "SKILL.md"), "---\nname: mine\ndescription: d\n---\nv2\n")
	p, _ := u.Plan("mine")
	if len(p.Changes) != 1 || p.Changes[0].Path != "SKILL.md" {
		t.Fatalf("plan = %+v, want only SKILL.md", p.Changes)
	}
	if err := u.Apply("mine", p.PlanID, false); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".git", "escape"} {
		if _, err := os.Lstat(filepath.Join(installed, rel)); err == nil {
			t.Errorf("%s was copied into the skill", rel)
		}
	}
}

func TestATooLargeSkillIsRefusedNotTruncated(t *testing.T) {
	u, src, _ := install(t)
	for i := range skilllock.MaxFiles + 1 {
		put(t, filepath.Join(src, "bulk", strings.Repeat("d", 1+i%7), string(rune('a'+i%26))+"-"+string(rune('A'+i/26))), "x")
	}
	if _, err := u.Plan("mine"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Plan = %v, want ErrTooLarge", err)
	}
}

func TestAPlanKeepsWhatItShowsBounded(t *testing.T) {
	u, src, _ := install(t)
	big := strings.Repeat("line of a long script\n", 4000)
	put(t, filepath.Join(src, "scripts", "big.sh"), big)
	p, err := u.Plan("mine")
	if err != nil {
		t.Fatal(err)
	}
	c := changesByPath(p)["scripts/big.sh"]
	if !c.Truncated || c.Diff != "" {
		t.Fatalf("an oversized file must be marked truncated and not shown cut: %+v", c)
	}
}

func TestARecordWriteThatFailsOnceIsRetriedAndOneThatKeepsFailingIsHealedByTheNextApply(t *testing.T) {
	u, src, installed := install(t)
	put(t, filepath.Join(src, "SKILL.md"), "---\nname: mine\ndescription: d\n---\nv2\n")
	p, _ := u.Plan("mine")

	recordRetryPause = 0
	real := recordEntry
	t.Cleanup(func() { recordEntry = real })
	calls := 0
	recordEntry = func(home string, e skilllock.Entry) error {
		calls++
		if calls == 1 {
			return errors.New("disk busy")
		}
		return real(home, e)
	}
	if err := u.Apply("mine", p.PlanID, false); err != nil {
		t.Fatalf("a record that fails once: %v", err)
	}
	if e, _, _ := skilllock.Find(u.Home, "mine", "global"); skilllock.Assess(e, installed) != skilllock.Synced {
		t.Fatal("the retried record did not leave the skill in step")
	}

	put(t, filepath.Join(src, "SKILL.md"), "---\nname: mine\ndescription: d\n---\nv3\n")
	p, _ = u.Plan("mine")
	recordEntry = func(string, skilllock.Entry) error { return errors.New("disk gone") }
	if err := u.Apply("mine", p.PlanID, false); !errors.Is(err, ErrFailed) || !strings.Contains(err.Error(), "the copy is updated") {
		t.Fatalf("a record that keeps failing = %v, want ErrFailed saying the copy is updated", err)
	}
	if !strings.Contains(read(t, filepath.Join(installed, "SKILL.md")), "v3") {
		t.Fatal("the copy was not updated")
	}

	recordEntry = real
	healed, _ := u.Plan("mine")
	if len(healed.Changes) != 0 {
		t.Fatalf("the copy already matches the source: %+v", healed.Changes)
	}
	if err := u.Apply("mine", healed.PlanID, true); err != nil {
		t.Fatal(err)
	}
	if e, _, _ := skilllock.Find(u.Home, "mine", "global"); skilllock.Assess(e, installed) != skilllock.Synced {
		t.Fatal("applying again did not bring the record up to date")
	}
}
