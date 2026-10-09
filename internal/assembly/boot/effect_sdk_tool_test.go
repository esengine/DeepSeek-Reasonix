package boot

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func buildSDKToolPackage(t *testing.T) string {
	t.Helper()
	example, err := filepath.Abs(filepath.Join("..", "..", "..", "sdk", "go", "examples", "toolextension"))
	if err != nil {
		t.Fatal(err)
	}
	source := robustTempDir(t)
	binary := filepath.Join(source, "bin", "word-counter.exe")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	cmd.Dir = example
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build SDK tool example: %v\n%s", err, out)
	}
	manifest, err := os.ReadFile(filepath.Join(example, pluginpkg.NativeManifest))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, pluginpkg.NativeManifest), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestEffectInstalledSDKToolReachesProvider(t *testing.T) {
	source := buildSDKToolPackage(t)
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "SDK TOOL BASE"

[environment]
enabled = false

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-effect-sdk-tool"
model = "x"
`)
	approveWorkspace(t, workspace)
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
	install := func(args map[string]any) string {
		t.Helper()
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		out, err := installer.Execute(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			OK      bool   `json:"ok"`
			Applied bool   `json:"applied"`
			Status  string `json:"status"`
			PlanID  string `json:"planId"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil || !result.OK {
			t.Fatalf("install_source = %s, err=%v", out, err)
		}
		if args["apply"] == true || args["op"] == "uninstall" {
			if !result.Applied || result.Status != "done" {
				t.Fatalf("apply = %s", out)
			}
		} else if result.Applied || result.Status != "planned" || result.PlanID == "" {
			t.Fatalf("preview = %s", out)
		}
		return result.PlanID
	}
	var rec *browserScriptProvider
	provider.Register("boot-effect-sdk-tool", func(provider.Config) (provider.Provider, error) { return rec, nil })
	const name = "ext__word-counter__count_words"
	var prefix, schemas string
	runPhase := func(t *testing.T, enabled bool) {
		t.Helper()
		rec = &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
			func(string) *provider.ToolCall {
				return browserCall("discover", "use_capability", map[string]any{"action": "search", "query": "count whitespace-separated words"})
			},
		}}
		if enabled {
			for i, text := range []any{"hello\t世界\u3000again\n", nil} {
				rec.rounds = append(rec.rounds, func(string) *provider.ToolCall {
					return browserCall([]string{"count", "invalid"}[i], "use_capability", map[string]any{
						"action": "call", "capability_id": "tool:" + name, "arguments": map[string]any{"text": text},
					})
				})
			}
		}
		res, err := BuildRuntime(t.Context(), Options{Sink: event.Discard})
		if err != nil {
			t.Fatal(err)
		}
		defer res.Controller.Close()
		active := res.Extensions != nil && res.Extensions.Client("word-counter") != nil
		if active != enabled {
			t.Fatalf("SDK tool process active=%t, want %t", active, enabled)
		}
		if active {
			client := res.Extensions.Client("word-counter")
			defer func() {
				res.Controller.Close()
				waitForCond(t, "SDK tool process exit", 10*time.Second, client.Exited)
			}()
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		if err := res.Controller.Run(ctx, "Count the supplied words with the installed capability"); err != nil {
			t.Fatal(err)
		}
		reqs := agentRequests(rec.requests())
		if len(reqs) == 0 {
			t.Fatal("no provider request")
		}
		wireSchemas, err := json.Marshal(reqs[0].Tools)
		if err != nil {
			t.Fatal(err)
		}
		if systemMessage(reqs[0].Messages) == "" {
			t.Fatal("no system prefix reached provider")
		}
		if prefix == "" {
			prefix, schemas = systemMessage(reqs[0].Messages), string(wireSchemas)
		}
		if systemMessage(reqs[0].Messages) != prefix || string(wireSchemas) != schemas || toolNames(reqs[0])[name] {
			t.Fatal("installing an SDK tool changed the provider's cached prefix or tool schema")
		}
		results := effectToolResults(reqs[len(reqs)-1])
		if len(results) == 0 || strings.Contains(results[0], "tool:"+name) != enabled {
			t.Fatalf("catalog discovery enabled=%t: %q", enabled, results)
		}
		if enabled && (len(results) != 3 || results[1] != "3" || !strings.Contains(results[2], "text must be provided as a string")) {
			t.Fatalf("SDK results reaching provider = %q", results)
		}
	}
	t.Run("absent", func(t *testing.T) { runPhase(t, false) })
	args := map[string]any{"source": source, "kind": "plugin", "scope": "global", "mode": "copy"}
	args["planId"] = install(args)
	args["apply"] = true
	install(args)
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	t.Run("installed", func(t *testing.T) { runPhase(t, true) })
	if err := pluginpkg.SetEnabled(reasonixHome, "word-counter", false); err != nil {
		t.Fatal(err)
	}
	t.Run("disabled", func(t *testing.T) { runPhase(t, false) })
	if err := pluginpkg.SetEnabled(reasonixHome, "word-counter", true); err != nil {
		t.Fatal(err)
	}
	t.Run("re-enabled", func(t *testing.T) { runPhase(t, true) })
	install(map[string]any{"op": "uninstall", "name": "word-counter", "scope": "global"})
	t.Run("removed", func(t *testing.T) { runPhase(t, false) })
}
