package installsource

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/ext/skill"
)

func TestApprovedDirectoryCopyFromRootAlias(t *testing.T) {
	for _, tc := range []struct {
		kind, scope string
		manifest    bool
	}{
		{"skill", "project", false}, {"skill", "global", false},
		{"skill", "project", true}, {"skill", "global", true},
		{"plugin", "global", false},
	} {
		form := "directory"
		if tc.manifest {
			form = "manifest"
		}
		for _, mode := range []string{"auto", "copy"} {
			for _, spelling := range []string{"direct", "alias", "chain", "parent-alias"} {
				t.Run(tc.kind+"/"+tc.scope+"/"+form+"/"+mode+"/"+spelling, func(t *testing.T) {
					requireSymlinks(t)
					project, home, author := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
					src := filepath.Join(author, "source")
					const body = "---\nname: orientation\ndescription: Inspect selected inputs\n---\nRead the selected files.\n"
					profile := "SKILL.md"
					if tc.kind == "plugin" {
						profile = filepath.Join("skills", "orientation", "SKILL.md")
						writeFile(t, filepath.Join(src, ".claude-plugin", "plugin.json"), `{"name":"orientation","version":"1.0.0"}`)
						writeFile(t, filepath.Join(src, "commands", "plan.md"), "---\ndescription: Plan selected inputs\n---\nPlan")
						copyRootAliasLink(t, "plan.md", filepath.Join(src, "commands", "alias.md"))
					}
					writeFile(t, filepath.Join(src, profile), body)
					writeFile(t, filepath.Join(src, "assets", "data.txt"), "author resource")
					script := filepath.Join(src, "scripts", "inspect.sh")
					writeFile(t, script, "#!/bin/sh\nexit 0\n")
					if err := os.Chmod(script, 0o751); err != nil {
						t.Fatal(err)
					}
					copyRootAliasLink(t, "data.txt", filepath.Join(src, "assets", "alias.txt"))
					outside := filepath.Join(author, "source-sibling")
					writeFile(t, filepath.Join(outside, "outside.txt"), "outside resource")
					copyRootAliasLink(t, filepath.Join(outside, "outside.txt"), filepath.Join(src, "assets", "escape.txt"))
					copyRootAliasLink(t, outside, filepath.Join(src, "assets", "directory"))
					copyRootAliasLink(t, ".", filepath.Join(src, "assets", "loop"))
					copyRootAliasLink(t, "missing.txt", filepath.Join(src, "assets", "broken"))
					writeFile(t, filepath.Join(src, ".git", "config"), "author git state")
					source := src
					switch spelling {
					case "alias":
						source = filepath.Join(author, "alias")
						copyRootAliasLink(t, "source", source)
					case "chain":
						copyRootAliasLink(t, "source", filepath.Join(author, "first"))
						source = filepath.Join(author, "second")
						copyRootAliasLink(t, "first", source)
					case "parent-alias":
						alias := filepath.Join(author, "parent-alias")
						copyRootAliasLink(t, author, alias)
						source = filepath.Join(alias, "source")
					}
					actionSource := source
					if tc.manifest {
						source = filepath.Join(source, "SKILL.md")
					}
					tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
					args := map[string]any{"source": source, "kind": tc.kind, "scope": tc.scope, "mode": mode}
					plan := execInstall(t, tl, args)
					if !plan.OK || plan.Applied || plan.Status != "planned" || len(plan.Actions) != 1 || plan.PlanID == "" {
						t.Fatalf("preview = %+v", plan)
					}
					act := plan.Actions[0]
					if act.Source != actionSource || act.Mode != "copy" || act.Scope != tc.scope || act.RiskLevel != RiskHigh || act.SkillCount != 1 {
						t.Fatalf("preview action = %+v", act)
					}
					if tc.kind == "plugin" && act.CommandCount != 2 {
						t.Fatalf("preview command count = %d", act.CommandCount)
					}
					target := act.Target
					if tc.kind == "skill" {
						target = filepath.Dir(target)
					}
					if _, err := os.Lstat(target); !os.IsNotExist(err) {
						t.Fatalf("preview wrote target: %v", err)
					}
					args["apply"] = true
					unticketed := execInstall(t, tl, args)
					if unticketed.Applied || unticketed.Status != "planned" || unticketed.PlanID != plan.PlanID {
						t.Fatalf("unticketed apply = %+v", unticketed)
					}
					if _, err := os.Lstat(target); !os.IsNotExist(err) {
						t.Fatalf("unticketed apply wrote target: %v", err)
					}
					args["planId"] = plan.PlanID
					done := execInstall(t, tl, args)
					if !done.OK || done.Status != "done" || !done.Applied || done.PlanID != plan.PlanID || len(done.Actions) != 1 {
						t.Fatalf("approved copy = %+v", done)
					}
					if done.Actions[0].Source != actionSource || done.Actions[0].Target != act.Target {
						t.Fatalf("copied action = %+v", done.Actions[0])
					}
					for rel, want := range map[string]string{
						profile: body, filepath.Join("assets", "data.txt"): "author resource",
						filepath.Join("assets", "alias.txt"): "author resource", filepath.Join("scripts", "inspect.sh"): "#!/bin/sh\nexit 0\n",
					} {
						path := filepath.Join(target, rel)
						if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
							t.Fatalf("copied %s = %v, err=%v", rel, info, err)
						}
						if got, err := os.ReadFile(path); err != nil || string(got) != want {
							t.Fatalf("copied %s = %q, err=%v", rel, got, err)
						}
					}
					for _, rel := range []string{"assets/escape.txt", "assets/directory", "assets/loop", "assets/broken", ".git"} {
						if _, err := os.Lstat(filepath.Join(target, filepath.FromSlash(rel))); !os.IsNotExist(err) {
							t.Fatalf("copied skipped entry %s: %v", rel, err)
						}
					}
					if runtime.GOOS != "windows" {
						if info, err := os.Stat(filepath.Join(target, "scripts", "inspect.sh")); err != nil || info.Mode().Perm() != 0o755 {
							t.Fatalf("executable permissions = %v, err=%v", info, err)
						}
					}
					if tc.kind == "skill" {
						store := skill.New(skill.Options{HomeDir: home, ReasonixHomeDir: tl.reasonixHome, ProjectRoot: project, DisableBuiltins: true})
						selected, ok := store.Read("orientation")
						if !ok || selected.Path != act.Target || !done.Actions[0].Discoverable || !done.Actions[0].Indexed {
							t.Fatalf("fresh skill selection = %+v, found=%t, action=%+v", selected, ok, done.Actions[0])
						}
						found := false
						for _, listed := range store.List() {
							if listed.Name == "orientation" {
								found = true
							}
						}
						if !found {
							t.Fatal("copied skill missing from fresh index")
						}
					} else {
						pkg, _, err := pluginpkg.ParseDir(target)
						if err != nil {
							t.Fatal(err)
						}
						skills, commands, _, _ := pkg.CapabilityCounts()
						if skills != 1 || commands != 2 {
							t.Fatalf("installed capabilities = skills %d, commands %d", skills, commands)
						}
						installed, found, err := pluginpkg.FindInstalled(tl.reasonixHome, "orientation")
						if err != nil || !found || !installed.Enabled || installed.Source != actionSource {
							t.Fatalf("plugin registration = %+v, found=%t, err=%v", installed, found, err)
						}
					}
					writeFile(t, filepath.Join(src, "assets", "data.txt"), "changed author resource")
					if got, err := os.ReadFile(filepath.Join(target, "assets", "alias.txt")); err != nil || string(got) != "author resource" {
						t.Fatalf("copy still depends on source: %q, err=%v", got, err)
					}
					removed := execInstall(t, tl, map[string]any{"op": "uninstall", "name": "orientation", "scope": tc.scope})
					if !removed.OK || removed.Status != "done" {
						t.Fatalf("remove = %+v", removed)
					}
					if _, err := os.Lstat(target); !os.IsNotExist(err) {
						t.Fatalf("install remains after removal: %v", err)
					}
					if got, err := os.ReadFile(filepath.Join(src, profile)); err != nil || string(got) != body {
						t.Fatalf("author profile changed: %q, err=%v", got, err)
					}
					if got, err := os.ReadFile(filepath.Join(src, "assets", "data.txt")); err != nil || string(got) != "changed author resource" {
						t.Fatalf("author resource changed: %q, err=%v", got, err)
					}
					if got, err := os.Readlink(filepath.Join(src, "assets", "alias.txt")); err != nil || got != "data.txt" {
						t.Fatalf("author link changed: %q, err=%v", got, err)
					}
					if spelling == "alias" || spelling == "chain" {
						if info, err := os.Lstat(actionSource); err != nil || info.Mode()&os.ModeSymlink == 0 {
							t.Fatalf("source link removed: %v, err=%v", info, err)
						}
					}
				})
			}
		}
	}
}

