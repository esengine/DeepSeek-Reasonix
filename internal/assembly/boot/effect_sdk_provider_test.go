package boot

import (
	"context"
	"encoding/json"
	"errors"
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

func buildSDKProviderPackage(t *testing.T) string {
	t.Helper()
	example, err := filepath.Abs(filepath.Join("..", "..", "..", "sdk", "go", "examples", "providerextension"))
	if err != nil {
		t.Fatal(err)
	}
	source := robustTempDir(t)
	binary := filepath.Join(source, "bin", "echo-provider.exe")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	cmd.Dir = example
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build SDK provider example: %v\n%s", err, out)
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

func TestEffectInstalledSDKProviderCompletesFirstTurn(t *testing.T) {
	source := buildSDKProviderPackage(t)
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	const ref = "plugin/echo-provider/offline/echo"
	writePluginDefaultFixture(t, workspace, ref)
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
	runPhase := func(t *testing.T, enabled bool) {
		t.Helper()
		res, err := BuildRuntime(t.Context(), Options{Sink: event.Discard})
		if !enabled {
			if res != nil {
				res.Controller.Close()
			}
			if !errors.Is(err, ErrUnknownModel) {
				t.Fatalf("inactive plugin default_model: got %v, want ErrUnknownModel", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		defer res.Controller.Close()
		if res.Controller.ModelRef() != ref || res.Extensions == nil || res.Extensions.Client("echo-provider") == nil {
			t.Fatalf("first boot did not select the installed SDK provider: %q", res.Controller.ModelRef())
		}
		client := res.Extensions.Client("echo-provider")
		defer func() {
			res.Controller.Close()
			waitForCond(t, "SDK provider process exit", 10*time.Second, client.Exited)
			if !res.Runtime.Closed() {
				t.Fatal("SDK provider runtime did not close")
			}
		}()
		for _, input := range []string{"SDK-FIRST-TURN 世界", "SDK-SECOND-TURN again"} {
			before := len(res.Controller.History())
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			err := res.Controller.RunTurn(ctx, input)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			var text strings.Builder
			for _, message := range res.Controller.History()[before:] {
				if len(message.ToolCalls) != 0 || message.Role == provider.RoleTool {
					t.Fatalf("offline provider produced a tool call or result: %+v", message)
				}
				if message.Role == provider.RoleAssistant {
					text.WriteString(message.Content)
				}
			}
			if !strings.HasPrefix(text.String(), "Offline echo: ") || !strings.Contains(text.String(), input) {
				t.Fatalf("assistant turn = %q, want the SDK echo of %q", text.String(), input)
			}
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
	if err := pluginpkg.SetEnabled(reasonixHome, "echo-provider", false); err != nil {
		t.Fatal(err)
	}
	t.Run("disabled", func(t *testing.T) { runPhase(t, false) })
	if err := pluginpkg.SetEnabled(reasonixHome, "echo-provider", true); err != nil {
		t.Fatal(err)
	}
	t.Run("re-enabled", func(t *testing.T) { runPhase(t, true) })
	install(map[string]any{"op": "uninstall", "name": "echo-provider", "scope": "global"})
	t.Run("removed", func(t *testing.T) { runPhase(t, false) })
}
