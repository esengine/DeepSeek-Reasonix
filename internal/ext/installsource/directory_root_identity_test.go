package installsource

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func directoryIdentityAlias(t *testing.T, target, alias string) {
	t.Helper()
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
}

func directoryIdentityFixture(t *testing.T, root, kind, body string) string {
	t.Helper()
	profile := "SKILL.md"
	if kind == "plugin" {
		writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"same","version":"1.0.0"}`)
		profile = filepath.Join("skills", "same", "SKILL.md")
	}
	writeFile(t, filepath.Join(root, profile), "---\nname: same\ndescription: Same\n---\n"+body+"\n")
	writeFile(t, filepath.Join(root, "token.txt"), "fixture-only sibling "+body)
	return profile
}

func TestDirectoryRootIdentityPreview(t *testing.T) {
	for _, kind := range []string{"skill", "plugin"} {
		for _, mode := range []string{"auto", "copy", "link"} {
			for _, location := range []string{"project", "home", "outside"} {
				t.Run(kind+"/"+mode+"/"+location, func(t *testing.T) {
					project, home := testenv.TempDir(t), testenv.TempDir(t)
					t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
					parent := project
					switch location {
					case "home":
						parent = home
					case "outside":
						parent = testenv.TempDir(t)
					}
					root, alias := filepath.Join(parent, "actual"), filepath.Join(project, "vendor")
					profile := directoryIdentityFixture(t, root, kind, "original")
					directoryIdentityAlias(t, root, alias)
					tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
					args := map[string]any{"source": alias, "kind": kind, "scope": "project", "mode": mode}
					preview := execInstall(t, tl, args)
					if !preview.OK || preview.Applied || len(preview.Actions) != 1 {
						t.Fatalf("preview = %+v", preview)
					}
					act := preview.Actions[0]
					resolved, err := filepath.EvalSymlinks(root)
					if err != nil {
						t.Fatal(err)
					}
					want := RiskMedium
					if location == "outside" {
						want = RiskHigh
					}
					if act.Source != alias || !slices.Contains(act.RiskReasons, "source resolves to "+hostLiteral(resolved)) || act.RiskLevel != want || !strings.HasPrefix(preview.PlanID, string(want)+":") {
						t.Errorf("source preview = %+v, plan = %s, want resolved %s / %s", act, preview.PlanID, resolved, want)
					}
					if _, err := os.Stat(act.Target); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("preview wrote target: %v", err)
					}
					if mode == "link" && location == "outside" {
						return
					}
					args["apply"], args["planId"] = true, preview.PlanID
					done := execInstall(t, tl, args)
					if !done.OK || done.Status != "done" {
						t.Fatalf("approved apply = %+v", done)
					}
					installed := done.Actions[0].Target
					if kind == "skill" && mode != "link" {
						installed = filepath.Dir(installed)
					}
					for rel, want := range map[string]string{profile: "---\nname: same\ndescription: Same\n---\noriginal\n", "token.txt": "fixture-only sibling original"} {
						if got, err := os.ReadFile(filepath.Join(installed, rel)); err != nil || string(got) != want {
							t.Errorf("installed %s = %q, err=%v", rel, got, err)
						}
					}
					if got, err := os.ReadFile(filepath.Join(root, "token.txt")); err != nil || string(got) != "fixture-only sibling original" {
						t.Errorf("author sibling = %q, err=%v", got, err)
					}
				})
			}
		}
	}
}

func TestDirectoryRootIdentitySameNamesRetarget(t *testing.T) {
	for _, kind := range []string{"skill", "plugin"} {
		for _, mode := range []string{"auto", "copy", "link"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				project, home := testenv.TempDir(t), testenv.TempDir(t)
				t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
				first, second, alias := filepath.Join(project, "first"), filepath.Join(project, "second"), filepath.Join(project, "vendor")
				profile := directoryIdentityFixture(t, first, kind, "original")
				directoryIdentityFixture(t, second, kind, "swapped")
				directoryIdentityAlias(t, first, alias)
				approved := 0
				tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true, Approval: func([]action) error { approved++; return nil }})
				args := map[string]any{"source": alias, "kind": kind, "scope": "project", "mode": mode}
				preview := execInstall(t, tl, args)
				if !preview.OK || len(preview.Actions) != 1 {
					t.Fatalf("preview = %+v", preview)
				}
				if repeat := execInstall(t, tl, args); repeat.PlanID != preview.PlanID {
					t.Fatalf("unchanged resolution changed ticket: %s / %s", repeat.PlanID, preview.PlanID)
				}
				if err := os.Remove(alias); err != nil {
					t.Fatal(err)
				}
				directoryIdentityAlias(t, second, alias)
				args["apply"], args["planId"] = true, preview.PlanID
				raw, err := json.Marshal(args)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tl.Execute(t.Context(), raw); !errors.Is(err, ErrApprovalDenied) {
					t.Errorf("retarget error = %v, want ErrApprovalDenied", err)
				}
				if approved != 0 {
					t.Errorf("stale ticket reached approval %d times", approved)
				}
				for _, path := range []string{filepath.Join(project, ".reasonix", "skills"), filepath.Join(home, ".reasonix", "plugins"), filepath.Join(project, "reasonix.toml"), pluginpkg.StatePath(filepath.Join(home, ".reasonix"))} {
					if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("stale ticket wrote %s: %v", path, err)
					}
				}
				delete(args, "apply")
				delete(args, "planId")
				fresh := execInstall(t, tl, args)
				if fresh.PlanID == preview.PlanID {
					t.Errorf("same names retained ticket %s", fresh.PlanID)
				}
				if t.Failed() {
					return
				}
				args["apply"], args["planId"] = true, fresh.PlanID
				done := execInstall(t, tl, args)
				if !done.OK || done.Status != "done" || approved != 1 {
					t.Fatalf("fresh apply = %+v, approvals=%d", done, approved)
				}
				installed := done.Actions[0].Target
				if kind == "skill" && mode != "link" {
					installed = filepath.Dir(installed)
				}
				if got, err := os.ReadFile(filepath.Join(installed, profile)); err != nil || string(got) != "---\nname: same\ndescription: Same\n---\nswapped\n" {
					t.Errorf("fresh installed profile = %q, err=%v", got, err)
				}
			})
		}
	}
}

func TestDirectoryRootIdentityRegistration(t *testing.T) {
	for _, location := range []string{"project", "outside"} {
		t.Run(location, func(t *testing.T) {
			project, home := testenv.TempDir(t), testenv.TempDir(t)
			parent := project
			if location == "outside" {
				parent = testenv.TempDir(t)
			}
			first, second, alias := filepath.Join(parent, "first"), filepath.Join(parent, "second"), filepath.Join(project, "vendor")
			directoryIdentityFixture(t, filepath.Join(first, "same"), "skill", "original")
			directoryIdentityFixture(t, filepath.Join(second, "same"), "skill", "swapped")
			directoryIdentityAlias(t, first, alias)
			tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
			args := map[string]any{"source": filepath.Join(alias, "same", "SKILL.md"), "kind": "skill", "mode": "register", "scope": "project"}
			preview := execInstall(t, tl, args)
			want := RiskMedium
			if location == "outside" {
				want = RiskHigh
			}
			resolved, err := filepath.EvalSymlinks(first)
			if err != nil {
				t.Fatal(err)
			}
			if !preview.OK || len(preview.Actions) != 1 || preview.Actions[0].Source != alias || preview.Actions[0].RiskLevel != want || !slices.Contains(preview.Actions[0].RiskReasons, "source resolves to "+hostLiteral(resolved)) {
				t.Errorf("registration preview = %+v", preview)
			}
			if err := os.Remove(alias); err != nil {
				t.Fatal(err)
			}
			directoryIdentityAlias(t, second, alias)
			args["apply"], args["planId"] = true, preview.PlanID
			raw, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tl.Execute(t.Context(), raw); !errors.Is(err, ErrApprovalDenied) {
				t.Errorf("registration retarget = %v, want ErrApprovalDenied", err)
			}
			if _, err := os.Stat(filepath.Join(project, "reasonix.toml")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("stale registration wrote configuration: %v", err)
			}
		})
	}
}

func TestDirectoryRootIdentityRemotePluginKeepsCommitTicket(t *testing.T) {
	project, home := testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	first, second := testenv.TempDir(t), testenv.TempDir(t)
	directoryIdentityFixture(t, first, "plugin", "original")
	directoryIdentityFixture(t, second, "plugin", "original")
	current := first
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
	tl.preparePlugin = func(context.Context, string, string) (string, string, func(), error) {
		return current, "cafe0001", func() {}, nil
	}
	args := map[string]any{"source": "https://github.com/acme/same", "kind": "plugin", "mode": "copy"}
	preview := execInstall(t, tl, args)
	current = second
	fresh := execInstall(t, tl, args)
	if !preview.OK || len(preview.Actions) != 1 || preview.Actions[0].RiskLevel != RiskMedium || preview.Actions[0].Source != args["source"] || fresh.PlanID != preview.PlanID {
		t.Fatalf("remote temporary roots changed ticket/grade: %+v / %+v", preview, fresh)
	}
	args["apply"], args["planId"] = true, preview.PlanID
	if done := execInstall(t, tl, args); !done.OK || done.Status != "done" {
		t.Fatalf("same-commit remote apply = %+v", done)
	}
}

func TestDirectoryRootIdentityOrdinaryExternalSource(t *testing.T) {
	for _, form := range []string{"skill-file", "skill-directory", "plugin-directory"} {
		t.Run(form, func(t *testing.T) {
			project, home, source := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
			kind := "skill"
			if form == "plugin-directory" {
				kind = "plugin"
			}
			profile := directoryIdentityFixture(t, source, kind, "original")
			if form == "skill-file" {
				path := filepath.Join(source, "same.md")
				if err := os.Rename(filepath.Join(source, profile), path); err != nil {
					t.Fatal(err)
				}
				source = path
			}
			tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
			args := map[string]any{"source": source, "kind": kind, "mode": "copy", "scope": "project"}
			preview := execInstall(t, tl, args)
			if !preview.OK || len(preview.Actions) != 1 || preview.Actions[0].RiskLevel != RiskHigh || !strings.HasPrefix(preview.PlanID, "high:") {
				t.Errorf("ordinary outside-root preview = %+v, want high", preview)
			}
			args["apply"], args["planId"] = true, preview.PlanID
			if done := execInstall(t, tl, args); !done.OK || done.Status != "done" {
				t.Fatalf("approved external copy = %+v", done)
			}
		})
	}
}

func TestDirectoryRootAliasSwapDuringApprovalRefusesCopy(t *testing.T) {
	for _, kind := range []string{"skill", "plugin"} {
		t.Run(kind, func(t *testing.T) {
			project, home, outside := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
			first := filepath.Join(project, "approved")
			profile := directoryIdentityFixture(t, first, kind, "approved")
			directoryIdentityFixture(t, outside, kind, "unapproved")
			writeFile(t, filepath.Join(outside, "id_ed25519"), "fixture-only unapproved bytes")
			alias := filepath.Join(project, "vendor")
			directoryIdentityAlias(t, first, alias)
			called := false
			tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true, Approval: func(actions []action) error {
				called = true
				if len(actions) != 1 || actions[0].RiskLevel != RiskMedium {
					t.Fatalf("approval = %+v", actions)
				}
				if err := os.Remove(alias); err != nil {
					t.Fatal(err)
				}
				directoryIdentityAlias(t, outside, alias)
				return nil
			}})
			args := map[string]any{"source": alias, "kind": kind, "scope": "project", "mode": "copy"}
			preview := execInstall(t, tl, args)
			if !preview.OK || len(preview.Actions) != 1 {
				t.Fatalf("preview = %+v", preview)
			}
			args["apply"], args["planId"] = true, preview.PlanID
			done := execInstall(t, tl, args)
			if !called || done.OK || done.Status != "failed" {
				t.Fatalf("swapped apply = %+v, approval called=%v", done, called)
			}
			target := preview.Actions[0].Target
			if kind == "skill" {
				target = filepath.Dir(target)
			}
			if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("swapped apply created target %s: %v", target, err)
			}
			if got, err := os.ReadFile(filepath.Join(first, profile)); err != nil || !strings.Contains(string(got), "approved") {
				t.Errorf("approved source changed: %q, %v", got, err)
			}
		})
	}
}

func TestDirectoryCopyRejectsChangedApprovedRoot(t *testing.T) {
	project := testenv.TempDir(t)
	first, second := filepath.Join(project, "first"), filepath.Join(project, "second")
	writeFile(t, filepath.Join(first, "token.txt"), "approved fixture")
	writeFile(t, filepath.Join(second, "token.txt"), "unapproved fixture")
	alias := filepath.Join(project, "alias")
	directoryIdentityAlias(t, first, alias)
	approved, err := filepath.EvalSymlinks(alias)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	directoryIdentityAlias(t, second, alias)
	target := filepath.Join(project, "installed")
	if err := copyDir(alias, target, maxSkillCopyBytes, approved); !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("copy error = %v, want ErrApprovalDenied", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("denied copy created target: %v", err)
	}
}
