package installsource

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/skill"
)

func TestApprovedSkillInstallReportsItsOwnDiscovery(t *testing.T) {
	for _, mode := range []string{"copy", "link"} {
		for _, layout := range []string{"file", "directory"} {
			for _, tc := range []struct {
				name, scope, other string
				shadowed           bool
			}{
				{"global", "global", "", false},
				{"project-shadows-global", "global", "project", true},
				{"custom-shadows-global", "global", "custom", true},
				{"custom-alias-of-global", "global", "alias", false},
				{"project-over-global", "project", "global", false},
			} {
				t.Run(mode+"/"+layout+"/"+tc.name, func(t *testing.T) {
					if mode == "link" || tc.other == "alias" {
						requireSymlinks(t)
					}
					project, home := testenv.TempDir(t), testenv.TempDir(t)
					reasonixHome := filepath.Join(home, ".reasonix")
					t.Setenv("REASONIX_HOME", reasonixHome)
					projectRoot := filepath.Join(project, ".reasonix", "skills")
					globalRoot := filepath.Join(reasonixHome, "skills")
					installRoot := globalRoot
					if tc.scope == "project" {
						installRoot = projectRoot
					}
					canonical := filepath.Join(installRoot, "orientation", skill.SkillFile)
					const installed = "---\nname: orientation\ndescription: Installed helper\n---\nUse the installed helper.\n"
					const earlier = "---\nname: orientation\ndescription: Earlier helper\n---\nUse the earlier helper.\n"
					var otherPath string
					var custom []string
					switch tc.other {
					case "project":
						otherPath = filepath.Join(projectRoot, "orientation", skill.SkillFile)
					case "global":
						otherPath = filepath.Join(globalRoot, "orientation", skill.SkillFile)
					case "custom":
						custom = []string{filepath.Join(project, "custom-skills")}
						otherPath = filepath.Join(custom[0], "orientation.md")
					case "alias":
						if err := os.MkdirAll(globalRoot, 0o755); err != nil {
							t.Fatal(err)
						}
						custom = []string{filepath.Join(project, "skills-alias")}
						if err := os.Symlink(globalRoot, custom[0]); err != nil {
							t.Fatal(err)
						}
					}
					if otherPath != "" {
						writeFile(t, otherPath, earlier)
					}
					if len(custom) > 0 {
						if err := config.EditConfigFile(config.UserConfigPath(), func(cfg *config.Config) error {
							return cfg.AddSkillPath(custom[0])
						}); err != nil {
							t.Fatal(err)
						}
					}
					original := filepath.Join(project, "source", "orientation.md")
					if layout == "directory" {
						original = filepath.Join(project, "source", "orientation", skill.SkillFile)
						writeFile(t, filepath.Join(filepath.Dir(original), "reference.txt"), "Keep this resource.\n")
					}
					writeFile(t, original, installed)
					tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
					args := map[string]any{"source": original, "kind": "skill", "scope": tc.scope, "mode": mode}
					plan := execInstall(t, tl, args)
					if !plan.OK || plan.Status != "planned" || plan.Applied || plan.PlanID == "" || len(plan.Actions) != 1 {
						t.Fatalf("preview = %+v", plan)
					}
					if _, err := os.Lstat(canonical); !os.IsNotExist(err) {
						t.Fatalf("preview wrote its destination: %v", err)
					}
					args["apply"], args["planId"] = true, plan.PlanID
					applied := execInstall(t, tl, args)
					if !applied.OK || !applied.Applied || applied.Status != "done" || len(applied.Actions) != 1 {
						t.Fatalf("approved install = %+v", applied)
					}
					act := applied.Actions[0]
					for path, want := range map[string]string{original: installed, canonical: installed, otherPath: earlier} {
						if path != "" {
							if got, err := os.ReadFile(path); err != nil || string(got) != want {
								t.Fatalf("file %s = %q, err=%v, want %q", path, got, err, want)
							}
						}
					}
					if layout == "directory" {
						if got, err := os.ReadFile(filepath.Join(filepath.Dir(canonical), "reference.txt")); err != nil || string(got) != "Keep this resource.\n" {
							t.Fatalf("installed resource = %q, err=%v", got, err)
						}
					}
					store := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, CustomPaths: custom, DisableBuiltins: true})
					selected, ok := store.Read("orientation")
					wantPath, wantBody := canonical, "Use the installed helper."
					if tc.shadowed {
						wantPath, wantBody = otherPath, "Use the earlier helper."
					}
					if !ok || config.CanonicalSkillPath(selected.Path) != config.CanonicalSkillPath(wantPath) || strings.TrimSpace(selected.Body) != wantBody {
						t.Fatalf("selected skill = %+v, ok=%t, want %s", selected, ok, wantPath)
					}
					if listed := store.List(); len(listed) != 1 || config.CanonicalSkillPath(listed[0].Path) != config.CanonicalSkillPath(wantPath) {
						t.Fatalf("index = %+v, want %s", listed, wantPath)
					}
					if act.CanonicalPath != canonical {
						t.Errorf("canonicalPath = %q, want installed destination %q", act.CanonicalPath, canonical)
					}
					if act.Discoverable != !tc.shadowed || act.Indexed != !tc.shadowed {
						t.Errorf("installed discovery = %t/%t, shadowed=%t", act.Discoverable, act.Indexed, tc.shadowed)
					}
					if tc.shadowed {
						want := fmt.Sprintf("skill %q installed at %s is shadowed in this workspace by %s", "orientation", canonical, selected.Path)
						if !slices.Contains(act.Warnings, want) {
							t.Errorf("warnings = %q, want %q", act.Warnings, want)
						}
						if err := os.Remove(otherPath); err != nil {
							t.Fatal(err)
						}
						reloaded := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, CustomPaths: custom, DisableBuiltins: true})
						if sk, ok := reloaded.Read("orientation"); !ok || config.CanonicalSkillPath(sk.Path) != config.CanonicalSkillPath(canonical) || strings.TrimSpace(sk.Body) != "Use the installed helper." {
							t.Fatalf("unshadowed install = %+v, ok=%t", sk, ok)
						}
					} else if len(act.Warnings) != 0 {
						t.Errorf("unshadowed install warnings = %q", act.Warnings)
					}
				})
			}
		}
	}
}
