package installsource

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestGitSubdirBulkSkipsUnavailableEntries(t *testing.T) {
	for _, tc := range []struct {
		name, url, path, ref string
		downloadErr          error
	}{
		{"unsupported host", "https://gitlab.com/acme/presentations", "skills", "", nil},
		{"invalid path", "https://github.com/acme/presentations", "../outside", "", nil},
		{"invalid ref", "https://github.com/acme/presentations", "skills", "-invalid", nil},
		{"network error", "https://github.com/acme/presentations", "skills", "", newErr(ErrSourceUnreadable, "fixture download failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testenv.TempDir(t)
			manifest := map[string]any{"name": "marketplace", "plugins": []any{
				map[string]any{"name": "working", "source": "plugins/working"},
				map[string]any{"name": "unavailable", "source": map[string]string{"source": "git-subdir", "url": tc.url, "path": tc.path, "ref": tc.ref}},
			}}
			body, _ := json.Marshal(manifest)
			writeFile(t, filepath.Join(root, ".claude-plugin", "marketplace.json"), string(body))
			writeFile(t, filepath.Join(root, "plugins", "working", ".claude-plugin", "plugin.json"), `{"name":"working","version":"1.0.0"}`)
			writeFile(t, filepath.Join(root, "plugins", "working", "skills", "working", "SKILL.md"), "---\nname: working\ndescription: Fixture\n---\nFixture.")
			tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
			tl.preparePlugin = func(_ context.Context, source, _ string) (string, string, func(), error) {
				if source == "https://github.com/acme/marketplace" {
					return root, strings.Repeat("a", 40), func() {}, nil
				}
				if tc.downloadErr == nil {
					t.Fatalf("invalid source reached clone: %s", source)
				}
				return "", "", func() {}, tc.downloadErr
			}
			plan := execInstall(t, tl, map[string]any{"source": "https://github.com/acme/marketplace", "kind": "plugin"})
			if !plan.OK || len(plan.Actions) != 1 || plan.Actions[0].Name != "working" || len(plan.Warnings) == 0 {
				t.Fatalf("bulk plan = %+v", plan)
			}
			raw, _ := json.Marshal(map[string]any{"source": "https://github.com/acme/marketplace", "kind": "plugin", "name": "unavailable"})
			if _, err := tl.Execute(context.Background(), raw); err == nil {
				t.Fatal("explicitly selected unavailable entry did not fail")
			}
		})
	}
}

func TestGitSubdirMissingGitKeepsTarballMarketplaceUsable(t *testing.T) {
	archive := buildTarball(t, []tarEntry{
		{name: "marketplace-abc1234/.claude-plugin/marketplace.json", body: `{"name":"fixture","plugins":[{"name":"working","source":"plugins/working"},{"name":"unavailable","source":{"source":"git-subdir","url":"https://github.com/acme/presentations","path":"skills"}}]}`},
		{name: "marketplace-abc1234/plugins/working/.claude-plugin/plugin.json", body: `{"name":"working","version":"1.0.0"}`},
		{name: "marketplace-abc1234/plugins/working/skills/working/SKILL.md", body: "---\nname: working\ndescription: Fixture\n---\nFixture."},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/marketplace/tarball" {
			t.Errorf("unavailable source reached network: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	old := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	t.Cleanup(func() { githubAPIBaseURL = old })
	t.Setenv("PATH", testenv.TempDir(t))
	home := testenv.TempDir(t)
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: home, HTTPClient: server.Client(), RequireApprovedPlan: true})
	args := map[string]any{"source": "https://github.com/acme/marketplace", "kind": "plugin"}
	plan := execInstall(t, tl, args)
	if !plan.OK || len(plan.Actions) != 1 || plan.Actions[0].Name != "working" || len(plan.Warnings) == 0 {
		t.Fatalf("no-Git HTTP tarball plan = %+v", plan)
	}
	args["apply"], args["planId"] = true, plan.PlanID
	applied := execInstall(t, tl, args)
	if !applied.OK || applied.Status != "done" {
		t.Fatalf("no-Git HTTP tarball apply = %+v", applied)
	}
	installed, found, err := pluginpkg.FindInstalled(tl.reasonixHome, "working")
	if err != nil || !found || !installed.Enabled {
		t.Fatalf("working plugin state = %+v, found=%v, err=%v", installed, found, err)
	}
	root := pluginpkg.ResolveRoot(tl.reasonixHome, installed.Root)
	body, err := os.ReadFile(filepath.Join(root, "skills", "working", "SKILL.md"))
	if err != nil || string(body) != "---\nname: working\ndescription: Fixture\n---\nFixture." {
		t.Fatalf("working plugin on disk = %q, err=%v", body, err)
	}
	args["name"], args["apply"] = "unavailable", false
	if _, err := tl.Execute(context.Background(), mustReviewArgs(t, args)); !errors.Is(err, ErrBinaryMissing) {
		t.Fatalf("explicit no-Git entry = %v, want missing runtime", err)
	}
}

func mustReviewArgs(t *testing.T, args map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestGitSubdirWithoutGitReportsMissingRuntime(t *testing.T) {
	t.Setenv("PATH", testenv.TempDir(t))
	for _, ref := range []string{"", "main"} {
		tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
		tl.preparePlugin = func(_ context.Context, _, _ string) (string, string, func(), error) {
			t.Fatal("git-subdir attempted to download without Git")
			return "", "", func() {}, nil
		}
		_, _, _, _, err := tl.marketplaceGitSubdir(context.Background(), claudeMarketplaceURLSource{
			URL: "https://github.com/acme/presentations", Path: "skills", Ref: ref,
		})
		if !errors.Is(err, ErrBinaryMissing) || errors.Is(err, ErrInvalidManifest) {
			t.Fatalf("without Git ref=%q: %v, want missing runtime", ref, err)
		}
	}
	root := testenv.TempDir(t)
	writeFile(t, filepath.Join(root, ".claude-plugin", "marketplace.json"), `{"name":"fixture","plugins":[{"name":"unavailable","source":{"source":"git-subdir","url":"https://github.com/acme/presentations","path":"skills","ref":"main"}}]}`)
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
	tl.preparePlugin = func(_ context.Context, source, _ string) (string, string, func(), error) {
		if source != "https://github.com/acme/marketplace" {
			t.Fatal("missing Git source reached download")
		}
		return root, strings.Repeat("a", 40), func() {}, nil
	}
	raw, _ := json.Marshal(map[string]any{"source": "https://github.com/acme/marketplace", "kind": "plugin", "name": "unavailable"})
	if _, err := tl.Execute(context.Background(), raw); !errors.Is(err, ErrBinaryMissing) {
		t.Fatalf("selected no-Git source lost its error class: %v", err)
	}
}

func TestGitSubdirCancellationStopsBulkPlanning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
	tl.preparePlugin = func(_ context.Context, _, _ string) (string, string, func(), error) {
		cancel()
		return "", "", func() {}, newErr(ErrSourceUnreadable, "clone interrupted")
	}
	_, _, _, _, err := tl.marketplaceObjectSource(ctx, json.RawMessage(`{"source":"git-subdir","url":"https://github.com/acme/presentations","path":"skills"}`))
	var skipped *unsupportedMarketplaceObject
	if !errors.Is(err, context.Canceled) || errors.As(err, &skipped) {
		t.Fatalf("canceled source became skippable: %v", err)
	}
}
