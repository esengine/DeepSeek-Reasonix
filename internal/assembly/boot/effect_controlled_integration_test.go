package boot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/checkenv"
	"reasonix/internal/session/control"
)

func controlledAutoIntegrationEffect(t *testing.T, image string, client checkenv.ClientSpec) {
	t.Helper()
	home, root := statedBootHome(t), t.TempDir()
	managed := filepath.Join(config.RootsForHome(home).DeliveryWorktreeDir(), "isolated")
	p := &hostCheckScriptProvider{isolationScriptProvider: isolationScriptProvider{dir: root, managed: managed}, allowShell: true}
	kind := "controlled-auto-integration-effect-" + t.Name()
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", fmt.Sprintf(`default_model="fixture"
[agent]
collaboration="shared"
verification_runtime=%q
[codegraph]
enabled=false
[[providers]]
name="fixture"
kind=%q
model="fixture"
`, image, kind))
	writeFile(t, root, "REASONIX.md", "## Reasonix host checks\n\n- verify: [ -f out.txt ]\n")
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "config", "core.autocrlf", "false")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "fixture")
	var mu sync.Mutex
	checks, modelApplies := 0, 0
	var hostErrors []string
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, VerificationClient: client, Sink: event.FuncSink(func(e event.Event) {
		mu.Lock()
		defer mu.Unlock()
		if e.Kind == event.ToolResult && e.Tool.Issuer == event.IssuedByHost && e.Tool.Err != "" {
			hostErrors = append(hostErrors, e.Tool.Err)
		}
		if e.Kind == event.ToolResult && e.Tool.Issuer == event.IssuedByHost && e.Tool.Name == "bash" && e.Tool.Err == "" {
			checks++
		}
		if e.Kind == event.ToolDispatch && e.Tool.Issuer != event.IssuedByHost && e.Tool.Name == "apply_isolated" {
			modelApplies++
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	bindPresenceSession(t, ctrl)
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := ctrl.Run(t.Context(), "delegate an isolated writer"); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "out.txt")); err != nil || string(raw) != "made in isolation\n" {
		mu.Lock()
		t.Logf("host check failures: %v", hostErrors)
		mu.Unlock()
		t.Fatalf("automatic integration did not publish: %q %v; task=%s", raw, err, p.childSaw)
	}
	mu.Lock()
	defer mu.Unlock()
	if checks != 2 || modelApplies != 0 {
		t.Fatalf("checks=%d model applies=%d", checks, modelApplies)
	}
	if !strings.Contains(p.childSaw, "verification belongs to that exact integrated snapshot") {
		t.Fatalf("missing snapshot-bound delivery: %s", p.childSaw)
	}
}
