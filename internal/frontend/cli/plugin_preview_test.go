package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginInstallDryRunBoundsPackageText(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	source := testenv.TempDir(t)
	args, _ := json.Marshal([]string{"--serve", "\x1b[2K" + strings.Repeat("A", 4000)})
	version, _ := json.Marshal("1.0.0\x1b[2J‮" + strings.Repeat("9", 500))
	writePluginTestFile(t, filepath.Join(source, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"evil","version":`+string(version)+`,
"runtime":{"command":"${REASONIX_PLUGIN_ROOT}/bin/x","args":`+string(args)+`,"required":true}}`)
	writePluginTestFile(t, filepath.Join(source, "bin", "x"), "#!/bin/sh\n")

	out := captureStdout(t, func() {
		if rc := pluginCommand([]string{"install", source, "--dry-run"}); rc != 0 {
			t.Fatalf("rc = %d", rc)
		}
	})
	if strings.ContainsAny(out, "\x1b‮") {
		t.Fatalf("raw control characters reached the terminal: %q", out)
	}
	var plan struct {
		PreviewTruncated bool `json:"previewTruncated"`
		Actions          []struct {
			Version string `json:"version"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !plan.PreviewTruncated || len(plan.Actions) != 1 || len([]rune(plan.Actions[0].Version)) > 128 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestPluginShowBoundsPackageText(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	writePluginTestFile(t, filepath.Join(home, "plugins", "evil", "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"evil","version":"1.0.0",
"hooks":{"SessionStart":[{"command":"run\u001b[2J‮ `+strings.Repeat("a", 3000)+`"}]},
"mcpServers":{"docs":{"command":"docs\u001b[31m","args":["--stdio"]}}}`)
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "evil", Root: "plugins/evil", ManifestKind: "reasonix", Version: "1\x1b[2J", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"show", "evil"}, {"list"}, {"doctor", "evil"}} {
		out := captureStdout(t, func() {
			if rc := pluginCommand(args); rc != 0 {
				t.Fatalf("%v rc = %d", args, rc)
			}
		})
		if strings.ContainsAny(out, "\x1b‮") || len(out) > 6000 {
			t.Fatalf("%v prints raw package text (%d bytes): %q", args, len(out), out[:min(len(out), 200)])
		}
	}
}
