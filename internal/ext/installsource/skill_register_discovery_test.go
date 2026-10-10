package installsource

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/skill"
)

func TestApprovedSkillRegistrationReportsItsOwnDiscovery(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		for _, tc := range []struct {
			name, entry, earlier string
			shadows              []string
		}{
			{"ordinary", "root", "", nil},
			{"project-all", "root", "project", []string{"alpha", "beta"}},
			{"custom-all", "root", "custom", []string{"alpha", "beta"}},
			{"project-partial", "root", "project", []string{"alpha"}},
			{"custom-partial", "root", "custom", []string{"alpha"}},
			{"global-is-lower-priority", "root", "global", nil},
			{"file-shadow", "file", "project", []string{"alpha"}},
			{"directory-shadow", "directory", "project", []string{"alpha"}},
			{"nested-shadow", "nested", "project", []string{"alpha"}},
			{"registered-root-alias", "alias", "alias", nil},
			{"registered-file-link", "file-link", "", nil},
			{"registered-directory-link", "directory-link", "", nil},
		} {
			t.Run(scope+"/"+tc.name, func(t *testing.T) {
				if tc.entry == "alias" || strings.HasSuffix(tc.entry, "-link") {
					requireSymlinks(t)
				}
				project, home := testenv.TempDir(t), testenv.TempDir(t)
				t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
				root := filepath.Join(project, "source-skills")
				files := map[string]string{
					"alpha": filepath.Join(root, "alpha.md"),
					"beta":  filepath.Join(root, "beta", skill.SkillFile),
				}
				if tc.entry == "directory" || tc.entry == "directory-link" {
					files["alpha"] = filepath.Join(root, "alpha", skill.SkillFile)
				}
				if tc.entry == "nested" {
					files["beta"] = filepath.Join(root, "nested", "beta", skill.SkillFile)
				}
				bytesByPath := map[string]string{}
				for name, path := range files {
					body := fmt.Sprintf("---\nname: %s\ndescription: Registered %s\n---\nUse registered %s.\n", name, name, name)
					if strings.HasSuffix(tc.entry, "-link") && name == "alpha" {
						external := filepath.Join(home, "author-source", filepath.Base(path))
						if tc.entry == "directory-link" {
							external = filepath.Join(home, "author-source", "alpha", skill.SkillFile)
						}
						writeFile(t, external, body)
						bytesByPath[external] = body
						target, source := path, external
						if tc.entry == "directory-link" {
							target, source = filepath.Dir(path), filepath.Dir(external)
						}
						if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(source, target); err != nil {
							t.Fatal(err)
						}
					} else {
						writeFile(t, path, body)
					}
					bytesByPath[path] = body
				}
				source := root
				switch tc.entry {
				case "file", "file-link":
					source = files["alpha"]
				case "directory", "directory-link":
					source = filepath.Dir(files["alpha"])
				}
				configPath := config.UserConfigPath()
				if scope == "project" {
					configPath = filepath.Join(project, "reasonix.toml")
				}
				foreign := map[string]string{}
				var custom []string
				switch tc.earlier {
				case "alias":
					custom = []string{root}
					source = filepath.Join(project, "source-alias")
					if err := os.Symlink(root, source); err != nil {
						t.Fatal(err)
					}
					for name, path := range files {
						rel, err := filepath.Rel(root, path)
						if err != nil {
							t.Fatal(err)
						}
						files[name] = filepath.Join(source, rel)
					}
					source = files["alpha"]
				case "custom":
					custom = []string{filepath.Join(project, "earlier-skills")}
				}
				if len(custom) > 0 {
					if err := config.EditConfigFile(configPath, func(cfg *config.Config) error { return cfg.AddSkillPath(custom[0]) }); err != nil {
						t.Fatal(err)
					}
				}
				for _, name := range []string{"alpha", "beta"} {
					var dir string
					switch tc.earlier {
					case "project":
						dir = filepath.Join(project, ".reasonix", "skills")
					case "custom":
						dir = custom[0]
					case "global":
						dir = filepath.Join(home, ".reasonix", "skills")
					}
					if dir != "" && (tc.earlier == "global" || slices.Contains(tc.shadows, name)) {
						foreign[name] = filepath.Join(dir, name+".md")
						body := fmt.Sprintf("---\nname: %s\ndescription: Earlier %s\n---\nUse earlier %s.\n", name, name, name)
						writeFile(t, foreign[name], body)
						bytesByPath[foreign[name]] = body
					}
				}
				tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
				args := map[string]any{"source": source, "kind": "skill", "mode": "register", "scope": scope}
				plan := execInstall(t, tl, args)
				if !plan.OK || plan.Applied || plan.Status != "planned" || plan.PlanID == "" {
					t.Fatalf("preview = %+v", plan)
				}
				if paths := config.LoadForEdit(configPath).SkillCustomPaths(); len(paths) != len(custom) {
					t.Fatalf("preview changed registration: %v", paths)
				}
				args["apply"], args["planId"] = true, plan.PlanID
				done := execInstall(t, tl, args)
				if !done.OK || !done.Applied || done.Status != "done" || len(done.Actions) != len(plan.Actions) {
					t.Fatalf("registration = %+v", done)
				}
				cfg := config.LoadForEdit(configPath)
				store := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, CustomPaths: cfg.SkillCustomPaths(), DisableBuiltins: true})
				for _, act := range done.Actions {
					if !slices.Contains(cfg.SkillCustomPaths(), act.Source) && tc.entry != "alias" {
						t.Errorf("registered source %q absent from config %v", act.Source, cfg.SkillCustomPaths())
					}
					var firstOwn string
					for _, name := range act.Skills {
						wantPath, wantBody := files[name], "Use registered "+name+"."
						if slices.Contains(tc.shadows, name) {
							wantPath, wantBody = foreign[name], "Use earlier "+name+"."
						} else if firstOwn == "" {
							firstOwn = files[name]
						}
						sk, ok := store.Read(name)
						if !ok || config.CanonicalSkillPath(sk.Path) != config.CanonicalSkillPath(wantPath) || strings.TrimSpace(sk.Body) != wantBody {
							t.Fatalf("selected %s = %+v, ok=%t, want %s", name, sk, ok, wantPath)
						}
						if slices.Contains(tc.shadows, name) {
							want := fmt.Sprintf("skill %q registered from %s is not selected in this workspace; current selection is %s", name, act.Source, sk.Path)
							if !slices.Contains(act.Warnings, want) {
								t.Errorf("warnings = %q, want %q", act.Warnings, want)
							}
						}
						indexed := slices.IndexFunc(store.List(), func(listed skill.Skill) bool { return listed.Name == name })
						if indexed < 0 || config.CanonicalSkillPath(store.List()[indexed].Path) != config.CanonicalSkillPath(wantPath) {
							t.Fatalf("index selects wrong %s", name)
						}
					}
					if act.CanonicalPath != firstOwn || act.Discoverable != (firstOwn != "") || act.Indexed != (firstOwn != "") {
						t.Errorf("registered result = %+v, want own path %q and discovery/index %t", act, firstOwn, firstOwn != "")
					}
					if len(act.Warnings) > 0 && len(tc.shadows) == 0 {
						t.Errorf("unshadowed warnings = %q", act.Warnings)
					}
				}
				for path, want := range bytesByPath {
					if got, err := os.ReadFile(path); err != nil || string(got) != want {
						t.Errorf("registration changed %s: %q, err=%v", path, got, err)
					}
				}
				for _, name := range tc.shadows {
					if err := os.Remove(foreign[name]); err != nil {
						t.Fatal(err)
					}
				}
				reloaded := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, CustomPaths: config.LoadForEdit(configPath).SkillCustomPaths(), DisableBuiltins: true})
				for _, act := range done.Actions {
					for _, name := range act.Skills {
						if sk, ok := reloaded.Read(name); !ok || config.CanonicalSkillPath(sk.Path) != config.CanonicalSkillPath(files[name]) || strings.TrimSpace(sk.Body) != "Use registered "+name+"." {
							t.Errorf("fresh unshadowed %s = %+v, ok=%t", name, sk, ok)
						}
					}
				}
			})
		}
	}
}

