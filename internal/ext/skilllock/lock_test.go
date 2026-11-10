package skilllock

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func putFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestADigestIsTheSameForACopyAndIgnoresTimeButNotContentOrNames(t *testing.T) {
	a, b := testenv.TempDir(t), testenv.TempDir(t)
	for _, root := range []string{a, b} {
		putFile(t, filepath.Join(root, "SKILL.md"), "---\ndescription: x\n---\nbody")
		putFile(t, filepath.Join(root, "scripts", "run.sh"), "echo hi")
	}
	if err := os.Chtimes(filepath.Join(b, "SKILL.md"), time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	da, _ := Digest(a)
	db, _ := Digest(b)
	if da == "" || da != db {
		t.Fatalf("a copy digests differently: %q vs %q", da, db)
	}
	putFile(t, filepath.Join(b, "scripts", "run.sh"), "echo bye")
	if changed, _ := Digest(b); changed == da {
		t.Fatal("a changed script did not change the digest")
	}
	putFile(t, filepath.Join(b, "scripts", "run.sh"), "echo hi")
	if err := os.Rename(filepath.Join(b, "scripts", "run.sh"), filepath.Join(b, "scripts", "go.sh")); err != nil {
		t.Fatal(err)
	}
	if renamed, _ := Digest(b); renamed == da {
		t.Fatal("a renamed file did not change the digest")
	}
}

func TestRecordReplacesAndForgetRemoves(t *testing.T) {
	home := testenv.TempDir(t)
	if err := Record(home, Entry{Name: "a", Scope: "global", Source: "/s/a", Mode: "copy", Digest: "d1"}); err != nil {
		t.Fatal(err)
	}
	if err := Record(home, Entry{Name: "a", Scope: "global", Source: "/s/a", Mode: "copy", Digest: "d2"}); err != nil {
		t.Fatal(err)
	}
	e, ok, err := Find(home, "a", "global")
	if err != nil || !ok || e.Digest != "d2" {
		t.Fatalf("Find = %+v %v %v, want the replaced entry", e, ok, err)
	}
	if err := Forget(home, "a", "global"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := Find(home, "a", "global"); ok {
		t.Fatal("a forgotten skill is still recorded")
	}
}

func TestADamagedLockIsReportedAndNeverReadAsEmpty(t *testing.T) {
	home := testenv.TempDir(t)
	putFile(t, Path(home), "{not json")
	if _, err := Entries(home); err == nil {
		t.Fatal("a damaged lock read as an empty one")
	}
	putFile(t, Path(home), `{"version":99,"entries":[]}`)
	if _, err := Entries(home); err == nil {
		t.Fatal("a lock from a newer build read as an empty one")
	}
	if entries, err := Entries(testenv.TempDir(t)); err != nil || len(entries) != 0 {
		t.Fatalf("a missing lock = %v %v, want nothing and no error", entries, err)
	}
	if err := Record(home, Entry{Name: "a", Scope: "global", Digest: "d"}); err != nil {
		t.Fatalf("recording over a damaged lock: %v", err)
	}
	if entries, err := Entries(home); err != nil || len(entries) != 1 {
		t.Fatalf("after recording: %v %v", entries, err)
	}
}

func TestAssessSaysWhichSideMoved(t *testing.T) {
	src, installed := testenv.TempDir(t), testenv.TempDir(t)
	for _, root := range []string{src, installed} {
		putFile(t, filepath.Join(root, "SKILL.md"), "v1")
	}
	digest, _ := Digest(src)
	e := Entry{Name: "a", Scope: "global", Source: src, Mode: "copy", Digest: digest}
	if got := Assess(e, installed); got != Synced {
		t.Fatalf("untouched = %s", got)
	}
	putFile(t, filepath.Join(src, "SKILL.md"), "v2")
	if got := Assess(e, installed); got != SourceChanged {
		t.Fatalf("source edited = %s", got)
	}
	putFile(t, filepath.Join(installed, "SKILL.md"), "local")
	if got := Assess(e, installed); got != Diverged {
		t.Fatalf("both edited = %s", got)
	}
	putFile(t, filepath.Join(src, "SKILL.md"), "v1")
	if got := Assess(e, installed); got != LocalEdited {
		t.Fatalf("installed edited = %s", got)
	}
	if got := Assess(Entry{Name: "a", Source: filepath.Join(src, "gone"), Mode: "copy", Digest: digest}, installed); got != SourceMissing {
		t.Fatalf("source gone = %s", got)
	}
}

func TestAssessNeverReadsWhatItCannotTrustAsInStep(t *testing.T) {
	installed := testenv.TempDir(t)
	putFile(t, filepath.Join(installed, "SKILL.md"), "v1")
	for name, e := range map[string]Entry{
		"no digest":     {Name: "a", Source: testenv.TempDir(t), Mode: "copy"},
		"no source":     {Name: "a", Mode: "copy", Digest: "d"},
		"remote source": {Name: "a", Source: "https://example.com/skill", Mode: "copy", Digest: "d"},
	} {
		if got := Assess(e, installed); got != Unknown {
			t.Errorf("%s = %s, want unknown", name, got)
		}
	}
	d, _ := Digest(installed)
	if got := Assess(Entry{Name: "a", Source: installed, Mode: "copy", Digest: d}, filepath.Join(installed, "missing")); got != Unknown {
		t.Errorf("an unreadable installed copy = %s, want unknown", got)
	}
}

func TestADigestIsBoundedAndAnOversizedSkillReadsAsUnknown(t *testing.T) {
	many := testenv.TempDir(t)
	for i := range MaxFiles + 1 {
		putFile(t, filepath.Join(many, "f", string(rune('a'+i%26)), strconv.Itoa(i)), "x")
	}
	if _, err := Digest(many); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a folder of %d files digested: %v", MaxFiles+1, err)
	}
	big := testenv.TempDir(t)
	putFile(t, filepath.Join(big, "SKILL.md"), "small")
	f, err := os.Create(filepath.Join(big, "blob"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := Digest(big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a folder past %d bytes digested: %v", MaxBytes, err)
	}
	installed := testenv.TempDir(t)
	putFile(t, filepath.Join(installed, "SKILL.md"), "v1")
	d, _ := Digest(installed)
	for name, src := range map[string]string{"too many files": many, "too many bytes": big} {
		if got := Assess(Entry{Name: "a", Source: src, Mode: "copy", Digest: d}, installed); got != Unknown {
			t.Errorf("%s = %s, want unknown", name, got)
		}
	}
	if _, err := Digest(installed); err != nil {
		t.Fatalf("an ordinary skill is refused: %v", err)
	}
}

// A copy materialises an in-folder link as the file it points at and leaves out
// .git and every other link, so the source and its copy must digest the same.
func TestTreeReadsAFolderTheWayAnInstallCopiesIt(t *testing.T) {
	root, outside := testenv.TempDir(t), testenv.TempDir(t)
	putFile(t, filepath.Join(root, "SKILL.md"), "body")
	putFile(t, filepath.Join(root, "real.txt"), "real")
	putFile(t, filepath.Join(root, ".git", "HEAD"), "ref")
	putFile(t, filepath.Join(outside, "secret"), "secret")
	if os.Symlink("real.txt", filepath.Join(root, "alias.txt")) != nil {
		t.Skip("no symlinks here")
	}
	_ = os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "escape"))
	_ = os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "broken"))
	_ = os.Symlink(".", filepath.Join(root, "loop"))
	files, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Rel)
	}
	want := []string{"SKILL.md", "alias.txt", "real.txt"}
	if len(got) != len(want) {
		t.Fatalf("tree = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tree = %v, want %v", got, want)
		}
	}
	copied := testenv.TempDir(t)
	putFile(t, filepath.Join(copied, "SKILL.md"), "body")
	putFile(t, filepath.Join(copied, "real.txt"), "real")
	putFile(t, filepath.Join(copied, "alias.txt"), "real")
	a, _ := Digest(root)
	b, _ := Digest(copied)
	if a == "" || a != b {
		t.Fatalf("a source and its materialised copy digest differently: %q vs %q", a, b)
	}
}
