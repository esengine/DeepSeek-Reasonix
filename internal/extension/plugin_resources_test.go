package extension

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/pluginpkg"
)

// writePluginFile writes one file inside a plugin package fixture.
func writePluginFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// parseResourcesPlugin builds a v1 package with prompts and themes on disk.
func parseResourcesPlugin(t *testing.T) pluginpkg.Package {
	t.Helper()
	root := t.TempDir()
	writePluginFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "res",
  "contributes": {
    "prompts": ["prompts"],
    "themes": ["themes/*.reasonix-theme"]
  }
}`)
	writePluginFile(t, filepath.Join(root, "prompts", "plan.md"), "---\ndescription: plan\n---\nPlan $ARGUMENTS")
	writePluginFile(t, filepath.Join(root, "themes", "neon.reasonix-theme"), "theme bytes")
	pkg, _, err := pluginpkg.ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	return pkg
}

func TestPluginResourcesContributor(t *testing.T) {
	pkg := parseResourcesPlugin(t)
	contributions, err := PluginResourcesContributor(pkg).Contribute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(contributions) != 2 {
		t.Fatalf("contributions = %+v, want one prompt and one theme", contributions)
	}
	byKind := map[ContributionKind]Contribution{}
	for _, c := range contributions {
		byKind[c.Kind] = c
		if c.Source.Scope != ScopePlugin || c.Source.PluginID != "res" {
			t.Fatalf("contribution source = %+v, want plugin tier owned by res", c.Source)
		}
	}
	prompt, ok := byKind[KindPrompt]
	if !ok {
		t.Fatalf("no KindPrompt contribution: %+v", contributions)
	}
	if prompt.ID != "plugin:res:plan" {
		t.Fatalf("prompt ID = %q, want plugin:res:plan", prompt.ID)
	}
	if _, ok := prompt.Payload.(pluginpkg.PromptRef); !ok {
		t.Fatalf("prompt payload = %T, want pluginpkg.PromptRef", prompt.Payload)
	}
	theme, ok := byKind[KindTheme]
	if !ok {
		t.Fatalf("no KindTheme contribution: %+v", contributions)
	}
	if theme.ID != "plugin:res:neon" {
		t.Fatalf("theme ID = %q, want plugin:res:neon", theme.ID)
	}
	if _, ok := theme.Payload.(pluginpkg.ThemeRef); !ok {
		t.Fatalf("theme payload = %T, want pluginpkg.ThemeRef", theme.Payload)
	}
}

// TestManifestV1ValidatorListsStayInSync pins the validation lists pluginpkg
// duplicates (interceptor points, replacement slots, priority bounds)
// against this package's source of truth. pluginpkg cannot import extension
// (extension -> hook -> pluginpkg would become an import cycle), so the
// lists live in two places; this test makes a one-sided edit fail. It builds
// a manifest whose runtime enumerates every declared value and requires the
// pluginpkg parser to accept all of them.
func TestManifestV1ValidatorListsStayInSync(t *testing.T) {
	points := []string{
		string(PointSessionStart), string(PointSessionEnd), string(PointSessionLoad),
		string(PointSessionSave), string(PointSessionRotate), string(PointInputReceive),
		string(PointAgentBeforeStart), string(PointSystemPromptBuild),
		string(PointContextPrepare), string(PointProviderRequest),
		string(PointProviderResponse), string(PointToolBefore), string(PointToolAfter),
		string(PointPermissionDecision), string(PointCompactionPrepare),
		string(PointCompactionComplete), string(PointFrontendEvent),
	}
	slots := []string{
		string(SlotSystemPrompt), string(SlotContext), string(SlotProviderRequest),
		string(SlotProviderResponse), string(SlotCompaction), string(SlotSessionPolicy),
		string(SlotPermission), string(SlotFrontendEvents),
		string(SlotTool("bash")), string(SlotProviderRef("openai/gpt-5")),
	}
	for _, p := range points {
		if !knownInterceptorPoint(InterceptorPoint(p)) {
			t.Fatalf("test bug: %q is not a declared point", p)
		}
	}
	for _, s := range slots {
		if _, err := ParseSlot(s); err != nil {
			t.Fatalf("test bug: slot %q does not parse: %v", s, err)
		}
	}
	quoted := func(values []string) string {
		out := make([]string, len(values))
		for i, v := range values {
			out[i] = `"` + v + `"`
		}
		return strings.Join(out, ", ")
	}
	root := t.TempDir()
	manifest := `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "sync-check",
  "runtime": {
    "command": "sync-runtime",
    "priority": 1000,
    "intercepts": [` + quoted(points) + `],
    "replaces": [` + quoted(slots) + `]
  }
}`
	writePluginFile(t, filepath.Join(root, pluginpkg.NativeManifest), manifest)
	pkg, _, err := pluginpkg.ParseDir(root)
	if err != nil {
		t.Fatalf("pluginpkg rejected extension-declared values (lists drifted): %v", err)
	}
	rt := pkg.Manifest.Runtime
	if rt == nil || len(rt.Intercepts) != len(points) || len(rt.Replaces) != len(slots) {
		t.Fatalf("runtime = %+v", rt)
	}
	if ValidatePriority(rt.Priority) != nil {
		t.Fatalf("pluginpkg accepted priority %d that extension rejects", rt.Priority)
	}
}
