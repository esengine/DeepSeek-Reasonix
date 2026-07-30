package skill

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	fileencoding "reasonix/internal/fileutil/encoding"
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

func writeSkillBytes(t *testing.T, base, rel string, content []byte) string {
	t.Helper()
	full := filepath.Join(base, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

// writeScript creates a file at base/rel with the given content.
func writeScript(t *testing.T, base, rel, content string) string {
	t.Helper()
	full := filepath.Join(base, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o755); err != nil {
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

func TestDisableDiscoveryReturnsEmptyStore(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	custom := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/global.md", "---\ndescription: global\n---\nbody")
	writeSkill(t, project, ".reasonix/skills/project.md", "---\ndescription: project\n---\nbody")
	writeSkill(t, custom, "custom.md", "---\ndescription: custom\n---\nbody")

	store := New(Options{
		HomeDir:          home,
		ProjectRoot:      project,
		CustomPaths:      []string{custom},
		DisableDiscovery: true,
	})

	if roots := store.Roots(); len(roots) != 0 {
		t.Fatalf("disabled store roots = %+v, want none", roots)
	}
	if skills := store.List(); len(skills) != 0 {
		t.Fatalf("disabled store skills = %+v, want none", skills)
	}
	if skills := store.SlashList(); len(skills) != 0 {
		t.Fatalf("disabled store slash skills = %+v, want none", skills)
	}
	if inspection := store.Inspect(); len(inspection.Roots) != 0 || len(inspection.Candidates) != 0 {
		t.Fatalf("disabled store inspection = %+v, want empty", inspection)
	}
	if _, ok := store.Read("project"); ok {
		t.Fatal("disabled store read discovered a skill")
	}
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

func TestPluginClaudeAgentLoadsAsManualSubagent(t *testing.T) {
	home := t.TempDir()
	agentRoot := filepath.Join(t.TempDir(), "agents")
	writeSkill(t, agentRoot, "reviewer.md", "---\ndescription: Review changes\nmodel: sonnet\ntools: [Read, Grep, \"mcp__*__search\"]\n---\nReview carefully.")
	key := config.CanonicalSkillPath(agentRoot)
	st := New(Options{HomeDir: home, CustomPaths: []string{agentRoot}, PluginPaths: map[string][]string{key: {"legal"}}, PluginAgentPaths: map[string][]string{key: {"legal"}}, DisableBuiltins: true})
	sk, ok := st.Read("reviewer")
	if !ok {
		t.Fatal("Claude agent was not discoverable")
	}
	if sk.RunAs != RunSubagent || sk.Invocation != "manual" || sk.Model != "" {
		t.Fatalf("agent profile = %+v", sk)
	}
	if sk.SlashName() != "legal:agent:reviewer" {
		t.Fatalf("agent slash name = %q", sk.SlashName())
	}
	want := []string{"read_file", "grep", "mcp__*__search"}
	if !slices.Equal(sk.AllowedTools, want) {
		t.Fatalf("allowed tools = %v, want %v", sk.AllowedTools, want)
	}
}

func TestPluginAgentAndSkillWithSameNameHaveDistinctQualifiedInvocations(t *testing.T) {
	home := t.TempDir()
	skillRoot := filepath.Join(t.TempDir(), "skills")
	agentRoot := filepath.Join(t.TempDir(), "agents")
	writeSkill(t, skillRoot, "leave-tracker/SKILL.md", "---\ndescription: Track leave\n---\nSkill body")
	writeSkill(t, agentRoot, "leave-tracker.md", "---\ndescription: Monitor leave\n---\nAgent body")
	skillKey := config.CanonicalSkillPath(skillRoot)
	agentKey := config.CanonicalSkillPath(agentRoot)
	st := New(Options{
		HomeDir: home, CustomPaths: []string{skillRoot, agentRoot},
		PluginPaths:      map[string][]string{skillKey: {"employment-legal"}, agentKey: {"employment-legal"}},
		PluginAgentPaths: map[string][]string{agentKey: {"employment-legal"}}, DisableBuiltins: true,
	})

	inline, ok := st.ReadSlash("employment-legal:leave-tracker")
	if !ok || inline.RunAs != RunInline {
		t.Fatalf("inline skill = %+v, found=%v", inline, ok)
	}
	agent, ok := st.ReadSlash("employment-legal:agent:leave-tracker")
	if !ok || agent.RunAs != RunSubagent || agent.Invocation != "manual" {
		t.Fatalf("agent profile = %+v, found=%v", agent, ok)
	}
}

func TestPluginSkillsUseQualifiedSlashNamesWithoutChangingModelIndex(t *testing.T) {
	home := t.TempDir()
	alpha := t.TempDir()
	beta := t.TempDir()
	writeSkill(t, alpha, "plan/SKILL.md", "---\ndescription: alpha plan\n---\nALPHA")
	writeSkill(t, beta, "plan/SKILL.md", "---\ndescription: beta plan\n---\nBETA")
	writeSkill(t, beta, "review/SKILL.md", "---\ndescription: beta review\n---\nREVIEW")

	st := New(Options{
		HomeDir:         home,
		CustomPaths:     []string{alpha, beta},
		PluginPaths:     map[string][]string{config.CanonicalSkillPath(alpha): {"alpha"}, config.CanonicalSkillPath(beta): {"beta"}},
		DisableBuiltins: true,
	})

	modelSkills := st.List()
	if len(modelSkills) != 2 || modelSkills[0].Name != "plan" || modelSkills[1].Name != "review" {
		t.Fatalf("model skills = %+v", modelSkills)
	}
	if got := IndexBlock(modelSkills); strings.Contains(got, "alpha:plan") || strings.Contains(got, "beta:plan") {
		t.Fatalf("model index must keep bare run_skill identifiers:\n%s", got)
	}
	withoutPluginMetadata := append([]Skill(nil), modelSkills...)
	for i := range withoutPluginMetadata {
		withoutPluginMetadata[i].Plugin = ""
	}
	if got, want := IndexBlock(modelSkills), IndexBlock(withoutPluginMetadata); got != want {
		t.Fatalf("plugin ownership changed cache-stable model index:\ngot:\n%s\nwant:\n%s", got, want)
	}

	slashSkills := st.SlashList()
	gotNames := make([]string, 0, len(slashSkills))
	for _, sk := range slashSkills {
		gotNames = append(gotNames, sk.SlashName())
	}
	wantNames := []string{"alpha:plan", "beta:plan", "beta:review"}
	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("slash skills = %v, want %v", gotNames, wantNames)
	}
	if _, ok := st.ReadSlash("plan"); ok {
		t.Fatal("ambiguous short plugin skill must not resolve")
	}
	if sk, ok := st.ReadSlash("/beta:plan"); !ok || sk.Body != "BETA" || sk.Name != "plan" {
		t.Fatalf("qualified beta skill = %+v, %v", sk, ok)
	}
	if sk, ok := st.Read("plan"); !ok || sk.Body != "ALPHA" || sk.Name != "plan" {
		t.Fatalf("run_skill bare winner changed = %+v, %v", sk, ok)
	}
}

func TestPluginSkillShortAliasIsHiddenAndProjectSkillKeepsShortName(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	pluginRoot := t.TempDir()
	writeSkill(t, pluginRoot, "plan/SKILL.md", "---\ndescription: plugin plan\n---\nPLUGIN")

	st := New(Options{
		HomeDir:         home,
		ProjectRoot:     project,
		CustomPaths:     []string{pluginRoot},
		PluginPaths:     map[string][]string{config.CanonicalSkillPath(pluginRoot): {"superpowers"}},
		DisableBuiltins: true,
	})
	if sk, ok := st.ReadSlash("plan"); !ok || sk.Body != "PLUGIN" {
		t.Fatalf("unambiguous short compatibility alias = %+v, %v", sk, ok)
	}
	if got := st.SlashList(); len(got) != 1 || got[0].SlashName() != "superpowers:plan" {
		t.Fatalf("visible plugin skills = %+v", got)
	}

	writeSkill(t, project, ".reasonix/skills/plan/SKILL.md", "---\ndescription: project plan\n---\nPROJECT")
	if sk, ok := st.ReadSlash("plan"); !ok || sk.Body != "PROJECT" || sk.Plugin != "" {
		t.Fatalf("project short skill = %+v, %v", sk, ok)
	}
	if sk, ok := st.ReadSlash("superpowers:plan"); !ok || sk.Body != "PLUGIN" {
		t.Fatalf("qualified plugin skill beside project winner = %+v, %v", sk, ok)
	}
}

func TestListDecodesGB18030SkillFile(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	body := "---\ndescription: 中文技能\n---\n用中文处理任务。"
	writeSkillBytes(t, root, filepath.Join("cn", SkillFile), fileencoding.Encode(body, fileencoding.GB18030))

	st := New(Options{HomeDir: home, CustomPaths: []string{root}, DisableBuiltins: true})
	skills := st.List()
	if len(skills) != 1 || skills[0].Description != "中文技能" || !strings.Contains(skills[0].Body, "用中文处理任务") {
		t.Fatalf("decoded skills = %+v", skills)
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

func TestReasonixHomeDirOverridesGlobalReasonixSkills(t *testing.T) {
	home := t.TempDir()
	reasonixHome := filepath.Join(t.TempDir(), "rx-home")
	writeSkill(t, home, ".reasonix/skills/old.md", "---\ndescription: old\n---\nold")
	writeSkill(t, home, ".reasonix/skills/current.md", "---\ndescription: old current\n---\nold current")
	currentPath := writeSkill(t, reasonixHome, "skills/current.md", "---\ndescription: current\n---\ncurrent")

	st := New(Options{HomeDir: home, ReasonixHomeDir: reasonixHome, DisableBuiltins: true})
	list := st.List()
	current, ok := find(list, "current")
	if !ok {
		t.Fatal("Reasonix home skill should be discovered")
	}
	if current.Path != currentPath {
		t.Fatalf("current skill path = %q, want Reasonix home path %q", current.Path, currentPath)
	}
	if _, ok := find(list, "old"); !ok {
		t.Fatal("legacy ~/.reasonix skill should remain discoverable")
	}

	path, err := st.Create("created", ScopeGlobal)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := filepath.Join(reasonixHome, SkillsDirname, "created", SkillFile)
	if path != want {
		t.Fatalf("created skill path = %q, want %q", path, want)
	}
}

func TestNonSkillMarkdownInClaudeSkillRootsIgnored(t *testing.T) {
	proj := t.TempDir()
	writeSkill(t, proj, ".claude/skills/guide.md", "# Skill notes\n\nThis is documentation, not a skill.")
	writeSkill(t, proj, ".claude/skills/notes.md", "---\ntitle: Notes\n---\n# Notes")
	writeSkill(t, proj, ".claude/skills/real.md", "---\ndescription: real skill\n---\nbody")

	var stderr bytes.Buffer
	st := New(Options{HomeDir: t.TempDir(), ProjectRoot: proj, DisableBuiltins: true, Stderr: &stderr})
	list := st.List()
	if _, ok := find(list, "real"); !ok {
		t.Fatal("real skill should be discovered")
	}
	for _, name := range []string{"guide", "notes"} {
		if _, ok := find(list, name); ok {
			t.Errorf("non-skill markdown %q should not be listed", name)
		}
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("non-skill markdown should not warn during List, got %q", got)
	}

	for _, name := range []string{"guide", "notes"} {
		stderr.Reset()
		if _, ok := st.Read(name); ok {
			t.Errorf("non-skill markdown %q should not be readable as a skill", name)
		}
		if got := stderr.String(); got != "" {
			t.Errorf("non-skill markdown %q should not warn during Read, got %q", name, got)
		}
	}
}

func TestSkillLikeFlatClaudeMarkdownWithoutDescriptionWarns(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".claude/skills/named.md", "---\nname: renamed\n---\nbody")

	var stderr bytes.Buffer
	st := New(Options{HomeDir: home, DisableBuiltins: true, Stderr: &stderr})
	list := st.List()
	if _, ok := find(list, "renamed"); !ok {
		t.Fatal("skill-like flat Claude markdown should still load")
	}
	if got := stderr.String(); !strings.Contains(got, "has no description") {
		t.Fatalf("skill-like flat Claude markdown without description should warn, got %q", got)
	}
}

func TestBlankDescriptionFlatClaudeMarkdownIsSkillLike(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{name: "blank", content: "---\ndescription:\n---\nbody"},
		{name: "quoted", content: "---\ndescription: \"\"\n---\nbody"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			writeSkill(t, home, ".claude/skills/"+tc.name+".md", tc.content)

			var stderr bytes.Buffer
			st := New(Options{HomeDir: home, DisableBuiltins: true, Stderr: &stderr})
			if _, ok := find(st.List(), tc.name); !ok {
				t.Fatal("blank description marker should still list flat Claude markdown as skill-like")
			}
			if got := stderr.String(); !strings.Contains(got, "has no description") {
				t.Fatalf("blank description listed skill should warn, got %q", got)
			}

			stderr.Reset()
			sk, ok := st.Read(tc.name)
			if !ok {
				t.Fatal("blank description marker should still make flat Claude markdown skill-like")
			}
			if sk.Description != "" {
				t.Fatalf("description should stay empty, got %q", sk.Description)
			}
			if got := stderr.String(); !strings.Contains(got, "has no description") {
				t.Fatalf("blank description skill should warn, got %q", got)
			}
		})
	}
}

func TestRunAsOnlyFlatClaudeMarkdownIsSkillLike(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".claude/skills/sub.md", "---\nrunAs: subagent\n---\nbody")

	var stderr bytes.Buffer
	st := New(Options{HomeDir: home, DisableBuiltins: true, Stderr: &stderr})
	sk, ok := st.Read("sub")
	if !ok {
		t.Fatal("runAs-only Claude markdown should be treated as skill-like")
	}
	if sk.RunAs != RunSubagent {
		t.Fatalf("runAs should be parsed despite frontmatter key casing, got %s", sk.RunAs)
	}
	if got := stderr.String(); !strings.Contains(got, "has no description") {
		t.Fatalf("runAs-only Claude markdown without description should warn, got %q", got)
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
		"---\ndescription: a sub\nrunAs: subagent\nallowed-tools: read_file, grep\nmodel: deepseek-pro\nread-only: true\n---\nbody")
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
	if !sub.ReadOnly {
		t.Error("read-only: true not parsed")
	}
	if fork, _ := st.Read("fork"); fork.RunAs != RunSubagent {
		t.Error("context: fork should imply subagent")
	}
	if plain, _ := st.Read("plain"); plain.RunAs != RunInline {
		t.Error("default runAs should be inline")
	}
	if plain, _ := st.Read("plain"); plain.ReadOnly {
		t.Error("read-only should default to false when the key is absent")
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

func TestScriptsAppended(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home with spaces")
	writeSkill(t, home, ".reasonix/skills/withscripts/SKILL.md", "---\ndescription: r\n---\nmain body")
	writeScript(t, home, ".reasonix/skills/withscripts/scripts/lint.py", "#!/usr/bin/env python3\nprint('ok')")
	writeScript(t, home, ".reasonix/skills/withscripts/scripts/deploy.sh", "#!/usr/bin/env bash\necho ok")

	st := New(Options{HomeDir: home, DisableBuiltins: true})
	sk, ok := st.Read("withscripts")
	if !ok {
		t.Fatal("skill not found")
	}
	if !strings.Contains(sk.Body, "main body") {
		t.Error("main body missing")
	}
	if !strings.Contains(sk.Body, "## Scripts") {
		t.Error("scripts section missing")
	}
	if !strings.Contains(sk.Body, "lint.py") || !strings.Contains(sk.Body, "deploy.sh") {
		t.Error("script paths missing from body")
	}
	if !strings.Contains(sk.Body, "main body\n\n## Scripts") {
		t.Errorf("scripts section should be separated from the original body:\n%s", sk.Body)
	}
	if !strings.Contains(sk.Body, "quote the path if it contains spaces") {
		t.Error("scripts guidance should mention quoting paths with spaces")
	}
}

func TestScriptsStayOutOfSkillIndex(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/withscripts/SKILL.md", "---\ndescription: cache-safe script skill\n---\nmain body")
	writeScript(t, home, ".reasonix/skills/withscripts/scripts/lint.py", "#!/usr/bin/env python3\nprint('ok')")

	st := New(Options{HomeDir: home, DisableBuiltins: true})
	sk, ok := st.Read("withscripts")
	if !ok {
		t.Fatal("skill not found")
	}
	if !strings.Contains(sk.Body, "## Scripts") || !strings.Contains(sk.Body, "lint.py") {
		t.Fatal("test setup expected scripts in the on-demand skill body")
	}

	index := ApplyIndex("BASE", []Skill{sk})
	if !strings.Contains(index, "withscripts") || !strings.Contains(index, "cache-safe script skill") {
		t.Fatalf("skill index missing name/description:\n%s", index)
	}
	for _, forbidden := range []string{"## Scripts", "lint.py", filepath.Join("scripts", "lint.py")} {
		if strings.Contains(index, forbidden) {
			t.Fatalf("skill index should not include on-demand script listing %q:\n%s", forbidden, index)
		}
	}
}

func TestNoScriptsWhenDirAbsent(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/noscripts/SKILL.md", "---\ndescription: r\n---\nmain body")
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	sk, ok := st.Read("noscripts")
	if !ok {
		t.Fatal("skill not found")
	}
	if strings.Contains(sk.Body, "## Scripts") {
		t.Error("should not have scripts section when scripts/ missing")
	}
}

func TestFlatSkillNoScripts(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/flat.md", "---\ndescription: r\n---\nmain body")
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	sk, ok := st.Read("flat")
	if !ok {
		t.Fatal("skill not found")
	}
	if strings.Contains(sk.Body, "## Scripts") {
		t.Error("flat skill should not have scripts section")
	}
}

func TestScriptsFilteredByExt(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/scriptscheck/SKILL.md", "---\ndescription: t\n---\nbody")
	writeScript(t, home, ".reasonix/skills/scriptscheck/scripts/lint.py", "#!/usr/bin/env python3\nprint('ok')\n")
	writeScript(t, home, ".reasonix/skills/scriptscheck/scripts/.hidden.py", "")
	writeScript(t, home, ".reasonix/skills/scriptscheck/scripts/readme.md", "# readme")
	writeScript(t, home, ".reasonix/skills/scriptscheck/scripts/deploy", "#!/bin/sh\necho ok")
	writeScript(t, home, ".reasonix/skills/scriptscheck/scripts/legacy.p", "print 'ok'\n")
	writeScript(t, home, ".reasonix/skills/scriptscheck/scripts/.gitkeep", "")

	st := New(Options{HomeDir: home, DisableBuiltins: true})
	sk, ok := st.Read("scriptscheck")
	if !ok {
		t.Fatal("skill not found")
	}
	body := sk.Body
	// lint.py should be listed (recognized .py extension)
	if !strings.Contains(body, "lint.py") {
		t.Error("lint.py should be listed (recognized .py extension)")
	}
	// deploy (no extension) should be listed (bare executable)
	if !strings.Contains(body, "deploy") {
		t.Error("deploy (no extension) should be listed as bare executable")
	}
	// .hidden.py should NOT be listed (hidden file)
	if strings.Contains(body, ".hidden.py") {
		t.Error("hidden files should NOT be listed")
	}
	// readme.md should NOT be listed (documentation, not a script)
	if strings.Contains(body, "readme.md") {
		t.Error("non-script extensions should NOT be listed")
	}
	if strings.Contains(body, "legacy.p") {
		t.Error("partial extension matches should NOT be listed")
	}
	// .gitkeep should NOT be listed (hidden file)
	if strings.Contains(body, ".gitkeep") {
		t.Error(".gitkeep should NOT be listed")
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
		"explore":         {"read_file", "ls", "glob", "grep", "code_index"},
		"research":        {"read_file", "ls", "glob", "grep", "code_index", "web_fetch"},
		"review":          {"read_file", "ls", "glob", "grep", "code_index", "bash", "use_capability"},
		"security-review": {"read_file", "ls", "glob", "grep", "code_index", "bash", "use_capability"},
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

func TestAutoResearchIsNotSeparateBuiltinSkill(t *testing.T) {
	st := New(Options{HomeDir: t.TempDir()})
	if _, listed := find(st.List(), "auto-research"); listed {
		t.Error("auto-research should be a Goal strategy, not a separate builtin skill")
	}
	if _, ok := st.Read("auto-research"); ok {
		t.Error("auto-research should not be readable as a standalone builtin skill")
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

type fakeDirEntry struct {
	name  string
	isDir bool
	typ   os.FileMode
}

func (f fakeDirEntry) Name() string      { return f.name }
func (f fakeDirEntry) IsDir() bool       { return f.isDir }
func (f fakeDirEntry) Type() os.FileMode { return f.typ }
func (f fakeDirEntry) Info() (os.FileInfo, error) {
	return fakeFileInfo{name: f.name, mode: f.typ}, nil
}

type fakeFileInfo struct {
	name string
	mode os.FileMode
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }

func TestIrregularDirectoryEntryFollowsTarget(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".agents", "skills")
	writeSkill(t, root, "linkedpack/SKILL.md", "---\ndescription: linked pack\n---\nbody")
	writeSkill(t, root, "collection/nested.md", "---\ndescription: nested\n---\nbody")

	st := New(Options{HomeDir: home, DisableBuiltins: true})
	linkedPack := fakeDirEntry{name: "linkedpack", typ: os.ModeIrregular}
	if sk, ok := st.readEntry(root, ScopeGlobal, false, linkedPack); !ok || sk.Name != "linkedpack" {
		t.Fatalf("irregular directory-layout entry should follow target, got %+v ok=%v", sk, ok)
	}
	collection := fakeDirEntry{name: "collection", typ: os.ModeIrregular}
	if !st.canScanChildDir(root, collection) {
		t.Fatal("irregular directory entry should be scannable when its target is a directory")
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

func TestReadOnlyIndexBlockPointsAtReadOnlySkill(t *testing.T) {
	out := ReadOnlyIndexBlock([]Skill{{Name: "beta", Description: "the beta", RunAs: RunSubagent}})
	if !strings.Contains(out, "read_only_skill") {
		t.Fatalf("read-only index should name read_only_skill:\n%s", out)
	}
	if strings.Contains(out, "Call `run_skill") {
		t.Fatalf("read-only index should not tell the model to call run_skill:\n%s", out)
	}
}

func TestSkillRoutingMetadataParsesButStaysOutOfIndex(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/router.md", "---\ndescription: route me\ntriggers: code review, 检查代码\nnegative-triggers: explain only\nauto-use: prefer\nneeds-fresh-data: true\ncost: low\nrequires: mcp-server:github, mcp-tool:github/search_issues\nprofiles: delivery, balanced, economy, invalid\n---\nbody")
	sk, ok := New(Options{HomeDir: home, DisableBuiltins: true}).Read("router")
	if !ok {
		t.Fatal("skill not loaded")
	}
	if got := strings.Join(sk.Triggers, ","); got != "code review,检查代码" {
		t.Fatalf("Triggers = %q", got)
	}
	if got := strings.Join(sk.NegativeTriggers, ","); got != "explain only" {
		t.Fatalf("NegativeTriggers = %q", got)
	}
	if sk.AutoUse != "prefer" || !sk.NeedsFreshData || sk.Cost != "low" {
		t.Fatalf("routing metadata = auto:%q fresh:%v cost:%q", sk.AutoUse, sk.NeedsFreshData, sk.Cost)
	}
	if got := strings.Join(sk.Requires, ","); got != "mcp-server:github,mcp-tool:github/search_issues" {
		t.Fatalf("Requires = %q", got)
	}
	if got := strings.Join(sk.Profiles, ","); got != "delivery,balanced,economy" {
		t.Fatalf("Profiles = %q (invalid values should be dropped)", got)
	}
	if got := strings.Join(sk.InvalidProfiles, ","); got != "invalid" {
		t.Fatalf("InvalidProfiles = %q (rejected values must be preserved for doctor)", got)
	}
	index := IndexBlock([]Skill{sk})
	for _, forbidden := range []string{"code review", "auto-use", "needs-fresh-data", "mcp-server:github", "profiles"} {
		if strings.Contains(index, forbidden) {
			t.Fatalf("routing metadata leaked into index (%q):\n%s", forbidden, index)
		}
	}
}

func TestColorFrontmatterParses(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/tagged.md", "---\ndescription: has a color\ncolor: amber\n---\nbody")
	sk, ok := New(Options{HomeDir: home, DisableBuiltins: true}).Read("tagged")
	if !ok {
		t.Fatal("skill not loaded")
	}
	if sk.Color != "amber" {
		t.Fatalf("Color = %q, want amber", sk.Color)
	}
}

func TestInvocationDefaultsToAutoForExistingSkills(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/plain.md", "---\ndescription: no invocation field\n---\nbody")
	sk, ok := New(Options{HomeDir: home, DisableBuiltins: true}).Read("plain")
	if !ok {
		t.Fatal("skill not loaded")
	}
	if sk.Invocation != "auto" {
		t.Fatalf("Invocation = %q, want auto (default)", sk.Invocation)
	}
	if sk.Color != "" {
		t.Fatalf("Color = %q, want empty for a file with no color: key", sk.Color)
	}
}

func TestManualInvocationSkillExcludedFromIndex(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/private-agent.md", "---\ndescription: my private subagent\nrunAs: subagent\ninvocation: manual\n---\nbody")
	writeSkill(t, home, ".reasonix/skills/public-agent.md", "---\ndescription: a discoverable subagent\nrunAs: subagent\n---\nbody")
	store := New(Options{HomeDir: home, DisableBuiltins: true})
	private, ok := store.Read("private-agent")
	if !ok {
		t.Fatal("private-agent not loaded")
	}
	if private.Invocation != "manual" {
		t.Fatalf("Invocation = %q, want manual", private.Invocation)
	}
	public, ok := store.Read("public-agent")
	if !ok {
		t.Fatal("public-agent not loaded")
	}

	index := IndexBlock([]Skill{private, public})
	if strings.Contains(index, "private-agent") {
		t.Fatalf("manual-invocation skill leaked into index:\n%s", index)
	}
	if !strings.Contains(index, "public-agent") {
		t.Fatalf("auto-invocation skill missing from index:\n%s", index)
	}

	// A read-only index built from only manual-invocation skills must render
	// as empty, not a header wrapped around nothing.
	if got := IndexBlock([]Skill{private}); got != "" {
		t.Fatalf("IndexBlock of only manual-invocation skills = %q, want empty", got)
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

func TestIndexLineClipsGraphemeClusters(t *testing.T) {
	cluster := "👨‍👩‍👧‍👦"
	got := clipRunes("a"+cluster+"bc", 3)
	want := "a" + cluster + "…"
	if got != want {
		t.Fatalf("clipRunes() = %q, want %q", got, want)
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
