package boot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func TestEffectInstalledSDKSidecarLifecycle(t *testing.T) {
	example, err := filepath.Abs(filepath.Join("..", "..", "..", "sdk", "go", "examples", "starterextension"))
	if err != nil {
		t.Fatal(err)
	}
	source := robustTempDir(t)
	binary := filepath.Join(source, "bin", "starter-extension.exe")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	buildCtx, cancelBuild := context.WithTimeout(t.Context(), time.Minute)
	cmd := exec.CommandContext(buildCtx, "go", "build", "-o", binary, ".")
	cmd.Dir = example
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	cancelBuild()
	if err != nil {
		t.Fatalf("build SDK starterextension: %v\n%s", err, out)
	}
	manifest, err := os.ReadFile(filepath.Join(example, pluginpkg.NativeManifest))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, pluginpkg.NativeManifest), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}

	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-effect-installed-sdk"
model = "x"
`)
	approveWorkspace(t, workspace)
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
	runInstaller := func(request map[string]any) string {
		t.Helper()
		args, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		out, err := installer.Execute(t.Context(), args)
		if err != nil {
			t.Fatalf("install_source: %v", err)
		}
		return out
	}
	request := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "scope": "global"}
	for _, apply := range []bool{false, true} {
		request["apply"] = apply
		out := runInstaller(request)
		var result struct {
			OK      bool   `json:"ok"`
			Applied bool   `json:"applied"`
			Status  string `json:"status"`
			PlanID  string `json:"planId"`
			Actions []struct {
				Kind      string `json:"kind"`
				Name      string `json:"name"`
				RiskLevel string `json:"riskLevel"`
				Runtime   *struct {
					FullTrust bool `json:"fullTrust"`
				} `json:"runtime"`
			} `json:"actions"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil || !result.OK || result.Applied != apply || result.PlanID == "" {
			t.Fatalf("install_source apply=%t: %s, err=%v", apply, out, err)
		}
		if len(result.Actions) != 1 || result.Actions[0].Kind != "plugin" || result.Actions[0].Name != "starter-extension" {
			t.Fatalf("install actions = %s", out)
		}
		if !apply && (result.Status != "planned" || result.Actions[0].RiskLevel != "high" || result.Actions[0].Runtime == nil || !result.Actions[0].Runtime.FullTrust) {
			t.Fatalf("runtime preview lost its full-trust declaration: %s", out)
		}
		if apply && result.Status != "done" {
			t.Fatalf("installation = %s", out)
		}
		request["planId"] = result.PlanID
	}
	installed, found, err := pluginpkg.FindInstalled(reasonixHome, "starter-extension")
	if err != nil || !found || !installed.Enabled {
		t.Fatalf("installed package = %+v, found=%t, err=%v", installed, found, err)
	}
	root := pluginpkg.ResolveRoot(reasonixHome, installed.Root)
	if root == source {
		t.Fatal("copy installation still points at its source")
	}
	for name, want := range map[string][]byte{pluginpkg.NativeManifest: manifest, filepath.Join("bin", "starter-extension.exe"): built} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || sha256.Sum256(got) != sha256.Sum256(want) {
			t.Fatalf("copied %s differs from the SDK fixture, err=%v", name, err)
		}
	}
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}

	rec := &effectRecordingProvider{}
	provider.Register("boot-effect-installed-sdk", func(provider.Config) (provider.Provider, error) { return rec, nil })
	var prefix string
	runPhase := func(t *testing.T, enabled bool) {
		t.Helper()
		res, err := BuildRuntime(t.Context(), Options{Sink: event.Discard})
		if err != nil {
			t.Fatal(err)
		}
		defer res.Controller.Close()
		if enabled {
			if res.Extensions == nil || res.Extensions.Client("starter-extension") == nil {
				t.Fatal("installed SDK sidecar did not start")
			}
			client := res.Extensions.Client("starter-extension")
			defer func() {
				res.Controller.Close()
				waitForCond(t, "installed SDK sidecar exit", 10*time.Second, client.Exited)
				if !res.Runtime.Closed() {
					t.Fatal("SDK runtime was not closed")
				}
			}()
		} else if res.Extensions != nil && res.Extensions.Client("starter-extension") != nil {
			t.Fatal("disabled or removed SDK sidecar still started")
		}
		for _, input := range []string{"explain sidecars", "ordinary input"} {
			before := len(rec.requests())
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			err := res.Controller.Run(ctx, input)
			cancel()
			if err != nil {
				t.Fatalf("Run %q: %v", input, err)
			}
			reqs := rec.requests()
			if len(reqs) <= before {
				t.Fatal("turn never reached the recording provider")
			}
			last := reqs[len(reqs)-1]
			var user string
			for _, msg := range last.Messages {
				if msg.Role == provider.RoleUser {
					user = msg.Content
				}
			}
			if strings.Contains(user, "[rewritten by starter-extension]") != enabled {
				t.Fatalf("provider input for %q, enabled=%t:\n%s", input, enabled, user)
			}
			if !strings.Contains(user, input) || !strings.Contains(user, "Current workspace: "+strconv.Quote(workspace)) {
				t.Fatalf("provider input lost the request or workspace context:\n%s", user)
			}
			if enabled && strings.Count(user, "[rewritten by starter-extension]") != 1 {
				t.Fatalf("provider input should carry exactly one SDK marker:\n%s", user)
			}
			current := systemMessage(last.Messages)
			if prefix == "" {
				prefix = current
			} else if !bytes.Equal([]byte(current), []byte(prefix)) {
				t.Fatal("SDK package lifecycle changed the cached system prefix")
			}
		}
	}
	t.Run("installed", func(t *testing.T) { runPhase(t, true) })
	t.Run("restarted", func(t *testing.T) { runPhase(t, true) })
	if err := pluginpkg.SetEnabled(reasonixHome, "starter-extension", false); err != nil {
		t.Fatal(err)
	}
	t.Run("disabled", func(t *testing.T) { runPhase(t, false) })
	if err := pluginpkg.SetEnabled(reasonixHome, "starter-extension", true); err != nil {
		t.Fatal(err)
	}
	t.Run("enabled_again", func(t *testing.T) { runPhase(t, true) })
	removed := runInstaller(map[string]any{"op": "uninstall", "kind": "plugin", "name": "starter-extension", "scope": "global"})
	var result struct {
		OK      bool `json:"ok"`
		Applied bool `json:"applied"`
	}
	if err := json.Unmarshal([]byte(removed), &result); err != nil || !result.OK || !result.Applied {
		t.Fatalf("uninstall = %s, err=%v", removed, err)
	}
	if _, found, err := pluginpkg.FindInstalled(reasonixHome, "starter-extension"); err != nil || found {
		t.Fatalf("removed SDK package still registered, found=%t, err=%v", found, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("copied SDK package still exists after removal: %v", err)
	}
	t.Run("removed", func(t *testing.T) { runPhase(t, false) })
}