func TestDirectoryCopyRootAliasLimits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		limit    int64
		occupied bool
	}{
		{"exact-budget", 8, false}, {"over-budget", 7, false}, {"occupied-leaf", 8, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireSymlinks(t)
			author := testenv.TempDir(t)
			src := filepath.Join(author, "source")
			writeFile(t, filepath.Join(src, "data.txt"), "1234")
			copyRootAliasLink(t, "data.txt", filepath.Join(src, "alias.txt"))
			alias := filepath.Join(author, "alias")
			copyRootAliasLink(t, "source", alias)
			dst := testenv.TempDir(t)
			if tc.occupied {
				writeFile(t, filepath.Join(dst, "alias.txt"), "keep destination")
			}
			err := copyDir(alias, dst, tc.limit, "")
			switch {
			case tc.occupied:
				if !errors.Is(err, os.ErrExist) {
					t.Fatalf("occupied leaf error = %v", err)
				}
				if got, readErr := os.ReadFile(filepath.Join(dst, "alias.txt")); readErr != nil || string(got) != "keep destination" {
					t.Fatalf("destination overwritten: %q, err=%v", got, readErr)
				}
			case tc.limit < 8:
				if !errors.Is(err, ErrInvalidManifest) {
					t.Fatalf("over-budget error = %v", err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"data.txt", "alias.txt"} {
					if got, err := os.ReadFile(filepath.Join(dst, name)); err != nil || string(got) != "1234" {
						t.Fatalf("copied %s = %q, err=%v", name, got, err)
					}
				}
			}
		})
	}
}

func copyRootAliasLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestPluginRootAliasReplacementPreservesCopyVerification(t *testing.T) {
	for _, escapes := range []bool{false, true} {
		name := "in-root-command"
		if escapes {
			name = "escaping-command"
		}
		t.Run(name, func(t *testing.T) {
			requireSymlinks(t)
			author := testenv.TempDir(t)
			oldRoot, newRoot := filepath.Join(author, "v1"), filepath.Join(author, "v2")
			for _, version := range []struct{ root, version string }{{oldRoot, "1.0.0"}, {newRoot, "2.0.0"}} {
				writeFile(t, filepath.Join(version.root, ".claude-plugin", "plugin.json"), `{"name":"orientation","version":"`+version.version+`"}`)
				writeFile(t, filepath.Join(version.root, "commands", "plan.md"), "---\ndescription: Plan selected inputs\n---\n"+version.version)
			}
			old, _, err := pluginpkg.ParseDir(oldRoot)
			if err != nil {
				t.Fatal(err)
			}
			parent := testenv.TempDir(t)
			target := filepath.Join(parent, "orientation")
			if err := installCopiedPlugin(old, oldRoot, target, false, ""); err != nil {
				t.Fatal(err)
			}
			command := "plan.md"
			if escapes {
				command = filepath.Join(author, "external.md")
				writeFile(t, command, "---\ndescription: External command\n---\nExternal")
			}
			copyRootAliasLink(t, command, filepath.Join(newRoot, "commands", "alias.md"))
			alias := filepath.Join(author, "current")
			copyRootAliasLink(t, "v2", alias)
			next, _, err := pluginpkg.ParseDir(alias)
			if err != nil {
				t.Fatal(err)
			}
			if _, commands, _, _ := next.CapabilityCounts(); commands != 2 {
				t.Fatalf("source commands = %d", commands)
			}
			err = installCopiedPlugin(next, alias, target, true, "")
			wantVersion, wantCommands := "2.0.0", 2
			if escapes {
				if !errors.Is(err, ErrInvalidManifest) {
					t.Fatalf("unsafe copied capability error = %v", err)
				}
				wantVersion, wantCommands = "1.0.0", 1
			} else if err != nil {
				t.Fatal(err)
			}
			installed, _, err := pluginpkg.ParseDir(target)
			if err != nil {
				t.Fatal(err)
			}
			if _, commands, _, _ := installed.CapabilityCounts(); installed.Manifest.Version != wantVersion || commands != wantCommands {
				t.Fatalf("installed version = %s, commands = %d", installed.Manifest.Version, commands)
			}
			if got, err := os.ReadFile(filepath.Join(target, "commands", "plan.md")); err != nil || string(got) != "---\ndescription: Plan selected inputs\n---\n"+wantVersion {
				t.Fatalf("installed command changed: %q, err=%v", got, err)
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 1 || entries[0].Name() != "orientation" {
				t.Fatalf("staging/backup remains: %v, err=%v", entries, err)
			}
			if got, err := os.Readlink(alias); err != nil || got != "v2" {
				t.Fatalf("author alias changed: %q, err=%v", got, err)
			}
		})
	}
}
