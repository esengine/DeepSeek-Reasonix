package skill

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestIssue4038ChineseNamedSkillLoads(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/中文技能/SKILL.md", "---\nname: 中文技能\ndescription: Chinese skill fixture\n---\nfixture body")
	store := New(Options{HomeDir: home, DisableBuiltins: true})
	if _, ok := find(store.List(), "中文技能"); !ok {
		t.Fatal("Chinese-named skill was not discovered")
	}
	if read, ok := store.Read("中文技能"); !ok || read.Body != "fixture body" {
		t.Fatalf("Chinese-named skill was not readable: %#v, %v", read, ok)
	}
	out, err := NewRunSkillTool(store, nil).Execute(context.Background(), json.RawMessage(`{"name":"中文技能"}`))
	if err != nil || !strings.Contains(out, "fixture body") {
		t.Fatalf("Chinese-named skill was not invokable: output=%q, error=%v", out, err)
	}
}

func TestIssue4038NFDDirectoryMatchesNFCName(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/cafe\u0301/SKILL.md", "---\ndescription: normalized skill fixture\n---\nfixture body")
	store := New(Options{HomeDir: home, DisableBuiltins: true})
	if _, ok := find(store.List(), "caf\u00e9"); !ok {
		t.Fatal("NFD directory did not expose an NFC skill name")
	}
	if _, ok := store.Read("caf\u00e9"); !ok {
		t.Fatal("NFC name could not read the NFD directory skill")
	}
	disabled := New(Options{HomeDir: home, DisableBuiltins: true, DisabledNames: func() []string { return []string{"caf\u00e9"} }})
	if _, ok := find(disabled.List(), "caf\u00e9"); ok {
		t.Fatal("NFC disabled name did not hide the NFD directory skill")
	}
}

func TestIssue4038ExistingASCIIIDSurvivesUnicodeFrontmatter(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/code-review/SKILL.md", "---\nname: 代码审查\ndescription: compatibility fixture\n---\nfixture body")
	store := New(Options{HomeDir: home, DisableBuiltins: true})
	if _, ok := store.Read("code-review"); !ok {
		t.Fatal("existing ASCII skill ID changed after Unicode frontmatter became valid")
	}
	if _, ok := store.ReadSlash("code-review"); !ok {
		t.Fatal("existing slash skill ID changed")
	}
	disabled := New(Options{HomeDir: home, DisableBuiltins: true, DisabledNames: func() []string { return []string{"code-review"} }})
	if _, ok := find(disabled.List(), "code-review"); ok {
		t.Fatal("disabled_skills entry stopped hiding the existing skill")
	}
}

func TestIssue4038CreateUsesNFCFolderName(t *testing.T) {
	store := New(Options{HomeDir: t.TempDir(), DisableBuiltins: true})
	path, err := store.CreateWithContent("cafe\u0301", ScopeGlobal, "---\ndescription: fixture\n---\nbody")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "café") {
		t.Fatalf("created path is not NFC: %q", path)
	}
}
