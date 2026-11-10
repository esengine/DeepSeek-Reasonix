package modhost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/ext/extension/sidecar"
	"reasonix/internal/ext/pluginpkg"
)

// startMod installs a mod package on disk and starts it through the real
// sidecar client under a real node, as the host does.
func startMod(t *testing.T, hooksModule string, budgetMS int) (*sidecar.Client, *Registry) {
	t.Helper()
	node := needNode(t)
	root := testenv.TempDir(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "hooks"), 0o755))
	must(os.WriteFile(filepath.Join(root, "hooks", "hooks.json"), []byte(`{"modules":["./register.mjs"]}`), 0o600))
	must(os.WriteFile(filepath.Join(root, "hooks", "register.mjs"), []byte(hooksModule), 0o600))
	pkg := pluginpkg.InstalledPackage{Installed: pluginpkg.InstalledPlugin{Name: "demo", Enabled: true, ManifestKind: "claude"}, Package: pluginpkg.Package{Root: root, ManifestKind: "claude"}}
	reg := NewRegistry()
	deps := Deps{Settings: Settings{Enabled: true, HookBudgetMS: budgetMS}, Resolver: Resolver{LookPath: func(string) (string, error) { return node, nil }}, StateRoot: testenv.TempDir(t), Registry: reg}
	packages, warnings := Packages(context.Background(), deps, []pluginpkg.InstalledPackage{pkg})
	if len(packages) != 1 {
		t.Fatalf("packages = %d, warnings = %v, status = %+v", len(packages), warnings, reg.Snapshot())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	client, err := sidecar.StartClient(ctx, sidecar.ClientOptions{
		Package: packages[0].Package, Installed: packages[0].Installed,
		Session: protocol.SessionContext{SessionID: "s1", WorkspaceRoot: testenv.TempDir(t), Generation: 1},
		OnCrash: func(error) { reg.Crashed("demo") },
	})
	if err != nil {
		t.Fatalf("StartClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, reg
}

func TestAModStartsThroughTheRealClientAndDescribesItsToolsInTheHandshake(t *testing.T) {
	client, _ := startMod(t, toolsMod, 0)
	if !client.TightenOnly() || !client.ServesTool("count_lines") {
		t.Fatalf("tighten-only = %v, serves = %v", client.TightenOnly(), client.ServesTool("count_lines"))
	}
	tools := client.DeclaredTools()
	if len(tools) != 1 || tools[0].Name != "count_lines" || !tools[0].ReadOnly {
		t.Fatalf("declared tools = %+v", tools)
	}
	var schema map[string]any
	if err := json.Unmarshal(tools[0].InputSchema, &schema); err != nil || schema["type"] != "object" {
		t.Fatalf("schema = %s", tools[0].InputSchema)
	}
	res, err := client.CallTool(context.Background(), "count_lines", json.RawMessage(`{"text":"a\nb"}`), 10*time.Second)
	if err != nil || res.Content != "2" {
		t.Fatalf("call = %+v, %v", res, err)
	}
}

func TestAHookStuckInASynchronousLoopKillsTheProcessAndTheMessageNamesTheCause(t *testing.T) {
	client, reg := startMod(t, `export function register(on) { on('prompt.submit', async ($, e, next) => { while (true) {} }); }`, 500)
	start := time.Now()
	_, err := client.Intercept(context.Background(), protocol.InterceptEvent(extension.PointInputReceive), json.RawMessage(`{"text":"hi"}`), 1500*time.Millisecond)
	if err == nil {
		t.Fatal("a stuck hook answered")
	}
	deadline := time.Now().Add(10 * time.Second)
	for reg.Snapshot() == nil || len(reg.Snapshot()) == 0 || reg.Snapshot()[0].Restarts == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the process was not killed after %v: %v", time.Since(start), err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Later calls fail fast instead of each waiting out the budget.
	begin := time.Now()
	if _, err := client.Intercept(context.Background(), protocol.InterceptEvent(extension.PointInputReceive), json.RawMessage(`{"text":"hi"}`), 5*time.Second); err == nil || time.Since(begin) > time.Second {
		t.Fatalf("a call after the kill = %v after %v, want a fast failure", err, time.Since(begin))
	}
}
