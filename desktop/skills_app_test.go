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

	view := a.Capabilities()
	states := map[string]bool{}
	for _, sk := range view.Skills {
		states[sk.Name] = sk.Enabled
	}
	if states["explore"] != true {
		t.Fatalf("explore should be enabled in capabilities: %+v", view.Skills)
	}
	enabled, ok := states["review"]
	if !ok || enabled {
		t.Fatalf("review should be disabled but present in capabilities: %+v", view.Skills)
	}
}

func realTestPath(path string) string {
	if p, err := filepath.EvalSymlinks(path); err == nil {
		path = p
	}
	return config.CanonicalSkillPath(path)
}