// The registered root and the skill that keeps the selection can each live
// under any configured path, so the not-selected warning shows both as host
// literals, as the install-side shadowing warning does.
func TestNotSelectedWarningShowsBothPathsAsLiterals(t *testing.T) {
	for name, hostile := range map[string]string{"newline": "\n", "bidi": "\u202e", "zero-width": "\u200b"} {
		t.Run(name, func(t *testing.T) {
			if hostile == "\n" && runtime.GOOS == "windows" {
				t.Skip("a Windows file name cannot hold a newline, so this path cannot exist there")
			}
			project, home := testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
			root := filepath.Join(project, "source"+hostile+"skills")
			writeFile(t, filepath.Join(root, "alpha.md"), "---\nname: alpha\ndescription: Registered alpha\n---\nUse registered alpha.\n")
			earlier := filepath.Join(project, "earlier"+hostile+"skills")
			writeFile(t, filepath.Join(earlier, "alpha.md"), "---\nname: alpha\ndescription: Earlier alpha\n---\nUse earlier alpha.\n")
			if err := config.EditConfigFile(filepath.Join(project, "reasonix.toml"), func(cfg *config.Config) error { return cfg.AddSkillPath(earlier) }); err != nil {
				t.Fatal(err)
			}
			tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
			args := map[string]any{"source": root, "kind": "skill", "mode": "register", "scope": "project"}
			plan := execInstall(t, tl, args)
			args["apply"], args["planId"] = true, plan.PlanID
			done := execInstall(t, tl, args)
			if !done.OK || len(done.Actions) != 1 {
				t.Fatalf("registration = %+v", done)
			}
			i := slices.IndexFunc(done.Actions[0].Warnings, func(w string) bool { return strings.Contains(w, "is not selected in this workspace") })
			if i < 0 {
				t.Fatalf("warnings = %q, want the not-selected warning", done.Actions[0].Warnings)
			}
			warning := done.Actions[0].Warnings[i]
			if strings.Contains(warning, hostile) || strings.Count(warning, hostLiteral(hostile)) != 2 {
				t.Fatalf("not-selected warning = %q, want both paths to show %q as %q", warning, hostile, hostLiteral(hostile))
			}
		})
	}
}
