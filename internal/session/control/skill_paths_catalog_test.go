package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/skill"
)

func pathCatalogSet(t *testing.T) skillSet {
	t.Helper()
	root := testenv.TempDir(t)
	for name, body := range map[string]string{
		"plain":  "---\nname: plain\ndescription: always here\n---\nB",
		"gogate": "---\nname: gogate\ndescription: go work\npaths: \"src/**/*.go\"\n---\nB",
		"broken": "---\nname: broken\ndescription: typo\npaths: \"!x\"\n---\nB",
	} {
		writeSkillFile(t, root, ".reasonix/skills/"+name+"/SKILL.md", body)
	}
	store := skill.New(skill.Options{HomeDir: testenv.TempDir(t), ProjectRoot: root, DisableBuiltins: true})
	return newSkillSet(nil, nil, store, store, false, root)
}

func TestCatalogListsAGatedSkillOnlyAfterItsPathIsTouched(t *testing.T) {
	set := pathCatalogSet(t)
	before := set.owedCatalog()
	if !strings.Contains(before, "plain") || strings.Contains(before, "gogate") || strings.Contains(before, "broken") {
		t.Fatalf("listing before any hit:\n%s", before)
	}
	set.catalog.settle()
	if again := set.owedCatalog(); again != "" {
		t.Fatalf("an unchanged state owed the listing again:\n%s", again)
	}
	if err := set.pathHits.Observe("src/pkg/a.go"); err != nil {
		t.Fatal(err)
	}
	after := set.owedCatalog()
	if !strings.Contains(after, "gogate") || !strings.Contains(after, "plain") || strings.Contains(after, "broken") {
		t.Fatalf("listing after a matching hit:\n%s", after)
	}
	set.catalog.settle()
	if again := set.owedCatalog(); again != "" {
		t.Fatalf("a settled listing was owed again:\n%s", again)
	}
}

func TestCatalogBytesDependOnlyOnTheCanonicalState(t *testing.T) {
	a, b := pathCatalogSet(t), pathCatalogSet(t)
	for _, p := range []string{"src/z.go", "src/a.go"} {
		_ = a.pathHits.Observe(p)
	}
	for _, p := range []string{"src/a.go", "src/z.go", "src/a.go"} {
		_ = b.pathHits.Observe(p)
	}
	ga, gb := a.owedCatalog(), b.owedCatalog()
	if ga == "" || strings.ReplaceAll(ga, "\r", "") != strings.ReplaceAll(gb, "\r", "") {
		t.Fatalf("same touched set, different listing:\n%s\n---\n%s", ga, gb)
	}
}

func TestCatalogSaysSoWhenTheSetShrinksUnderADeliveredListing(t *testing.T) {
	set := pathCatalogSet(t)
	root := set.pathHits
	_ = root.Observe("src/a.go")
	if got := set.owedCatalog(); !strings.Contains(got, "gogate") {
		t.Fatalf("setup: %s", got)
	}
	set.catalog.settle()
	root.Reset()
	got := set.owedCatalog()
	if strings.Contains(got, "gogate") || !strings.Contains(got, "plain") {
		t.Fatalf("after a reset the listing must drop the gated skill and keep the rest:\n%s", got)
	}
}

func TestCatalogWithOnlyGatedSkillsNamesTheCauseWhenNoneApply(t *testing.T) {
	root := testenv.TempDir(t)
	writeSkillFile(t, root, ".reasonix/skills/gogate/SKILL.md", "---\nname: gogate\ndescription: go work\npaths: \"*.go\"\n---\nB")
	store := skill.New(skill.Options{HomeDir: testenv.TempDir(t), ProjectRoot: root, DisableBuiltins: true})
	set := newSkillSet(nil, nil, store, store, false, root)
	if got := set.owedCatalog(); got != "" {
		t.Fatalf("nothing eligible and nothing sent should say nothing, got:\n%s", got)
	}
	_ = set.pathHits.Observe("main.go")
	if got := set.owedCatalog(); !strings.Contains(got, "gogate") {
		t.Fatalf("listing after the hit:\n%s", got)
	}
	set.catalog.settle()
	set.pathHits.Reset()
	got := set.owedCatalog()
	if !strings.Contains(got, "None of this project's skills apply") || strings.Contains(got, "switched off") {
		t.Fatalf("a gated-away listing must not read as every skill being switched off:\n%s", got)
	}
}

func TestSlashSurfaceIgnoresPathGating(t *testing.T) {
	set := pathCatalogSet(t)
	if _, ok := set.bySlashName("gogate"); !ok {
		t.Fatal("an explicit /gogate must resolve before any path is touched")
	}
	found := false
	for _, sk := range set.slashList() {
		found = found || sk.Name == "gogate"
	}
	if !found {
		t.Fatal("the user's slash list must carry the gated skill")
	}
}

func writeSkillFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
