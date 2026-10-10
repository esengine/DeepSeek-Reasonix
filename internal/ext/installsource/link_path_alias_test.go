package installsource

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestApprovedLocalLinkAcceptsDirectoryAliases(t *testing.T) {
	for _, kind := range []string{"skill", "plugin"} {
		for _, location := range []string{"project", "home"} {
			for _, aliasSource := range []bool{false, true} {
				name := kind + "/" + location + "/root-alias"
				if aliasSource {
					name = kind + "/" + location + "/source-alias"
				}
				t.Run(name, func(t *testing.T) {
					base, err := filepath.EvalSymlinks(testenv.TempDir(t))
					if err != nil {
						t.Fatal(err)
					}
					project, home := filepath.Join(base, "project"), filepath.Join(base, "home")
					for _, root := range []string{project, home} {
						if err := os.MkdirAll(root, 0o755); err != nil {
							t.Fatal(err)
						}
					}
					root := project
					if location == "home" {
						root = home
					}
					alias := filepath.Join(base, "root-alias")
					if err := os.Symlink(root, alias); err != nil {
						t.Skipf("directory symlink unavailable: %v", err)
					}
					const body = "---\nname: orientation\ndescription: Inspect selected inputs\n---\nRead the selected files.\n"
					source := filepath.Join(root, "source")
					profile := filepath.Join(source, "SKILL.md")
					if kind == "plugin" {
						profile = filepath.Join(source, "skills", "orientation", "SKILL.md")
						writeFile(t, filepath.Join(source, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"orientation","version":"1.0.0","contributes":{"skills":["skills"]}}`)
					}
					writeFile(t, profile, body)
					if aliasSource {
						source = filepath.Join(alias, "source")
					} else if location == "project" {
						project = alias
					} else {
						home = alias
					}
					tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
					args := map[string]any{"source": source, "kind": kind, "mode": "link", "scope": "global"}
					plan := execInstall(t, tl, args)
					if !plan.OK || plan.Applied || len(plan.Actions) != 1 || plan.PlanID == "" || plan.Actions[0].RiskLevel != RiskMedium {
						t.Fatalf("preview = %+v", plan)
					}
					if _, err := os.Lstat(plan.Actions[0].Target); !os.IsNotExist(err) {
						t.Fatalf("preview wrote its target: %v", err)
					}
					args["apply"], args["planId"] = true, plan.PlanID
					applied := execInstall(t, tl, args)
					if !applied.OK || applied.Status != "done" {
						t.Fatalf("approved alias link = %+v", applied)
					}
					info, err := os.Lstat(applied.Actions[0].Target)
					if err != nil || info.Mode()&os.ModeSymlink == 0 {
						t.Fatalf("installed target is not a link: info=%v, err=%v", info, err)
					}
					if kind == "plugin" {
						installed, found, err := pluginpkg.FindInstalled(tl.reasonixHome, "orientation")
						if err != nil || !found || !installed.Enabled || installed.Source != source {
							t.Fatalf("installed source = %+v, found=%t, err=%v", installed, found, err)
						}
					}
					removed := execInstall(t, tl, map[string]any{"op": "uninstall", "name": "orientation", "scope": "global"})
					if !removed.OK || removed.Status != "done" {
						t.Fatalf("remove = %+v", removed)
					}
					if kind == "plugin" {
						if _, found, err := pluginpkg.FindInstalled(tl.reasonixHome, "orientation"); err != nil || found {
							t.Fatalf("registration remains after removal: found=%t, err=%v", found, err)
						}
					} else if _, err := os.Lstat(applied.Actions[0].Target); !os.IsNotExist(err) {
						t.Fatalf("skill link remains after removal: %v", err)
					}
					if got, err := os.ReadFile(profile); err != nil || string(got) != body {
						t.Fatalf("external source changed: %q, err=%v", got, err)
					}
				})
			}
		}
	}
}

func TestLocalLinkRejectsSourceOutsideAllowedRoots(t *testing.T) {
	for _, kind := range []string{"skill", "plugin"} {
		t.Run(kind, func(t *testing.T) {
			project, err := filepath.EvalSymlinks(testenv.TempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			source, err := filepath.EvalSymlinks(testenv.TempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			writeLinkAliasFixture(t, kind, source, "Read the selected files.")
			alias := filepath.Join(project, "source-alias")
			if err := os.Symlink(source, alias); err != nil {
				t.Skipf("directory symlink unavailable: %v", err)
			}
			tl := NewTool(Options{ProjectRoot: project, HomeDir: testenv.TempDir(t), RequireApprovedPlan: true})
			args := map[string]any{"source": alias, "kind": kind, "mode": "link", "scope": "global"}
			plan := execInstall(t, tl, args)
			if !plan.OK || len(plan.Actions) != 1 || plan.PlanID == "" {
				t.Fatalf("preview = %+v", plan)
			}
			args["apply"], args["planId"] = true, plan.PlanID
			applied := execInstall(t, tl, args)
			if applied.OK || applied.Status != "failed" || len(applied.Actions) != 1 || !strings.HasPrefix(applied.Actions[0].Error, ErrUnsafeLinkTarget.Error()) {
				t.Fatalf("outside-root link = %+v", applied)
			}
			if _, err := os.Lstat(plan.Actions[0].Target); !os.IsNotExist(err) {
				t.Fatalf("refused link wrote a target: %v", err)
			}
		})
	}
}

func TestLocalLinkPinsTheResolvedSource(t *testing.T) {
	for _, kind := range []string{"skill", "plugin"} {
		t.Run(kind, func(t *testing.T) {
			project, err := filepath.EvalSymlinks(testenv.TempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			source, other := filepath.Join(project, "source"), filepath.Join(project, "other-source")
			profile := writeLinkAliasFixture(t, kind, source, "Read the original selected files.")
			writeLinkAliasFixture(t, kind, other, "Read the other selected files.")
			original, err := os.ReadFile(filepath.Join(source, profile))
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(project, "source-alias")
			if err := os.Symlink(source, alias); err != nil {
				t.Skipf("directory symlink unavailable: %v", err)
			}
			tl := NewTool(Options{ProjectRoot: project, HomeDir: testenv.TempDir(t), RequireApprovedPlan: true})
			args := map[string]any{"source": alias, "kind": kind, "mode": "link", "scope": "global"}
			plan := execInstall(t, tl, args)
			args["apply"], args["planId"] = true, plan.PlanID
			applied := execInstall(t, tl, args)
			if !applied.OK || applied.Status != "done" {
				t.Fatalf("alias link = %+v", applied)
			}
			target := applied.Actions[0].Target
			if linked, err := os.Readlink(target); err != nil || linked != source {
				t.Fatalf("link was not pinned to its checked source: %q, err=%v", linked, err)
			}
			if kind == "plugin" {
				installed, found, err := pluginpkg.FindInstalled(tl.reasonixHome, "orientation")
				if err != nil || !found || installed.Root != source || installed.Source != alias {
					t.Fatalf("registration root/source = %+v, found=%t, err=%v", installed, found, err)
				}
			}
			if err := os.Remove(alias); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(other, alias); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(filepath.Join(target, profile)); err != nil || string(got) != string(original) {
				t.Fatalf("alias retarget redirected installed files: %q, err=%v", got, err)
			}
		})
	}
}

func TestLocalLinkMissingSourceWritesNothing(t *testing.T) {
	project, home := testenv.TempDir(t), testenv.TempDir(t)
	source := filepath.Join(project, "missing-source")
	if !isLinkTargetSafe(source, home, project) {
		t.Fatal("missing in-root source did not retain the lexical planning comparison")
	}
	if isLinkTargetSafe(filepath.Join(testenv.TempDir(t), "missing-source"), home, project) {
		t.Fatal("missing outside-root source passed the lexical planning comparison")
	}
	for _, kind := range []string{"skill", "plugin"} {
		t.Run(kind, func(t *testing.T) {
			tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
			_, err := tl.Execute(t.Context(), mustReviewArgs(t, map[string]any{"source": source, "kind": kind, "mode": "link", "apply": true}))
			if !errors.Is(err, ErrSourceUnreadable) {
				t.Fatalf("missing source = %v, want ErrSourceUnreadable", err)
			}
			if _, err := os.Stat(tl.reasonixHome); !os.IsNotExist(err) {
				t.Fatalf("missing source wrote installation state: %v", err)
			}
		})
	}
}

func writeLinkAliasFixture(t *testing.T, kind, source, prompt string) string {
	t.Helper()
	profile := "SKILL.md"
	if kind == "plugin" {
		profile = filepath.Join("skills", "orientation", "SKILL.md")
		writeFile(t, filepath.Join(source, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"orientation","version":"1.0.0","contributes":{"skills":["skills"]}}`)
	}
	writeFile(t, filepath.Join(source, profile), "---\nname: orientation\ndescription: Inspect selected inputs\n---\n"+prompt+"\n")
	return profile
}
