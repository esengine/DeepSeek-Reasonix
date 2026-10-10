package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/skill"
)

func TestInvokedSkillCarriesReferencesFromAListing(t *testing.T) {
	home := testenv.TempDir(t)
	dir := filepath.Join(home, ".reasonix", "skills", "deep")
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\ndescription: deep\n---\nmain"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "r.md"), []byte("reference text"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := skill.New(skill.Options{HomeDir: home, DisableBuiltins: true})

	live, listing := newSkillSet(nil, nil, st, st, false, ""), newSkillSet(st.List(), nil, nil, nil, false, "")
	for name, set := range map[string]*skillSet{"live store": &live, "listing only": &listing} {
		listed := set.list()
		if len(listed) != 1 {
			t.Fatalf("%s: listed %d skills", name, len(listed))
		}
		if got := set.renderInvocation(listed[0], ""); !strings.Contains(got, "reference text") {
			t.Errorf("%s: invocation lost the references: %q", name, got)
		}
	}
}

func TestSkillSetRecordsPathsRelativeToTheWorkspace(t *testing.T) {
	root := t.TempDir()
	set := newSkillSet(nil, nil, nil, nil, false, root)
	if err := set.pathHits.Observe(filepath.Join(root, "src", "a.go")); err != nil {
		t.Fatal(err)
	}
	if !set.pathHits.Eligible(skill.Skill{Name: "go", Paths: []string{"src/*.go"}}) {
		t.Fatalf("a touched workspace file did not make its skill eligible: %q", set.pathHits.Seen())
	}
}
