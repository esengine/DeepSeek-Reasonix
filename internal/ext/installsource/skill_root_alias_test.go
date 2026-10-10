package installsource

import (
	"context"
	"encoding/json"
	"errors"
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

func TestApprovedSkillCollectionThroughRootAlias(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		for _, mode := range []string{"auto", "register", "copy", "link"} {
			for _, entry := range []string{"ordinary", "root-link", "ancestor-link"} {
				t.Run(scope+"/"+mode+"/"+entry, func(t *testing.T) {
					requireSymlinks(t)
					project, home := testenv.TempDir(t), testenv.TempDir(t)
					author := filepath.Join(project, "author")
					t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
					root := filepath.Join(author, "catalog")
					files := map[string]string{"alpha": "alpha.md", "beta": filepath.Join("beta", skill.SkillFile), "gamma": filepath.Join("nested", "gamma", skill.SkillFile)}
					bodies := map[string]string{}
					for name, file := range files {
						bodies[name] = fmt.Sprintf("---\nname: %s\ndescription: Author %s\n---\nUse %s.\n", name, name, name)
						writeFile(t, filepath.Join(root, file), bodies[name])
					}
					source := root
					if entry != "ordinary" {
						alias := filepath.Join(project, "author-alias")
						target := author
						if entry == "root-link" {
							target = root
						}
						if err := os.Symlink(target, alias); err != nil {
							t.Fatal(err)
						}
						source = alias
						if entry == "ancestor-link" {
							source = filepath.Join(alias, "catalog")
						}
					}
					kind := "skill"
					if mode == "auto" {
						kind = "auto"
					}
					tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
					args := map[string]any{"source": source, "kind": kind, "mode": mode, "scope": scope}
					plan := execInstall(t, tl, args)
					if !plan.OK || plan.Applied || plan.Status != "planned" || plan.PlanID == "" {
						t.Fatalf("preview = %+v", plan)
					}
					configPath := config.UserConfigPath()
					if scope == "project" {
						configPath = filepath.Join(project, "reasonix.toml")
					}
					if paths := config.LoadForEdit(configPath).SkillCustomPaths(); len(paths) != 0 {
						t.Fatalf("preview registered paths: %v", paths)
					}
					args["apply"], args["planId"] = true, plan.PlanID
					done := execInstall(t, tl, args)
					if !done.OK || !done.Applied || done.Status != "done" || len(done.Actions) != len(plan.Actions) {
						t.Fatalf("apply = %+v", done)
					}
					paths := config.LoadForEdit(configPath).SkillCustomPaths()
					register := mode == "auto" || mode == "register"
					if register {
						want := []string{source, filepath.Join(source, "nested")}
						if !slices.Equal(paths, want) || len(done.Actions) != 2 {
							t.Fatalf("registered paths = %v, actions = %+v, want %v", paths, done.Actions, want)
						}
					} else if len(paths) != 0 || len(done.Actions) != 3 {
						t.Fatalf("copy/link paths = %v, actions = %+v", paths, done.Actions)
					}
					store := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, CustomPaths: paths, DisableBuiltins: true})
					for i, act := range done.Actions {
						if act.Scope != scope || act.RiskLevel != plan.Actions[i].RiskLevel || !act.Discoverable || !act.Indexed {
							t.Errorf("result = %+v", act)
						}
						for _, name := range act.Skills {
							want := filepath.Join(source, files[name])
							if !register {
								want = act.Target
								if mode == "link" && name != "alpha" {
									want = filepath.Join(want, skill.SkillFile)
								}
							}
							sk, ok := store.Read(name)
							if !ok || sk.Path != want || sk.Body != "Use "+name+"." {
								t.Errorf("Read(%s) = %+v, ok=%t, want %s", name, sk, ok, want)
							}
							if !slices.ContainsFunc(store.List(), func(listed skill.Skill) bool { return listed.Name == name && listed.Path == want }) {
								t.Errorf("List missing %s at %s", name, want)
							}
						}
						if !register {
							wantSource := filepath.Join(source, files[act.Name])
							if act.Name != "alpha" {
								wantSource = filepath.Dir(wantSource)
							}
							if act.Source != wantSource {
								t.Errorf("source spelling = %q, want %q", act.Source, wantSource)
							}
						}
					}
					for name, file := range files {
						if got, err := os.ReadFile(filepath.Join(root, file)); err != nil || string(got) != bodies[name] {
							t.Errorf("source %s changed: %q, %v", name, got, err)
						}
					}
					if register {
						removed := execInstall(t, tl, map[string]any{"op": "uninstall", "name": "alpha", "scope": scope})
						if !removed.OK || len(removed.Actions) != 1 || removed.Actions[0].Action != "remove_skill_root" || removed.Actions[0].Target != source {
							t.Fatalf("unregister = %+v", removed)
						}
						if got := config.LoadForEdit(configPath).SkillCustomPaths(); !slices.Equal(got, []string{filepath.Join(source, "nested")}) {
							t.Errorf("remaining registered paths = %v", got)
						}
						for name, file := range files {
							if got, err := os.ReadFile(filepath.Join(root, file)); err != nil || string(got) != bodies[name] {
								t.Errorf("unregister changed %s: %q, %v", name, got, err)
							}
						}
					}
				})
			}
		}
	}
}

