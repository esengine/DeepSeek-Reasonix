package installsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestGitHubClaudeMarketplaceGitSubdirPlansAndApplies(t *testing.T) {
	marketplaceRoot := testenv.TempDir(t)
	pluginRepo := testenv.TempDir(t)
	sha := strings.Repeat("a", 40)
	writeFile(t, filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json"), `{
  "name": "ppt-master",
  "plugins": [{"name":"ppt-master","source":{
    "source":"git-subdir","url":"https://github.com/hugohe3/ppt-master.git","path":"skills"
  }}]
}`)
	writeFile(t, filepath.Join(pluginRepo, "skills", ".claude-plugin", "plugin.json"), `{"name":"ppt-master","version":"6.6.0"}`)
	writeFile(t, filepath.Join(pluginRepo, "skills", "skills", "ppt-master", "SKILL.md"), "---\nname: ppt-master\ndescription: Create presentations\n---\nCreate presentations.")
	home := testenv.TempDir(t)
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: home, RequireApprovedPlan: true})
	cleanupCalls := 0
	tl.preparePlugin = func(_ context.Context, source, mode string) (string, string, func(), error) {
		if mode != "copy" {
			t.Fatalf("clone mode = %q", mode)
		}
		switch source {
		case "https://github.com/acme/presentation-marketplace":
			return marketplaceRoot, strings.Repeat("b", 40), func() { cleanupCalls++ }, nil
		case "https://github.com/hugohe3/ppt-master.git":
			return pluginRepo, sha, func() { cleanupCalls++ }, nil
		default:
			return "", "", func() {}, fmt.Errorf("unexpected source %q", source)
		}
	}
	args := map[string]any{"source": "https://github.com/acme/presentation-marketplace", "kind": "plugin", "name": "ppt-master"}
	plan := execInstall(t, tl, args)
	if !plan.OK || plan.Status != "planned" || len(plan.Actions) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	wantSource := "https://github.com/hugohe3/ppt-master/tree/" + sha + "/skills"
	if plan.Actions[0].Source != wantSource || plan.Actions[0].Commit != sha || plan.Actions[0].SkillCount != 1 {
		t.Fatalf("action = %+v", plan.Actions[0])
	}
	if cleanupCalls != 2 {
		t.Fatalf("preview cleanup calls = %d, want 2", cleanupCalls)
	}
	args["apply"] = true
	unticketed := execInstall(t, tl, args)
	if !unticketed.OK || unticketed.Status != "planned" {
		t.Fatalf("apply without reviewed plan = %+v", unticketed)
	}
	if _, err := os.Stat(pluginpkg.InstallRoot(filepath.Join(home, ".reasonix"), "ppt-master")); !os.IsNotExist(err) {
		t.Fatalf("unticketed apply wrote a plugin: %v", err)
	}
	args["planId"] = plan.PlanID
	applied := execInstall(t, tl, args)
	if !applied.OK || applied.Status != "done" || applied.PlanID != plan.PlanID {
		t.Fatalf("apply = %+v", applied)
	}
	installed, ok, err := pluginpkg.FindInstalled(filepath.Join(home, ".reasonix"), "ppt-master")
	if err != nil || !ok || installed.Commit != sha || installed.Source != wantSource {
		t.Fatalf("installed = %+v, found = %v, error = %v", installed, ok, err)
	}
	if cleanupCalls != 6 {
		t.Fatalf("total cleanup calls = %d, want 6", cleanupCalls)
	}
}

func TestGitHubClaudeMarketplaceGitSubdirSelectsGitRefOrSHA(t *testing.T) {

	for _, tc := range []struct {
		name, ref, wantVersion string
		pinSHA                 bool
	}{
		{"default", "", "1.0.0", false},
		{"branch with slash", "release/presentations", "2.0.0", false},
		{"pinned", "", "2.0.0", true},
		{"SHA takes precedence", "main", "2.0.0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := testenv.TempDir(t)
			gitRun := func(dir string, args ...string) string {
				t.Helper()
				cmd := pluginGitCommand(context.Background(), append([]string{"-C", dir}, args...)...)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			gitRun(repo, "init", "-b", "main")
			gitRun(repo, "config", "user.name", "Reasonix fixture")
			gitRun(repo, "config", "user.email", "fixture@example.invalid")
			writeFile(t, filepath.Join(repo, "skills", ".claude-plugin", "plugin.json"), `{"name":"ppt-master","version":"1.0.0"}`)
			writeFile(t, filepath.Join(repo, "skills", "skills", "ppt-master", "SKILL.md"), "---\nname: ppt-master\ndescription: First\n---\nFirst version.")
			gitRun(repo, "add", ".")
			gitRun(repo, "commit", "--no-gpg-sign", "-m", "first")
			first := gitRun(repo, "rev-parse", "HEAD")
			gitRun(repo, "checkout", "-b", "release/presentations")
			writeFile(t, filepath.Join(repo, "skills", ".claude-plugin", "plugin.json"), `{"name":"ppt-master","version":"2.0.0"}`)
			gitRun(repo, "add", ".")
			gitRun(repo, "commit", "--no-gpg-sign", "-m", "release")
			second := gitRun(repo, "rev-parse", "HEAD")
			gitRun(repo, "checkout", "main")
			sha, wantCommit := "", first
			if tc.pinSHA {
				sha = second
			}
			if tc.pinSHA || tc.ref == "release/presentations" {
				wantCommit = second
			}
			marketplaceRoot := testenv.TempDir(t)
			writeFile(t, filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json"), fmt.Sprintf(`{"name":"presentations","plugins":[{"name":"ppt-master","source":{"source":"git-subdir","url":"https://github.com/acme/presentations.git","path":"skills","ref":%q,"sha":%q}}]}`, tc.ref, sha))
			home := testenv.TempDir(t)
			tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: home, RequireApprovedPlan: true})
			cleanupCalls := 0
			tl.preparePlugin = func(_ context.Context, source, _ string) (string, string, func(), error) {
				if source == "https://github.com/acme/marketplace" {
					return marketplaceRoot, first, func() {}, nil
				}
				if source != "https://github.com/acme/presentations.git" {
					t.Fatalf("unexpected clone source %q", source)
				}
				clone := filepath.Join(testenv.TempDir(t), "clone")
				gitRun(repo, "clone", "--depth=1", "file://"+filepath.ToSlash(repo), clone)
				return clone, gitRun(clone, "rev-parse", "HEAD"), func() { cleanupCalls++ }, nil
			}
			plan := execInstall(t, tl, map[string]any{"source": "https://github.com/acme/marketplace", "kind": "plugin"})
			if !plan.OK || len(plan.Actions) != 1 || plan.Actions[0].Commit != wantCommit || plan.Actions[0].Version != tc.wantVersion {
				t.Fatalf("plan = %+v", plan)
			}
			if cleanupCalls != 1 {
				t.Fatalf("cleanup calls = %d, want 1", cleanupCalls)
			}
			if tc.name == "default" {
				writeFile(t, filepath.Join(repo, "skills", ".claude-plugin", "plugin.json"), `{"name":"ppt-master","version":"3.0.0"}`)
				gitRun(repo, "add", ".")
				gitRun(repo, "commit", "--no-gpg-sign", "-m", "move main")
				raw, err := json.Marshal(map[string]any{"source": "https://github.com/acme/marketplace", "kind": "plugin", "apply": true, "planId": plan.PlanID})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tl.Execute(context.Background(), raw); !errors.Is(err, ErrApprovalDenied) {
					t.Fatalf("apply after branch moved = %v, want approval denied", err)
				}
				if _, err := os.Stat(pluginpkg.InstallRoot(filepath.Join(home, ".reasonix"), "ppt-master")); !os.IsNotExist(err) {
					t.Fatalf("stale plan wrote a plugin: %v", err)
				}
				if cleanupCalls != 2 {
					t.Fatalf("cleanup calls after denied apply = %d, want 2", cleanupCalls)
				}
			}
		})
	}
}

func TestGitHubClaudeMarketplaceGitSubdirSourcePathRoundTrips(t *testing.T) {
	for _, path := range []string{"skills#release", "skills?draft", "skills%release"} {
		t.Run(path, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.Contains(path, "?") {
				t.Skip("Windows directory names cannot contain question marks")
			}
			repo := testenv.TempDir(t)
			if err := os.Mkdir(filepath.Join(repo, path), 0o755); err != nil {
				t.Fatal(err)
			}
			tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
			tl.preparePlugin = func(_ context.Context, _, _ string) (string, string, func(), error) {
				return repo, strings.Repeat("a", 40), func() {}, nil
			}
			_, source, _, cleanup, err := tl.marketplaceGitSubdir(context.Background(), claudeMarketplaceURLSource{URL: "https://github.com/acme/presentations", Path: path})
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			if src, ok := parseGitHubRepoSource(source); !ok || src.Path != path {
				t.Fatalf("source %q parsed as %+v, accepted %v", source, src, ok)
			}
		})
	}
}

func TestGitHubClaudeMarketplaceGitSubdirRejectsInvalidSourcesBeforeClone(t *testing.T) {
	valid := claudeMarketplaceURLSource{URL: "https://github.com/acme/presentations", Path: "skills"}
	for _, tc := range []struct {
		name string
		edit func(*claudeMarketplaceURLSource)
	}{
		{"missing path", func(s *claudeMarketplaceURLSource) { s.Path = "" }},
		{"parent directory", func(s *claudeMarketplaceURLSource) { s.Path = "../outside" }},
		{"absolute directory", func(s *claudeMarketplaceURLSource) { s.Path = "/outside" }},
		{"backslash", func(s *claudeMarketplaceURLSource) { s.Path = `skills\presentations` }},
		{"unrepresentable URL path", func(s *claudeMarketplaceURLSource) { s.Path = "skill packs" }},
		{"unsupported host", func(s *claudeMarketplaceURLSource) { s.URL = "https://example.invalid/presentations" }},
		{"tree URL", func(s *claudeMarketplaceURLSource) { s.URL += "/tree/main/skills" }},
		{"short SHA", func(s *claudeMarketplaceURLSource) { s.SHA = "1234" }},
		{"invalid ref", func(s *claudeMarketplaceURLSource) { s.Ref = "-invalid" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := valid
			tc.edit(&source)
			tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
			tl.preparePlugin = func(_ context.Context, _, _ string) (string, string, func(), error) {
				t.Fatal("invalid source reached clone")
				return "", "", func() {}, nil
			}
			if _, _, _, _, err := tl.marketplaceGitSubdir(context.Background(), source); !errors.Is(err, ErrInvalidManifest) {
				t.Fatalf("source rejected as %v, want invalid manifest", err)
			}
		})
	}
}

func TestGitHubClaudeMarketplaceGitSubdirCleansFailedResolution(t *testing.T) {
	repo := testenv.TempDir(t)
	cleanupCalls := 0
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
	tl.preparePlugin = func(_ context.Context, _, _ string) (string, string, func(), error) {
		return repo, strings.Repeat("a", 40), func() { cleanupCalls++ }, nil
	}
	_, _, _, _, err := tl.marketplaceGitSubdir(context.Background(), claudeMarketplaceURLSource{
		URL: "https://github.com/acme/presentations", Path: "missing",
	})
	if err == nil || cleanupCalls != 1 {
		t.Fatalf("error = %v, cleanup calls = %d, want an error and 1 cleanup", err, cleanupCalls)
	}
}
