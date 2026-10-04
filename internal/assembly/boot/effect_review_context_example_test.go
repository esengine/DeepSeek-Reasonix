package boot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func TestEffectReviewContextExampleLifecycle(t *testing.T) {
	example, err := filepath.Abs(filepath.Join("..", "..", "..", "examples", "review-context-kit"))
	if err != nil {
		t.Fatal(err)
	}
	source := robustTempDir(t)
	if err := os.CopyFS(source, os.DirFS(example)); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(source, "context", "review.txt"))
	if err != nil {
		t.Fatal(err)
	}
	block := "<hook-context event=\"SessionStart\">\n" + strings.TrimSpace(string(body)) + "\n</hook-context>"
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "CONTEXT EXAMPLE BASE"
[environment]
enabled = false
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-review-context-example"
model = "x"
`)
	approveWorkspace(t, workspace)
	var rec *effectRecordingProvider
	provider.Register("boot-review-context-example", func(provider.Config) (provider.Provider, error) { return rec, nil })
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
	type installResult struct {
		OK      bool   `json:"ok"`
		Status  string `json:"status"`
		Applied bool   `json:"applied"`
		PlanID  string `json:"planId"`
	}
	install := func(args map[string]any) installResult {
		t.Helper()
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		out, err := installer.Execute(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		var result installResult
		if err := json.Unmarshal([]byte(out), &result); err != nil || !result.OK {
			t.Fatalf("install_source = %s, err=%v", out, err)
		}
		return result
	}
	var prefix, schema string
	check := func(t *testing.T, present bool) {
		t.Helper()
		rec = &effectRecordingProvider{}
		ctrl, err := Build(t.Context(), Options{Sink: event.Discard, SessionDir: filepath.Join(robustTempDir(t), "sessions")})
		if err != nil {
			t.Fatal(err)
		}
		defer ctrl.Close()
		ctrl.EnsureSessionPath()
		prompts := []string{"Review this change", "Report the checks actually run", "Review in the fresh session"}
		for i, prompt := range prompts {
			if i == 2 {
				if err := ctrl.NewSession(); err != nil {
					t.Fatal(err)
				}
			}
			if err := ctrl.Run(t.Context(), prompt); err != nil {
				t.Fatal(err)
			}
		}
		requests := agentRequests(rec.requests())
		if len(requests) != len(prompts) {
			t.Fatalf("provider requests=%d, want %d", len(requests), len(prompts))
		}
		if raw := rec.rawUserInputs(); !slices.Equal(raw, prompts) {
			t.Fatalf("raw user inputs=%q, want %q", raw, prompts)
		}
		for i, req := range requests {
			var latest string
			for _, msg := range slices.Backward(req.Messages) {
				if msg.Role == provider.RoleUser {
					latest = msg.Content
					break
				}
			}
			want := 0
			if present && i != 1 {
				want = 1
			}
			if got := strings.Count(latest, block); got != want {
				t.Fatalf("turn %d: complete reference blocks=%d, want %d; latest user message=%s", i, got, want, latest)
			}
			if got := strings.Count(latest, "<hook-context"); got != want {
				t.Fatalf("turn %d: hook blocks=%d, want %d", i, got, want)
			}
			current := systemMessage(req.Messages)
			if strings.Contains(current, strings.TrimSpace(string(body))) {
				t.Fatal("reference entered the cache-stable system prefix")
			}
			encoded, err := json.Marshal(req.Tools)
			if err != nil {
				t.Fatal(err)
			}
			if prefix == "" {
				prefix, schema = current, string(encoded)
			} else if current != prefix || string(encoded) != schema {
				t.Fatal("context package lifecycle changed the system prefix or provider schema")
			}
		}
	}
	t.Run("absent", func(t *testing.T) { check(t, false) })
	args := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "scope": "global"}
	preview := install(args)
	if preview.Applied || preview.Status != "planned" || preview.PlanID == "" {
		t.Fatalf("preview = %+v", preview)
	}
	t.Run("preview", func(t *testing.T) { check(t, false) })
	args["apply"], args["planId"] = true, preview.PlanID
	if applied := install(args); !applied.Applied || applied.Status != "done" {
		t.Fatalf("applied = %+v", applied)
	}
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	t.Run("copied", func(t *testing.T) { check(t, true) })
	if err := pluginpkg.SetEnabled(reasonixHome, "review-context-kit", false); err != nil {
		t.Fatal(err)
	}
	if copied, err := os.ReadFile(filepath.Join(pluginpkg.InstallRoot(reasonixHome, "review-context-kit"), "context", "review.txt")); err != nil || string(copied) != string(body) {
		t.Fatalf("disable changed the copied reference: %q, err=%v", copied, err)
	}
	t.Run("disabled", func(t *testing.T) { check(t, false) })
	if err := pluginpkg.SetEnabled(reasonixHome, "review-context-kit", true); err != nil {
		t.Fatal(err)
	}
	t.Run("reenabled", func(t *testing.T) { check(t, true) })
	if removed := install(map[string]any{"op": "uninstall", "name": "review-context-kit", "scope": "global"}); !removed.Applied || removed.Status != "done" {
		t.Fatalf("removed = %+v", removed)
	}
	if _, err := os.Stat(pluginpkg.InstallRoot(reasonixHome, "review-context-kit")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("copied package survives uninstall: %v", err)
	}
	t.Run("removed", func(t *testing.T) { check(t, false) })
}
