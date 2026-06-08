package skill

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func writeSkill(t *testing.T, base, rel, content string) string {
	t.Helper()
	full := filepath.Join(base, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func find(skills []Skill, name string) (Skill, bool) {
	for _, s := range skills {
		if s.Name == name {
			return s, true
		}
	}
	return Skill{}, false
}

func TestListPrecedenceProjectOverGlobal(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	writeSkill(t, proj, ".reasonix/skills/greet.md", "---\nname: greet\ndescription: project greet\n---\nproject body")
	writeSkill(t, home, ".reasonix/skills/greet.md", "---\ndescription: global greet\n---\nglobal body")
	writeSkill(t, home, ".reasonix/skills/onlyglobal.md", "---\ndescription: only global\n---\nbody")

	st := New(Options{HomeDir: home, ProjectRoot: proj, DisableBuiltins: true})
	list := st.List()

	greet, ok := find(list, "greet")
	if !ok {
		t.Fatal("greet not found")
	}
	if greet.Scope != ScopeProject || greet.Description != "project greet" {
		t.Fatalf("project skill should win: got scope=%s desc=%q", greet.Scope, greet.Description)
	}
	if _, ok := find(list, "onlyglobal"); !ok {
		t.Fatal("global-only skill should be discovered")
	}
}

func TestFlatAndDirLayout(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/flat.md", "---\ndescription: flat\n---\nflat body")
	writeSkill(t, home, ".reasonix/skills/dir/SKILL.md", "---\ndescription: dir\n---\ndir body")

	st := New(Options{HomeDir: home, DisableBuiltins: true})
	list := st.List()
	if _, ok := find(list, "flat"); !ok {
		t.Error("flat <name>.md skill not discovered")
	}
	if _, ok := find(list, "dir"); !ok {
		t.Error("dir/SKILL.md skill not discovered")
	}
}

func TestNestedSkillsDiscoveredByDefault(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/superpower/skill-a.md", "---\ndescription: nested flat\n---\nflat body")
	writeSkill(t, home, ".reasonix/skills/superpower/tool-a/SKILL.md", "---\ndescription: nested dir\n---\ndir body")
	writeSkill(t, home, ".reasonix/skills/superpower/references/notes.md", "---\ndescription: not a skill\n---\nnotes")

	st := New(Options{HomeDir: home, DisableBuiltins: true})
	list := st.List()
	if _, ok := find(list, "skill-a"); !ok {
		t.Fatal("default max depth should discover nested flat skills")
	}
	if _, ok := find(list, "tool-a"); !ok {
		t.Fatal("default max depth should discover nested directory skills")
	}
	if _, ok := find(list, "notes"); ok {
		t.Fatal("references directories should not be scanned as skill roots")
	}
	if sk, ok := st.Read("skill-a"); !ok || sk.Description != "nested flat" || !strings.Contains(sk.Body, "flat body") {
		t.Fatalf("Read should resolve nested skills from the same discovery path: %+v ok=%v", sk, ok)
	}
}

func TestMaxDepthOnePreservesRootOnlyDiscovery(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/superpower/skill-a.md", "---\ndescription: nested flat\n---\nflat body")
	writeSkill(t, home, ".reasonix/skills/superpower/tool-a/SKILL.md", "---\ndescription: nested dir\n---\ndir body")

	st := New(Options{HomeDir: home, MaxDepth: 1, DisableBuiltins: true})
	if _, ok := find(st.List(), "skill-a"); ok {
		t.Fatal("max depth 1 should not discover nested flat skills")
	}
	if _, ok := find(st.List(), "tool-a"); ok {
		t.Fatal("max depth 1 should not discover nested directory skills")
	}
}

func TestNestedSkillsRequireDescription(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/root.md", "---\n---\nroot body")
	writeSkill(t, home, ".reasonix/skills/superpower/draft.md", "---\n---\ndraft body")
	writeSkill(t, home, ".reasonix/skills/superpower/tool/SKILL.md", "---\n---\ntool body")

	st := New(Options{HomeDir: home, DisableBuiltins: true})
	list := st.List()
	if _, ok := find(list, "root"); !ok {
		t.Fatal("root-level skills without description should keep legacy discovery behavior")
	}
	if _, ok := find(list, "draft"); ok {
		t.Fatal("nested flat skills without description should be ignored")
	}
	if _, ok := find(list, "tool"); ok {
		t.Fatal("nested directory skills without description should be ignored")
	}
	if _, ok := st.Read("draft"); ok {
		t.Fatal("Read should not resolve nested skills filtered for missing description")
	}
}

func TestNestedDirectorySkillStopsTraversal(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/pack/SKILL.md", "---\ndescription: pack\n---\npack body")
	writeSkill(t, home, ".reasonix/skills/pack/child.md", "---\ndescription: child\n---\nchild body")

	st := New(Options{HomeDir: home, MaxDepth: 3, DisableBuiltins: true})
	if _, ok := find(st.List(), "pack"); !ok {
		t.Fatal("directory-layout skill should be discovered")
	}
	if _, ok := find(st.List(), "child"); ok {
		t.Fatal("directory-layout skill packages should not be scanned for child skills")
	}
}

func TestConventionDirsDiscovered(t *testing.T) {
	proj := t.TempDir()
	writeSkill(t, proj, ".claude/skills/fromclaude.md", "---\ndescription: c\n---\nb")
	writeSkill(t, proj, ".agents/skills/fromagents.md", "---\ndescription: a\n---\nb")
	writeSkill(t, proj, ".agent/skills/fromagent.md", "---\ndescription: s\n---\nb")
	st := New(Options{HomeDir: t.TempDir(), ProjectRoot: proj, DisableBuiltins: true})
	list := st.List()
	for _, name := range []string{"fromclaude", "fromagents", "fromagent"} {
		if _, ok := find(list, name); !ok {
			t.Errorf("convention dir for %q not scanned", name)
		}
	}
}

func TestExcludedPathsHideConventionRoots(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/keep.md", "---\ndescription: keep\n---\nb")
	writeSkill(t, home, ".agents/skills/noisy.md", "---\ndescription: noisy\n---\nb")
	excluded := filepath.Join(home, ".agents", "skills")
	st := New(Options{HomeDir: home, ExcludedPaths: []string{excluded}, DisableBuiltins: true})

	if _, ok := find(st.List(), "keep"); !ok {
		t.Fatal("non-excluded skill should be listed")
	}
	if _, ok := find(st.List(), "noisy"); ok {
		t.Fatal("excluded skill should not be listed")
	}
	for _, root := range st.Roots() {
		if config.CanonicalSkillPath(root.Dir) == config.CanonicalSkillPath(excluded) {
			t.Fatalf("excluded root should be hidden from Roots: %+v", st.Roots())
		}
	}
}

func TestFrontmatterFields(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/sub.md",
		"---\ndescription: a sub\nrunAs: subagent\nallowed-tools: read_file, grep\nmodel: deepseek-pro\n---\nbody")
	writeSkill(t, home, ".reasonix/skills/fork.md", "---\ndescription: f\ncontext: fork\n---\nbody")
	writeSkill(t, home, ".reasonix/skills/plain.md", "---\ndescription: p\n---\nbody")

	st := New(Options{HomeDir: home, DisableBuiltins: true})
	sub, _ := st.Read("sub")
	if sub.RunAs != RunSubagent {
		t.Error("runAs: subagent not parsed")
	}
	if len(sub.AllowedTools) != 2 || sub.AllowedTools[0] != "read_file" || sub.AllowedTools[1] != "grep" {
		t.Errorf("allowed-tools mis-parsed: %v", sub.AllowedTools)
	}
	if sub.Model != "deepseek-pro" {
		t.Errorf("model mis-parsed: %q", sub.Model)
	}
	if fork, _ := st.Read("fork"); fork.RunAs != RunSubagent {
		t.Error("context: fork should imply subagent")
	}
	if plain, _ := st.Read("plain"); plain.RunAs != RunInline {
		t.Error("default runAs should be inline")
	}
}

func TestReferencesInlined(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/withrefs/SKILL.md", "---\ndescription: r\n---\nmain body")
	writeSkill(t, home, ".reasonix/skills/withrefs/references/b.md", "second ref")
	writeSkill(t, home, ".reasonix/skills/withrefs/references/a.md", "first ref")

	st := New(Options{HomeDir: home, DisableBuiltins: true})
	sk, ok := st.Read("withrefs")
	if !ok {
		t.Fatal("skill not found")
	}
	if !strings.Contains(sk.Body, "main body") {
		t.Error("main body missing")
	}
	// references are appended sorted by filename: a before b.
	ai := strings.Index(sk.Body, "## Reference: a")
	bi := strings.Index(sk.Body, "## Reference: b")
	if ai < 0 || bi < 0 || ai > bi {
		t.Errorf("references not appended in sorted order: a=%d b=%d", ai, bi)
	}
	if !strings.Contains(sk.Body, "first ref") || !strings.Contains(sk.Body, "second ref") {
		t.Error("reference contents missing")
	}
}

func TestBuiltinInitIsInlineSkill(t *testing.T) {
	// /init must resolve to a built-in inline skill (the model-driven AGENTS.md
	// bootstrap), present even with no project/user skills on disk.
	st := New(Options{HomeDir: t.TempDir()})
	sk, ok := st.Read("init")
	if !ok {
		t.Fatal("built-in init skill not found")
	}
	if sk.Scope != ScopeBuiltin || sk.RunAs != RunInline {
		t.Errorf("init should be a builtin inline skill, got scope=%s runAs=%s", sk.Scope, sk.RunAs)
	}
	if _, listed := find(st.List(), "init"); !listed {
		t.Error("init should appear in List() so it reaches the slash menu")
	}
}

func TestBuiltinSubagentSkillsDeclareAllowedTools(t *testing.T) {
	st := New(Options{HomeDir: t.TempDir()})
	cases := map[string][]string{
		"explore":         {"read_file", "ls", "glob", "grep"},
		"research":        {"read_file", "ls", "glob", "grep", "web_fetch"},
		"review":          {"read_file", "ls", "glob", "grep", "bash"},
		"security-review": {"read_file", "ls", "glob", "grep", "bash"},
	}
	for name, want := range cases {
		sk, ok := st.Read(name)
		if !ok {
			t.Fatalf("built-in %s skill not found", name)
		}
		if sk.RunAs != RunSubagent {
			t.Fatalf("%s RunAs = %s, want subagent", name, sk.RunAs)
		}
		if !sameStrings(sk.AllowedTools, want) {
			t.Errorf("%s AllowedTools = %v, want %v", name, sk.AllowedTools, want)
		}
		for _, meta := range []string{"task", "run_skill", "install_skill", "install_source", "explore", "research", "review", "security_review"} {
			if containsString(sk.AllowedTools, meta) {
				t.Errorf("%s AllowedTools should not include meta-tool %q: %v", name, meta, sk.AllowedTools)
			}
		}
	}
}

func TestBuiltinsPresentAndOverridable(t *testing.T) {
	st := New(Options{HomeDir: t.TempDir()})
	if _, ok := find(st.List(), "explore"); !ok {
		t.Error("built-in explore should be present")
	}
	// A user file named after a built-in overrides it.
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/explore.md", "---\ndescription: mine\nrunAs: inline\n---\nbody")
	st2 := New(Options{HomeDir: home})
	ex, _ := st2.Read("explore")
	if ex.Scope == ScopeBuiltin || ex.Description != "mine" {
		t.Errorf("user explore should override builtin: scope=%s desc=%q", ex.Scope, ex.Description)
	}
}

func TestInstallCapabilityBuiltinIsInlineWithExpectedMetadata(t *testing.T) {
	st := New(Options{HomeDir: t.TempDir()})
	sk, ok := st.Read("install-capability")
	if !ok {
		t.Fatal("install-capability builtin skill must be registered")
	}
	if sk.Scope != ScopeBuiltin {
		t.Errorf("install-capability scope = %s, want builtin", sk.Scope)
	}
	if sk.RunAs != RunInline {
		t.Errorf("install-capability runAs = %s, want inline (it folds into the parent turn)", sk.RunAs)
	}
	if !strings.Contains(sk.Description, "install_source") {
		t.Errorf("description should mention install_source, got %q", sk.Description)
	}
	if !strings.Contains(sk.Description, "uninstall") {
		t.Errorf("description should advertise op=uninstall, got %q", sk.Description)
	}
	if !strings.Contains(sk.Body, "riskLevel") {
		t.Error("body should mention the per-action riskLevel field so the model reads it")
	}
	if !strings.Contains(sk.Body, "planId") {
		t.Error("body should mention the planId echo requirement on apply=true")
	}
}

func TestDisabledSkillsAreFilteredFromListAndRead(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/active.md", "---\ndescription: active\n---\nbody")
	writeSkill(t, home, ".reasonix/skills/hidden.md", "---\ndescription: hidden\n---\nbody")

	st := New(Options{HomeDir: home, DisabledNames: []string{"hidden", "review"}})
	if _, ok := find(st.List(), "active"); !ok {
		t.Fatal("active skill should be listed")
	}
	if _, ok := find(st.List(), "hidden"); ok {
		t.Fatal("disabled file skill should not be listed")
	}
	if _, ok := st.Read("hidden"); ok {
		t.Fatal("disabled file skill should not be readable")
	}
	if _, ok := find(st.List(), "review"); ok {
		t.Fatal("disabled builtin skill should not be listed")
	}
	if _, ok := st.Read("review"); ok {
		t.Fatal("disabled builtin skill should not be readable")
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestInvalidNamesSkipped(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/bad name.md", "---\ndescription: x\n---\nb") // space → invalid
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	if len(st.List()) != 0 {
		t.Errorf("invalid-named skill should be skipped, got %d", len(st.List()))
	}
}

func TestSymlinkedDirAndFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on Windows")
	}
	home := t.TempDir()
	target := t.TempDir()
	// real skill dir + flat file living outside the skills root
	writeSkill(t, target, "realdir/SKILL.md", "---\ndescription: linked dir\n---\nb")
	writeSkill(t, target, "realflat.md", "---\ndescription: linked flat\n---\nb")

	skillsRoot := filepath.Join(home, ".reasonix", "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(target, "realdir"), filepath.Join(skillsRoot, "linkeddir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(target, "realflat.md"), filepath.Join(skillsRoot, "linkedflat.md")); err != nil {
		t.Fatal(err)
	}

	st := New(Options{HomeDir: home, DisableBuiltins: true})
	list := st.List()
	if _, ok := find(list, "linkeddir"); !ok {
		t.Error("symlinked skill directory not discovered")
	}
	if _, ok := find(list, "linkedflat"); !ok {
		t.Error("symlinked flat skill file not discovered")
	}
	// broken symlink is skipped, not fatal.
	if err := os.Symlink(filepath.Join(target, "does-not-exist"), filepath.Join(skillsRoot, "broken")); err != nil {
		t.Fatal(err)
	}
	if _, ok := find(st.List(), "broken"); ok {
		t.Error("broken symlink should not yield a skill")
	}
}

func TestApplyIndex(t *testing.T) {
	if got := ApplyIndex("BASE", nil); got != "BASE" {
		t.Errorf("empty skills should leave base unchanged, got %q", got)
	}
	skills := []Skill{
		{Name: "alpha", Description: "the alpha", RunAs: RunInline},
		{Name: "beta", Description: "the beta", RunAs: RunSubagent},
	}
	out := ApplyIndex("BASE", skills)
	if !strings.HasPrefix(out, "BASE\n\n# Skills") {
		t.Error("index should append after the base")
	}
	if !strings.Contains(out, "- alpha — the alpha") {
		t.Errorf("inline skill line missing: %s", out)
	}
	if !strings.Contains(out, "- beta [🧬 subagent] — the beta") {
		t.Errorf("subagent tag missing: %s", out)
	}
}

func TestApplyIndexMandatesInlineButRestrainsSubagent(t *testing.T) {
	out := ApplyIndex("BASE", []Skill{{Name: "alpha", Description: "the alpha", RunAs: RunInline}})

	if !strings.Contains(out, "inline) skill is even plausibly relevant") ||
		!strings.Contains(out, "invoke it before continuing") {
		t.Errorf("inline skills should be mandatory on plausible relevance:\n%s", out)
	}
	if !strings.Contains(out, "not on weak relevance") {
		t.Errorf("subagent skills should stay judgment-based, not mandatory:\n%s", out)
	}
}

func TestApplyIndexTruncates(t *testing.T) {
	var skills []Skill
	for i := 0; i < 200; i++ {
		skills = append(skills, Skill{Name: "skill" + strings.Repeat("x", 20), Description: strings.Repeat("d", 50)})
	}
	out := ApplyIndex("BASE", skills)
	if !strings.Contains(out, "truncated") {
		t.Error("oversized index should be truncated")
	}
}

func TestCreateRefusesOverwrite(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	path, err := st.Create("mine", ScopeGlobal)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join(".reasonix", "skills", "mine", SkillFile)) {
		t.Errorf("unexpected path %q", path)
	}
	if _, err := st.Create("mine", ScopeGlobal); err == nil {
		t.Error("second create should refuse to overwrite")
	}

	writeSkill(t, home, ".reasonix/skills/legacy.md", "---\ndescription: legacy\n---\nbody")
	if _, err := st.Create("legacy", ScopeGlobal); err == nil {
		t.Error("create should refuse to shadow an existing legacy flat skill")
	}
}
