package conformance

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/pluginpkg"
)

func copyFixtureFile(t *testing.T, src, dst string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		t.Fatalf("write fixture %s: %v", dst, err)
	}
}

// installExamplePackage assembles the built SDK example into the same layout
// as an installed plugin, persists enabled state, then reloads it exclusively
// through pluginpkg's v2 parser and installed-package inventory.
func installExamplePackage(t *testing.T) (string, pluginpkg.InstalledPackage) {
	t.Helper()
	home := t.TempDir()
	root := pluginpkg.InstallRoot(home, testPluginID)
	copyFixtureFile(t, filepath.Join(exampleRoot, pluginpkg.NativeManifest), filepath.Join(root, pluginpkg.NativeManifest), 0o644)
	binaryName := "full-sidecar"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	copyFixtureFile(t, examplePath, filepath.Join(root, "bin", binaryName), 0o755)

	pkg, warnings, err := pluginpkg.ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir(installed fullsidecar): %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("ParseDir warnings = %v", warnings)
	}
	installed := pluginpkg.InstalledPlugin{
		Name:         testPluginID,
		Root:         pluginpkg.RelativeRoot(home, root),
		Version:      pkg.Manifest.Version,
		ManifestKind: pkg.ManifestKind,
		Enabled:      true,
	}
	if err := pluginpkg.Upsert(home, installed); err != nil {
		t.Fatalf("Upsert installed fullsidecar: %v", err)
	}
	loaded, warnings := pluginpkg.LoadInstalled(home)
	if len(warnings) != 0 {
		t.Fatalf("LoadInstalled warnings = %v", warnings)
	}
	if len(loaded) != 1 {
		t.Fatalf("LoadInstalled = %d packages, want 1", len(loaded))
	}
	return home, loaded[0]
}

func TestInstallableFixtureManifestCoversEveryRuntimeSurface(t *testing.T) {
	_, item := installExamplePackage(t)
	pkg := item.Package
	if pkg.Manifest.APIVersion != pluginpkg.ManifestAPIVersionV2 {
		t.Fatalf("apiVersion = %q, want %q", pkg.Manifest.APIVersion, pluginpkg.ManifestAPIVersionV2)
	}
	if pkg.Manifest.Name != testPluginID || pkg.Manifest.Version != "1.0.0" {
		t.Fatalf("manifest identity = %q/%q", pkg.Manifest.Name, pkg.Manifest.Version)
	}
	rt := pkg.Manifest.Runtime
	if rt == nil || rt.Command != "${REASONIX_PLUGIN_ROOT}/bin/full-sidecar" {
		t.Fatalf("runtime = %+v", rt)
	}
	wantIntercepts := map[string]bool{"input.receive": true, "tool.before": true, "system_prompt.build": true, "session.start": true}
	if len(rt.Intercepts) != len(wantIntercepts) {
		t.Fatalf("runtime.intercepts = %v", rt.Intercepts)
	}
	for _, point := range rt.Intercepts {
		if !wantIntercepts[point] {
			t.Fatalf("unexpected runtime intercept %q", point)
		}
	}
	wantCapabilities := map[string]bool{"interceptors": true, "strategies": true, "providers": true, "ui": true}
	if len(rt.Capabilities) != len(wantCapabilities) {
		t.Fatalf("runtime.capabilities = %v", rt.Capabilities)
	}
	for _, capability := range rt.Capabilities {
		if !wantCapabilities[capability] {
			t.Fatalf("unexpected runtime capability %q", capability)
		}
	}
	if len(rt.Replaces) != 1 || rt.Replaces[0] != "system_prompt" {
		t.Fatalf("runtime.replaces = %v", rt.Replaces)
	}
	wantProvides := map[string]string{
		"plugin/full-sidecar/interceptors/default":     "",
		"plugin/full-sidecar/strategies/system_prompt": "",
		"plugin/full-sidecar/provider/fake/echo":       fixtureProviderSchemaHash,
		"plugin/full-sidecar/uiaction/demo":            fixtureUIActionSchemaHash,
	}
	if len(pkg.Manifest.Provides) != len(wantProvides) {
		t.Fatalf("manifest provides = %+v", pkg.Manifest.Provides)
	}
	for _, provided := range pkg.Manifest.Provides {
		key := provided.Namespace + "/" + provided.Kind + "/" + provided.ID
		wantHash, ok := wantProvides[key]
		if !ok || provided.SchemaHash != wantHash {
			t.Fatalf("provided capability %q has schemaHash %q", key, provided.SchemaHash)
		}
	}
}

func TestInstallableFixtureEnableDisableRemoveAndKernelContributions(t *testing.T) {
	home, _ := installExamplePackage(t)
	if err := pluginpkg.SetEnabled(home, testPluginID, false); err != nil {
		t.Fatalf("disable fixture: %v", err)
	}
	if packages, warnings := sidecar.LoadRuntimePackages(home); len(packages) != 0 || len(warnings) != 0 {
		t.Fatalf("disabled fixture loaded packages=%d warnings=%v", len(packages), warnings)
	}
	if err := pluginpkg.SetEnabled(home, testPluginID, true); err != nil {
		t.Fatalf("enable fixture: %v", err)
	}
	mgr, warnings, err := sidecar.StartPackages(context.Background(), home, protocol.SessionContext{
		SessionID: "sess-installed", WorkspaceRoot: "/ws", Generation: 1,
	}, nil)
	if err != nil {
		t.Fatalf("StartPackages: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	if len(warnings) != 0 {
		t.Fatalf("StartPackages warnings = %v", warnings)
	}
	want := map[string]bool{
		string(extension.KindInterceptor) + ":input.receive":       true,
		string(extension.KindInterceptor) + ":tool.before":         true,
		string(extension.KindInterceptor) + ":system_prompt.build": true,
		string(extension.KindInterceptor) + ":session.start":       true,
		string(extension.KindStrategy) + ":system_prompt":          true,
		string(extension.KindProvider) + ":fake/echo":              true,
		string(extension.KindUIAction) + ":demo":                   true,
	}
	contributions := mgr.Contributions()
	if len(contributions) != len(want) {
		t.Fatalf("contributions = %+v", contributions)
	}
	for _, contribution := range contributions {
		key := string(contribution.Kind) + ":" + contribution.ID
		if !want[key] {
			t.Fatalf("unexpected contribution %q", key)
		}
	}
	if _, ok, err := pluginpkg.Remove(home, testPluginID); err != nil || !ok {
		t.Fatalf("remove fixture: ok=%v err=%v", ok, err)
	}
	if packages, warnings := sidecar.LoadRuntimePackages(home); len(packages) != 0 || len(warnings) != 0 {
		t.Fatalf("removed fixture loaded packages=%d warnings=%v", len(packages), warnings)
	}
}
