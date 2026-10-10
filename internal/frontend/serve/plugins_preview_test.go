package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func writeHostilePluginSource(t *testing.T, root string) {
	t.Helper()
	args, _ := json.Marshal([]string{"--serve", "\x1b[2K" + strings.Repeat("A", 4000)})
	version, _ := json.Marshal("1.0.0\x1b[2J‮" + strings.Repeat("9", 500))
	writePluginFile(t, filepath.Join(root, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"evil","version":`+string(version)+`,
"runtime":{"command":"${REASONIX_PLUGIN_ROOT}/bin/x","args":`+string(args)+`,"required":true}}`)
	writePluginFile(t, filepath.Join(root, "bin", "x"), "#!/bin/sh\n")
}

func TestPluginPlanRouteBoundsPackageText(t *testing.T) {
	_, _, base := pluginHome(t)
	src := testenv.TempDir(t)
	writeHostilePluginSource(t, src)

	resp := postJSON(t, base+"/plugins/plan", map[string]any{"source": src})
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, raw)
	}
	var plan struct {
		PreviewTruncated bool `json:"previewTruncated"`
		Actions          []struct {
			Version          string `json:"version"`
			PreviewTruncated bool   `json:"previewTruncated"`
			Runtime          struct {
				Args []string `json:"args"`
			} `json:"runtime"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.PreviewTruncated || len(plan.Actions) != 1 || !plan.Actions[0].PreviewTruncated {
		t.Fatalf("truncation not reported: %s", raw)
	}
	a := plan.Actions[0]
	if len([]rune(a.Version)) > 128 || !strings.HasPrefix(a.Version, `1.0.0\u{1b}[2J\u{202e}`) || !strings.HasSuffix(a.Version, "…") {
		t.Fatalf("version = %q", a.Version)
	}
	if len(a.Runtime.Args) != 2 || len([]rune(a.Runtime.Args[1])) > 1024 || !strings.HasPrefix(a.Runtime.Args[1], `\u{1b}[2K`) {
		t.Fatalf("runtime args = %q", a.Runtime.Args)
	}
	if strings.ContainsAny(raw, "\x1b‮") {
		t.Fatal("raw control characters reached the wire")
	}
}

func TestInstalledPluginListBoundsPackageText(t *testing.T) {
	home, _, base := pluginHome(t)
	root := filepath.Join(home, "plugins", "evil")
	writePluginFile(t, filepath.Join(root, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"evil","version":"1.0.0",
"hooks":{"SessionStart":[{"command":"run\u001b[2J‮ `+strings.Repeat("a", 3000)+`"}]},
"mcpServers":{"docs":{"command":"docs\u001b[31m‮","args":["--stdio"]}}}`)
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "evil", Root: "plugins/evil", ManifestKind: "reasonix", Version: "1\x1b[2J", Description: "d‮", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(base + "/plugins")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || len(raw) != 1 {
		t.Fatalf("body %s: %v", body, err)
	}
	var view pluginView
	_ = json.Unmarshal(raw[0], &view)
	if len(view.Hooks) != 1 || len([]rune(view.Hooks[0].Command)) > 1024 || !strings.HasPrefix(view.Hooks[0].Command, `run\u{1b}[2J\u{202e}`) {
		t.Fatalf("hooks = %+v", view.Hooks)
	}
	if len(view.MCPServers) != 1 || view.MCPServers[0].Command != `docs\u{1b}[31m\u{202e}` || view.Version != `1\u{1b}[2J` || view.Description != "d" {
		t.Fatalf("view = %+v", view)
	}
	if strings.ContainsAny(string(body), "\x1b‮") {
		t.Fatal("raw hidden characters reached the wire")
	}
}