func TestUnregisterPreviouslyConfiguredRootAlias(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			requireSymlinks(t)
			project, home := testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
			root, alias := testenv.TempDir(t), filepath.Join(project, "custom-skills")
			body := "---\nname: alpha\ndescription: Alpha\n---\nUse alpha.\n"
			writeFile(t, filepath.Join(root, "alpha.md"), body)
			if err := os.Symlink(root, alias); err != nil {
				t.Fatal(err)
			}
			path := config.UserConfigPath()
			if scope == "project" {
				path = filepath.Join(project, "reasonix.toml")
			}
			if err := config.EditConfigFile(path, func(cfg *config.Config) error { return cfg.AddSkillPath(alias) }); err != nil {
				t.Fatal(err)
			}
			store := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, CustomPaths: []string{alias}, DisableBuiltins: true})
			if _, ok := store.Read("alpha"); !ok {
				t.Fatal("configured alias must already be runtime-readable")
			}
			done := execInstall(t, NewTool(Options{ProjectRoot: project, HomeDir: home}), map[string]any{"op": "uninstall", "name": "alpha", "scope": scope})
			if !done.OK || !done.Applied || len(done.Actions) != 1 || done.Actions[0].Action != "remove_skill_root" || done.Actions[0].Target != alias {
				t.Fatalf("unregister = %+v", done)
			}
			if paths := config.LoadForEdit(path).SkillCustomPaths(); len(paths) != 0 {
				t.Errorf("root remains registered: %v", paths)
			}
			if got, err := os.ReadFile(filepath.Join(alias, "alpha.md")); err != nil || string(got) != body {
				t.Errorf("unregister changed author bytes: %q, %v", got, err)
			}
		})
	}
}

func TestSkillRootAliasKeepsScanBounds(t *testing.T) {
	requireSymlinks(t)
	root, aliasParent, outside := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
	alias := filepath.Join(aliasParent, "catalog")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: alpha\ndescription: Alpha\n---\nUse alpha.\n"
	writeFile(t, filepath.Join(root, "alpha.md"), body)
	writeFile(t, filepath.Join(root, ".git", "hidden.md"), strings.ReplaceAll(body, "alpha", "hidden"))
	writeFile(t, filepath.Join(root, "one", "two", "three", "four", "deep.md"), strings.ReplaceAll(body, "alpha", "deep"))
	writeFile(t, filepath.Join(outside, "escaped.md"), strings.ReplaceAll(body, "alpha", "escaped"))
	for name, target := range map[string]string{"outside": outside, "escaped.md": filepath.Join(outside, "escaped.md"), "loop": root, "broken": filepath.Join(outside, "missing")} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := scanSkillRoot(alias, true)
	if err != nil || len(candidates) != 1 || candidates[0].Name != "alpha" || candidates[0].SourcePath != filepath.Join(alias, "alpha.md") || candidates[0].RootPath != alias {
		t.Fatalf("bounded scan = %+v, err=%v", candidates, err)
	}
}

func TestSkillRootAliasCountLimit(t *testing.T) {
	requireSymlinks(t)
	root, alias := testenv.TempDir(t), filepath.Join(testenv.TempDir(t), "catalog")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= maxSkillScanCount; i++ {
		name := fmt.Sprintf("skill-%03d", i)
		writeFile(t, filepath.Join(root, name+".md"), fmt.Sprintf("---\nname: %s\ndescription: Skill\n---\nUse skill.\n", name))
	}
	if _, err := scanSkillRoot(alias, true); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("over-limit alias error = %v, want ErrInvalidManifest", err)
	}
}

