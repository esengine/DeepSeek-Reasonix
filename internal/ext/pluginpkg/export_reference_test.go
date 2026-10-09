package pluginpkg

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func TestExportWholeReferencesExpandInInstalledConfig(t *testing.T) {
	const variable = "REASONIX_EXPORT_REFERENCE"
	for _, tc := range []struct {
		name, value string
		reference   bool
	}{
		{name: "complete", value: "${" + variable + "}", reference: true},
		{name: "bare", value: "$" + variable},
		{name: "missing-close", value: "${" + variable},
		{name: "missing-open", value: "$" + variable + "}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, source := testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			t.Setenv(variable, "recipient-reference")
			t.Setenv("DOCS_AUTHORIZATION", "recipient-header")
			t.Setenv("DOCS_TOKEN", "recipient-env")
			manifest, err := json.Marshal(map[string]any{
				"apiVersion": ManifestAPIVersionV2, "name": "reference-export",
				"contributes": map[string]any{"mcpServers": map[string]any{"docs": map[string]any{
					"type": "http", "url": "https://fixture.invalid/mcp",
					"headers": map[string]string{"Authorization": tc.value},
					"env":     map[string]string{"TOKEN": tc.value},
				}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			writeExportFile(t, filepath.Join(source, NativeManifest), string(manifest))
			if _, _, err := ParseDir(source); err != nil {
				t.Fatalf("source package is not accepted: %v", err)
			}
			archive, required, err := Export("reference-export", source)
			if err != nil {
				t.Fatal(err)
			}
			root := InstallRoot(home, "reference-export")
			writeExportFile(t, filepath.Join(root, NativeManifest), exportedEntries(t, archive)["reference-export/"+NativeManifest])
			pkg, _, err := ParseDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := Upsert(home, InstalledPlugin{Name: "reference-export", Root: RelativeRoot(home, root), Enabled: true}); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadForRoot(testenv.TempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, entry := range cfg.Plugins {
				if entry.Name != "docs" {
					continue
				}
				found = true
				wantHeader, wantEnv := "recipient-header", "recipient-env"
				if tc.reference {
					wantHeader, wantEnv = "recipient-reference", "recipient-reference"
				}
				expanded := entry.ExpandedPlugin()
				if expanded.Headers["Authorization"] != wantHeader || expanded.Env["TOKEN"] != wantEnv {
					t.Errorf("recipient expansion: Authorization=%q, TOKEN=%q", expanded.Headers["Authorization"], expanded.Env["TOKEN"])
				}
			}
			if !found {
				t.Fatal("installed MCP server is missing")
			}
			srv := pkg.Manifest.MCPServers["docs"]
			if tc.reference {
				if !slices.Equal(required, []string{variable}) || srv.Headers["Authorization"] != tc.value || srv.Env["TOKEN"] != tc.value {
					t.Errorf("complete reference changed: required=%v, server=%+v", required, srv)
				}
			} else if !slices.Equal(required, []string{"DOCS_AUTHORIZATION", "DOCS_TOKEN"}) || srv.Headers["Authorization"] != "${DOCS_AUTHORIZATION}" || srv.Env["TOKEN"] != "${DOCS_TOKEN}" {
				t.Errorf("literal not replaced: required=%v, header=%q, env=%q", required, srv.Headers["Authorization"], srv.Env["TOKEN"])
			}
		})
	}
}
