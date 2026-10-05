package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/ext/extension/sidecar"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func TestInstalledSDKSidecarCanRunThenBeDisabledAndRemoved(t *testing.T) {
	source := testenv.TempDir(t)
	copyFixtureFile(t, filepath.Join(exampleRoot, pluginpkg.NativeManifest), filepath.Join(source, pluginpkg.NativeManifest), 0o644)
	binary := "full-sidecar"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	copyFixtureFile(t, examplePath, filepath.Join(source, "bin", binary), 0o755)

	parent := testenv.TempDir(t)
	home := filepath.Join(parent, ".reasonix")
	tool := installsource.NewTool(installsource.Options{
		ProjectRoot: testenv.TempDir(t), HomeDir: parent, RequireApprovedPlan: true,
	})
	args := map[string]any{"source": source, "kind": "plugin", "scope": "global", "mode": "copy"}
	type installResult struct {
		Status  string `json:"status"`
		Applied bool   `json:"applied"`
		PlanID  string `json:"planId"`
	}
	call := func() installResult {
		t.Helper()
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		out, err := tool.Execute(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		var result installResult
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	plan := call()
	if plan.Status != "planned" || plan.Applied || plan.PlanID == "" {
		t.Fatalf("preview = %+v", plan)
	}
	args["apply"] = true
	args["planId"] = plan.PlanID
	done := call()
	if done.Status != "done" || !done.Applied {
		t.Fatalf("install = %+v", done)
	}
	installed, warnings := pluginpkg.LoadInstalled(home)
	if len(installed) != 1 || len(warnings) != 0 || installed[0].Installed.Name != testPluginID {
		t.Fatalf("installed packages = %+v, warnings = %v", installed, warnings)
	}
	manager, warnings, err := sidecar.StartPackagesWithPlan(t.Context(), home, protocol.SessionContext{
		SessionID: "sess-installed", WorkspaceRoot: source, Generation: 1,
	}, nil, nil, nil)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("start installed sidecar: manager=%v warnings=%v err=%v", manager, warnings, err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	client := manager.Client(testPluginID)
	if client == nil {
		t.Fatal("installed sidecar did not start")
	}
	result, err := client.Intercept(t.Context(), protocol.EventInputReceive, json.RawMessage(`{"text":"/fs hello"}`), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var replacement struct {
		Text string `json:"text"`
	}
	decodeReplacement(t, result, &replacement)
	if replacement.Text != "hello [rewritten by fullsidecar]" {
		t.Fatalf("intercept replacement = %q", replacement.Text)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if err := pluginpkg.SetEnabled(home, testPluginID, false); err != nil {
		t.Fatal(err)
	}
	if packages, warnings := sidecar.LoadRuntimePackages(home); len(packages) != 0 || len(warnings) != 0 {
		t.Fatalf("disabled sidecar loaded packages=%d warnings=%v", len(packages), warnings)
	}
	args = map[string]any{"op": "uninstall", "name": testPluginID, "scope": "global"}
	removed := call()
	if removed.Status != "done" || !removed.Applied {
		t.Fatalf("remove = %+v", removed)
	}
	if _, err := os.Stat(pluginpkg.InstallRoot(home, testPluginID)); !os.IsNotExist(err) {
		t.Fatalf("removed sidecar still on disk: %v", err)
	}
	if packages, warnings := sidecar.LoadRuntimePackages(home); len(packages) != 0 || len(warnings) != 0 {
		t.Fatalf("removed sidecar loaded packages=%d warnings=%v", len(packages), warnings)
	}
}