func TestRootAliasRetargetRequiresNewPlan(t *testing.T) {
	requireSymlinks(t)
	project, home := testenv.TempDir(t), testenv.TempDir(t)
	first, second, alias := filepath.Join(project, "first"), filepath.Join(project, "second"), filepath.Join(project, "catalog")
	for root, name := range map[string]string{first: "alpha", second: "beta"} {
		writeFile(t, filepath.Join(root, name+".md"), fmt.Sprintf("---\nname: %s\ndescription: Skill\n---\nUse skill.\n", name))
	}
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
	args := map[string]any{"source": alias, "kind": "skill", "mode": "copy", "scope": "project"}
	plan := execInstall(t, tl, args)
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	args["apply"], args["planId"] = true, plan.PlanID
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tl.Execute(context.Background(), raw); !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("retarget apply error = %v, want ErrApprovalDenied", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".reasonix", "skills")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retarget wrote install root: %v", err)
	}
}

func makeSkillRootAlias(t *testing.T, target, alias string) {
	t.Helper()
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
}

func TestSkillRootAliasRegistrationPartiallyShadowed(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			requireSymlinks(t)
			project, home, root := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
			for _, name := range []string{"alpha", "beta"} {
				writeFile(t, filepath.Join(root, name+".md"), fmt.Sprintf("---\nname: %s\ndescription: Source skill\n---\nSource %s.\n", name, name))
			}
			shadow := filepath.Join(project, ".reasonix", "skills", "alpha.md")
			writeFile(t, shadow, "---\nname: alpha\ndescription: Shadow skill\n---\nShadow alpha.\n")
			alias := filepath.Join(project, "catalog")
			makeSkillRootAlias(t, root, alias)
			tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
			args := map[string]any{"source": alias, "kind": "skill", "mode": "register", "scope": scope}
			preview := execInstall(t, tl, args)
			if !preview.OK || len(preview.Actions) != 1 || preview.Actions[0].RiskLevel != RiskHigh || !strings.HasPrefix(preview.PlanID, "high:") {
				t.Fatalf("preview = %+v", preview)
			}
			resolved, err := filepath.EvalSymlinks(alias)
			if err != nil || !slices.Contains(preview.Actions[0].RiskReasons, "source resolves to "+resolved) {
				t.Fatalf("resolved source = %s, err=%v; preview=%+v", resolved, err, preview)
			}
			args["apply"], args["planId"] = true, preview.PlanID
			done := execInstall(t, tl, args)
			if !done.OK || !done.Applied || len(done.Actions) != 1 {
				t.Fatalf("apply = %+v", done)
			}
			act := done.Actions[0]
			wantBeta := filepath.Join(alias, "beta.md")
			wantWarning := fmt.Sprintf("skill %q registered from %s is not selected in this workspace; current selection is %s", "alpha", alias, shadow)
			if !act.Discoverable || !act.Indexed || act.CanonicalPath != wantBeta || !slices.Equal(act.Warnings, []string{wantWarning}) {
				t.Fatalf("partial discovery = %+v, want beta at %s and warning %q", act, wantBeta, wantWarning)
			}
			configPath := filepath.Join(project, "reasonix.toml")
			if scope == "global" {
				configPath = config.UserConfigPath()
			}
			custom := config.LoadForEdit(configPath).SkillCustomPaths()
			if !slices.Equal(custom, []string{alias}) {
				t.Fatalf("registered spelling = %v", custom)
			}
			store := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, CustomPaths: custom, DisableBuiltins: true})
			if sk, ok := store.Read("alpha"); !ok || sk.Path != shadow || sk.Body != "Shadow alpha." {
				t.Fatalf("shadow selection = %+v, found=%t", sk, ok)
			}
			if sk, ok := store.Read("beta"); !ok || sk.Path != wantBeta || sk.Body != "Source beta." {
				t.Fatalf("source selection = %+v, found=%t", sk, ok)
			}
			if err := os.Remove(shadow); err != nil {
				t.Fatal(err)
			}
			reloaded := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, CustomPaths: custom, DisableBuiltins: true})
			if sk, ok := reloaded.Read("alpha"); !ok || sk.Path != filepath.Join(alias, "alpha.md") || sk.Body != "Source alpha." {
				t.Fatalf("unshadowed reload = %+v, found=%t", sk, ok)
			}
		})
	}
}

