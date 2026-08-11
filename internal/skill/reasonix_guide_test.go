package skill_test

import (
	"strings"
	"testing"

	"reasonix/internal/skill"
)

func TestReasonixGuideBuiltinRegistered(t *testing.T) {
	store := skill.New(skill.Options{HomeDir: t.TempDir(), DisableBuiltins: false})
	sk, ok := store.Read("reasonix-guide")
	if !ok {
		t.Fatal("reasonix-guide must be registered as a builtin")
	}
	if sk.Scope != skill.ScopeBuiltin {
		t.Fatalf("scope = %s", sk.Scope)
	}
	if sk.RunAs != skill.RunInline {
		t.Fatalf("runAs = %s", sk.RunAs)
	}
	if sk.Description == "" {
		t.Fatal("description required for index line")
	}
	if !strings.Contains(sk.Body, "doctor capabilities") {
		t.Fatal("body missing doctor capabilities guidance")
	}
}

func TestReasonixGuideIndexLineOnly(t *testing.T) {
	store := skill.New(skill.Options{HomeDir: t.TempDir()})
	list := store.List()
	var guide skill.Skill
	found := false
	for _, s := range list {
		if s.Name == "reasonix-guide" {
			guide = s
			found = true
			break
		}
	}
	if !found {
		t.Fatal("reasonix-guide missing from List")
	}
	idx := skill.IndexBlock(list)
	if !strings.Contains(idx, "reasonix-guide") {
		t.Fatal("index missing reasonix-guide line")
	}
	// Body must not appear in the index block.
	if strings.Contains(idx, "First action") || strings.Contains(idx, skBodySnippet(guide)) {
		t.Fatal("skill body leaked into system-prompt index")
	}
	// Exactly one index line for the skill name.
	if c := strings.Count(idx, "- reasonix-guide"); c != 1 {
		t.Fatalf("index lines for reasonix-guide = %d, want 1", c)
	}
}

func skBodySnippet(sk skill.Skill) string {
	body := strings.TrimSpace(sk.Body)
	if len(body) > 40 {
		return body[:40]
	}
	return body
}

func TestReasonixGuideOverriddenByProject(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	store := skill.New(skill.Options{HomeDir: home, ProjectRoot: root})
	// Create project override.
	path, err := store.CreateWithContent("reasonix-guide", skill.ScopeProject, "---\ndescription: override\nrunAs: inline\n---\nproject body\n")
	if err != nil {
		t.Fatal(err)
	}
	_ = path
	store2 := skill.New(skill.Options{HomeDir: home, ProjectRoot: root})
	sk, ok := store2.Read("reasonix-guide")
	if !ok {
		t.Fatal("expected override")
	}
	if sk.Scope != skill.ScopeProject {
		t.Fatalf("scope = %s, want project", sk.Scope)
	}
	if !strings.Contains(sk.Body, "project body") {
		t.Fatalf("body = %q", sk.Body)
	}
}

func TestReasonixGuideDisabled(t *testing.T) {
	store := skill.New(skill.Options{
		HomeDir:       t.TempDir(),
		DisabledNames: []string{"reasonix-guide"},
	})
	if _, ok := store.Read("reasonix-guide"); ok {
		t.Fatal("disabled builtin should not be readable")
	}
	for _, s := range store.List() {
		if s.Name == "reasonix-guide" {
			t.Fatal("disabled builtin should not be listed")
		}
	}
}

func TestReasonixGuideIndexStableAcrossCalls(t *testing.T) {
	store := skill.New(skill.Options{HomeDir: t.TempDir()})
	a := skill.IndexBlock(store.List())
	b := skill.IndexBlock(store.List())
	if a != b {
		t.Fatal("skills index not byte-stable across List calls")
	}
}
