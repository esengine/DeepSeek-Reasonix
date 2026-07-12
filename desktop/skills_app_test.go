package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/skill"
)

func TestNormalizeSkillPathDirectoryLayout(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: x\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := normalizeSkillPath(skillDir); got != root {
		t.Fatalf("normalizeSkillPath(%q) = %q, want %q", skillDir, got, root)
	}
}

func TestSkillRootsViewCountsProjectSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	project := t.TempDir()
	root := filepath.Join(project, ".reasonix", "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "proj.md"), []byte("---\ndescription: project\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	roots := skillRootsView()
	want := realTestPath(root)
	for _, r := range roots {
		if realTestPath(r.Dir) == want {
			if r.Status != "ok" || r.Skills != 1 || r.Scope != "project" {
				t.Fatalf("project root view = %+v", r)
			}
			if len(r.SkillItems) != 1 || r.SkillItems[0].Name != "proj" || r.SkillItems[0].Description != "project" {
				t.Fatalf("project root skill items = %+v", r.SkillItems)
			}
			return
		}
	}
	t.Fatalf("project skill root %q not found in %+v", root, roots)
}

func TestSkillRootsViewMarksEnvConfiguredCustomRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	project := t.TempDir()
	root := filepath.Join(home, "custom-skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "custom.md"), []byte("---\ndescription: custom\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_TEST_SKILL_ROOT", root)
	cfgPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("[skills]\npaths = [\"${REASONIX_TEST_SKILL_ROOT}\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	roots := skillRootsView()
	want := realTestPath(root)
	for _, r := range roots {
		if realTestPath(r.Dir) == want {
			if !r.Configured || r.Skills != 1 || r.Scope != "custom" {
				t.Fatalf("custom root view = %+v, want configured custom root with one skill", r)
			}
			if len(r.SkillItems) != 1 || r.SkillItems[0].Name != "custom" || r.SkillItems[0].Scope != "custom" {
				t.Fatalf("custom root skill items = %+v", r.SkillItems)
			}
			return
		}
	}
	t.Fatalf("custom skill root %q not found in %+v", root, roots)
}

func TestSkillRootsViewDedupesConfiguredConventionRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	project := t.TempDir()
	root := filepath.Join(home, ".reasonix", "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "global.md"), []byte("---\ndescription: global\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("[skills]\npaths = [\"~/.reasonix/skills\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	roots := skillRootsView()
	want := realTestPath(root)
	var matches []SkillRootView
	for _, r := range roots {
		if realTestPath(r.Dir) == want {
			matches = append(matches, r)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("skill root %q appears %d times, want once: %+v", root, len(matches), matches)
	}
	if !matches[0].Configured || matches[0].Skills != 1 || len(matches[0].SkillItems) != 1 {
		t.Fatalf("deduped root should keep configured metadata and skills, got %+v", matches[0])
	}
}

func TestSkillRootsViewDedupesConfiguredProjectConventionRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	project := t.TempDir()
	root := filepath.Join(project, ".reasonix", "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "project.md"), []byte("---\ndescription: project\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(cwd, ".reasonix", "skills")
	cfgPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("[skills]\npaths = [\""+root+"\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	roots := skillRootsView()
	want := realTestPath(root)
	var matches []SkillRootView
	for _, r := range roots {
		if realTestPath(r.Dir) == want {
			matches = append(matches, r)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("project skill root %q appears %d times, want once: %+v", root, len(matches), matches)
	}
	if !matches[0].Configured || matches[0].Skills != 1 || len(matches[0].SkillItems) != 1 {
		t.Fatalf("deduped project root should keep configured metadata and skills, got %+v", matches[0])
	}
	if matches[0].Status == "inactive" {
		t.Fatalf("deduped project root should stay active, got %+v", matches[0])
	}
}

func TestSkillRootsViewOmitsExcludedConventionRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	project := t.TempDir()
	root := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "noisy.md"), []byte("---\ndescription: noisy\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("[skills]\nexcluded_paths = [\"~/.agents/skills\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	roots := skillRootsView()
	want := realTestPath(root)
	for _, r := range roots {
		if realTestPath(r.Dir) == want {
			t.Fatalf("excluded convention root should be hidden, got %+v in %+v", r, roots)
		}
	}
}

func TestRemoveSkillPathPseudoDeletesConventionRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	path := filepath.Join(home, ".agents", "skills")
	app := NewApp()

	if err := app.RemoveSkillPath(path); err != nil {
		t.Fatalf("RemoveSkillPath: %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if len(cfg.Skills.ExcludedPaths) != 1 || realTestPath(cfg.Skills.ExcludedPaths[0]) != realTestPath(path) {
		t.Fatalf("excluded paths = %v, want %q", cfg.Skills.ExcludedPaths, path)
	}
}

func TestAddSkillPathRestoresConventionRootWithoutCustomPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	path := filepath.Join(home, ".agents", "skills")
	cfgPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("[skills]\nexcluded_paths = [\"~/.agents/skills\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()

	if err := app.AddSkillPath(path); err != nil {
		t.Fatalf("AddSkillPath: %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if len(cfg.Skills.ExcludedPaths) != 0 {
		t.Fatalf("excluded paths after restore = %v, want empty", cfg.Skills.ExcludedPaths)
	}
	if len(cfg.Skills.Paths) != 0 {
		t.Fatalf("restored convention root should not become custom path: %v", cfg.Skills.Paths)
	}
}

func TestCapabilitiesIncludesDisabledSkills(t *testing.T) {
	a := NewApp()
	a.setTestCtrl(control.New(control.Options{
		Skills: []skill.Skill{
			{Name: "explore", Description: "enabled", Scope: skill.ScopeBuiltin, RunAs: skill.RunSubagent},
		},
		AllSkills: []skill.Skill{
			{Name: "explore", Description: "enabled", Scope: skill.ScopeBuiltin, RunAs: skill.RunSubagent},
			{Name: "review", Description: "disabled", Scope: skill.ScopeBuiltin, RunAs: skill.RunSubagent},
		},
	}), "")
	defer a.activeCtrl().Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	cfgPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("[skills]\ndisabled_skills = [\"review\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	settingsView := a.SkillsSettings()
	assertSkillStates := func(t *testing.T, skills []SkillView) {
		t.Helper()
		states := map[string]bool{}
		for _, sk := range skills {
			states[sk.Name] = sk.Enabled
		}
		if states["explore"] != true {
			t.Fatalf("explore should be enabled in skills view: %+v", skills)
		}
		enabled, ok := states["review"]
		if !ok || enabled {
			t.Fatalf("review should be disabled but present in skills view: %+v", skills)
		}
	}
	assertSkillStates(t, settingsView.Skills)
	assertSkillStates(t, a.Capabilities().Skills)
}

func TestSkillsSettingsCarriesSubagentProfileFields(t *testing.T) {
	a := NewApp()
	a.setTestCtrl(control.New(control.Options{
		AllSkills: []skill.Skill{
			{
				Name: "my-agent", Description: "a custom subagent", Scope: skill.ScopeGlobal, RunAs: skill.RunSubagent,
				Model: "deepseek-pro", Effort: "high", AllowedTools: []string{"read_file", "grep"},
				Color: "amber", Invocation: "manual",
			},
		},
	}), "")
	defer a.activeCtrl().Close()

	views := a.SkillsSettings().Skills
	if len(views) != 1 {
		t.Fatalf("Skills = %+v, want exactly one entry", views)
	}
	got := views[0]
	if got.Model != "deepseek-pro" || got.Effort != "high" || got.Color != "amber" || got.Invocation != "/my-agent" || got.InvocationMode != "manual" {
		t.Fatalf("subagent profile fields not carried through: %+v", got)
	}
	if len(got.AllowedTools) != 2 || got.AllowedTools[0] != "read_file" || got.AllowedTools[1] != "grep" {
		t.Fatalf("AllowedTools not carried through: %v", got.AllowedTools)
	}
}

func TestAvailableSubagentToolsExcludesAlwaysHiddenTools(t *testing.T) {
	a := NewApp()
	for _, view := range a.AvailableSubagentTools() {
		for _, hidden := range []string{"install_skill", "install_source", "parallel_tasks", "wait", "bash_output", "kill_shell"} {
			if view.Name == hidden {
				t.Fatalf("AvailableSubagentTools should exclude always-hidden tool %q", hidden)
			}
		}
	}
}

func TestSkillsSettingsRefreshInvalidatesSkillRootsCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	project := t.TempDir()
	root := filepath.Join(project, ".reasonix", "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "one.md"), []byte("---\ndescription: one\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.setTestCtrl(control.New(control.Options{}), "")
	defer a.activeCtrl().Close()

	first := a.SkillsSettings()
	if got := skillRootCount(first.SkillRoots, root); got != 1 {
		t.Fatalf("initial project skill count = %d, want 1; roots=%+v", got, first.SkillRoots)
	}
	if err := os.WriteFile(filepath.Join(root, "two.md"), []byte("---\ndescription: two\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	cached := a.SkillsSettings()
	if got := skillRootCount(cached.SkillRoots, root); got != 1 {
		t.Fatalf("cached project skill count = %d, want 1 before refresh; roots=%+v", got, cached.SkillRoots)
	}
	if err := a.RefreshSkills(); err != nil {
		t.Fatalf("RefreshSkills: %v", err)
	}
	refreshed := a.SkillsSettings()
	if got := skillRootCount(refreshed.SkillRoots, root); got != 2 {
		t.Fatalf("refreshed project skill count = %d, want 2; roots=%+v", got, refreshed.SkillRoots)
	}
}

func skillRootCount(roots []SkillRootView, path string) int {
	want := realTestPath(path)
	for _, r := range roots {
		if realTestPath(r.Dir) == want {
			return r.Skills
		}
	}
	return -1
}

func realTestPath(path string) string {
	if p, err := filepath.EvalSymlinks(path); err == nil {
		path = p
	}
	return config.CanonicalSkillPath(path)
}