func TestSkillRootAliasSameNamesRetargetRequiresNewPlan(t *testing.T) {
	for _, mode := range []string{"auto", "register", "copy", "link"} {
		for _, layout := range []string{"flat", "directory"} {
			t.Run(mode+"/"+layout, func(t *testing.T) {
				project, home := testenv.TempDir(t), testenv.TempDir(t)
				t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
				first, second, alias := filepath.Join(project, "first"), filepath.Join(project, "second"), filepath.Join(project, "catalog")
				file := "same.md"
				if layout == "directory" {
					file = filepath.Join("same", skill.SkillFile)
				}
				for root, body := range map[string]string{first: "first", second: "swapped"} {
					writeFile(t, filepath.Join(root, file), "---\nname: same\ndescription: Same skill\n---\n"+body+"\n")
				}
				makeSkillRootAlias(t, first, alias)
				approved := 0
				tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true, Approval: func([]action) error { approved++; return nil }})
				args := map[string]any{"source": alias, "kind": "skill", "mode": mode, "scope": "project"}
				preview := execInstall(t, tl, args)
				if !preview.OK || preview.Status != "planned" || len(preview.Actions) != 1 {
					t.Fatalf("preview = %+v", preview)
				}
				if repeat := execInstall(t, tl, args); repeat.PlanID != preview.PlanID {
					t.Fatalf("unchanged resolution changed ticket: %s / %s", repeat.PlanID, preview.PlanID)
				}
				if err := os.Remove(alias); err != nil {
					t.Fatal(err)
				}
				makeSkillRootAlias(t, second, alias)
				args["apply"], args["planId"] = true, preview.PlanID
				raw, err := json.Marshal(args)
				if err != nil {
					t.Fatal(err)
				}
				_, err = tl.Execute(t.Context(), raw)
				if !errors.Is(err, ErrApprovalDenied) {
					t.Errorf("same-names retarget error = %v, want ErrApprovalDenied", err)
				}
				if approved != 0 {
					t.Errorf("retarget reached approval callback %d times", approved)
				}
				if _, err := os.Stat(filepath.Join(project, ".reasonix", "skills")); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("retarget wrote installation: %v", err)
				}
				if _, err := os.Stat(filepath.Join(project, "reasonix.toml")); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("retarget wrote configuration: %v", err)
				}
				args["apply"] = false
				delete(args, "planId")
				replanned := execInstall(t, tl, args)
				if replanned.PlanID == preview.PlanID {
					t.Errorf("same-names retarget kept ticket %s", preview.PlanID)
				}
				if t.Failed() {
					return
				}
				args["apply"], args["planId"] = true, replanned.PlanID
				done := execInstall(t, tl, args)
				if !done.OK || done.Status != "done" || approved != 1 {
					t.Fatalf("fresh ticket apply = %+v; approvals=%d", done, approved)
				}
				custom := config.LoadForEdit(filepath.Join(project, "reasonix.toml")).SkillCustomPaths()
				if mode == "register" || mode == "auto" {
					if !slices.Equal(custom, []string{alias}) {
						t.Errorf("registered spelling = %v, want %s", custom, alias)
					}
				}
				store := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, CustomPaths: custom, DisableBuiltins: true})
				if got, ok := store.Read("same"); !ok || got.Body != "swapped" {
					t.Errorf("fresh ticket Read = %+v, found=%t", got, ok)
				}
			})
		}
	}
}

