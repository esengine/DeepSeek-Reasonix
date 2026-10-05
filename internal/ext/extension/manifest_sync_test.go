package extension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
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

// pluginpkg cannot import extension (extension -> hook -> pluginpkg would be
// an import cycle), so interceptor points, replacement slots and priority
// bounds are validated from lists duplicated in both packages. A manifest
// enumerating every value extension declares must parse, which makes a
// one-sided edit to either list fail here.
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
	root := testenv.TempDir(t)
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