func TestSkillRootAliasPreviewReportsResolvedSourceAndRisk(t *testing.T) {
	for _, mode := range []string{"auto", "register", "copy", "link"} {
		for _, outside := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/outside=%t", mode, outside), func(t *testing.T) {
				project, home := testenv.TempDir(t), testenv.TempDir(t)
				root := filepath.Join(project, "actual")
				if outside {
					root = filepath.Join(testenv.TempDir(t), "actual")
				}
				writeFile(t, filepath.Join(root, "same", skill.SkillFile), "---\nname: same\ndescription: Same skill\n---\nOriginal.\n")
				writeFile(t, filepath.Join(root, "same", "token.txt"), "fixture-only sibling")
				alias := filepath.Join(project, "vendor-skills")
				makeSkillRootAlias(t, root, alias)
				tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
				preview := execInstall(t, tl, map[string]any{"source": alias, "kind": "skill", "mode": mode, "scope": "project"})
				if !preview.OK || len(preview.Actions) != 1 {
					t.Fatalf("preview = %+v", preview)
				}
				act := preview.Actions[0]
				resolved, err := filepath.EvalSymlinks(act.Source)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Contains(act.RiskReasons, "source resolves to "+resolved) {
					t.Errorf("preview omits actual source %s: %+v", resolved, act)
				}
				want := RiskMedium
				if outside {
					want = RiskHigh
				}
				if act.RiskLevel != want || !strings.HasPrefix(preview.PlanID, string(want)+":") {
					t.Errorf("risk = %s, ticket = %s, want %s", act.RiskLevel, preview.PlanID, want)
				}
				if _, err := os.Stat(filepath.Join(project, "reasonix.toml")); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("preview changed config: %v", err)
				}
				if got, err := os.ReadFile(filepath.Join(root, "same", "token.txt")); err != nil || string(got) != "fixture-only sibling" {
					t.Errorf("preview changed sibling: %q, %v", got, err)
				}
			})
		}
	}
}

func TestOrdinaryExternalSkillCopyGradedHigh(t *testing.T) {
	for _, layout := range []string{"flat", "directory"} {
		t.Run(layout, func(t *testing.T) {
			project, home, source := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
			file := filepath.Join(source, "same.md")
			if layout == "directory" {
				file = filepath.Join(source, "same", skill.SkillFile)
			}
			body := "---\nname: same\ndescription: Same\n---\nOriginal.\n"
			writeFile(t, file, body)
			tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
			args := map[string]any{"source": file, "kind": "skill", "mode": "copy", "scope": "project"}
			preview := execInstall(t, tl, args)
			if !preview.OK || len(preview.Actions) != 1 || preview.Actions[0].RiskLevel != RiskHigh || !strings.HasPrefix(preview.PlanID, "high:") {
				t.Fatalf("ordinary outside-root preview = %+v, want high risk", preview)
			}
			args["apply"], args["planId"] = true, preview.PlanID
			done := execInstall(t, tl, args)
			if !done.OK || done.Status != "done" {
				t.Fatalf("approved external copy = %+v", done)
			}
			if got, err := os.ReadFile(done.Actions[0].CanonicalPath); err != nil || string(got) != body {
				t.Errorf("installed bytes = %q, err=%v", got, err)
			}
			if got, err := os.ReadFile(file); err != nil || string(got) != body {
				t.Errorf("author bytes = %q, err=%v", got, err)
			}
		})
	}
}

func TestSkillRootAliasPreviewQuotesResolvedPath(t *testing.T) {
	for _, mode := range []string{"register", "copy", "link"} {
		t.Run(mode, func(t *testing.T) {
			project := testenv.TempDir(t)
			name := "catalog\nforged reason\u202eend"
			if runtime.GOOS == "windows" {
				// A Windows file name cannot hold a newline.
				name = "catalog forged reason\u202eend"
			}
			root := filepath.Join(project, name)
			writeFile(t, filepath.Join(root, "alpha.md"), "---\nname: alpha\ndescription: Skill\n---\nUse skill.\n")
			alias := filepath.Join(project, "alias")
			makeSkillRootAlias(t, root, alias)
			plan := execInstall(t, NewTool(Options{ProjectRoot: project, HomeDir: testenv.TempDir(t)}), map[string]any{"source": alias, "kind": "skill", "mode": mode})
			if !plan.OK || len(plan.Actions) != 1 {
				t.Fatalf("preview = %+v", plan)
			}
			resolved, err := filepath.EvalSymlinks(plan.Actions[0].Source)
			if err != nil {
				t.Fatal(err)
			}
			want := "source resolves to " + hostLiteral(resolved)
			if !slices.Contains(plan.Actions[0].RiskReasons, want) {
				t.Fatalf("reasons = %q, want %q", plan.Actions[0].RiskReasons, want)
			}
		})
	}
}
