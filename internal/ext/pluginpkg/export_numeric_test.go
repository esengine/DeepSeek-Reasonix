package pluginpkg

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func TestExportNumericServerReferencesExpandInInstalledConfig(t *testing.T) {
	for _, server := range []string{"123-docs", "9", "docs"} {
		t.Run(server, func(t *testing.T) {
			home := testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			source := testenv.TempDir(t)
			manifest, err := json.Marshal(map[string]any{
				"apiVersion": ManifestAPIVersionV2, "name": "numeric-export", "version": "1.0.0",
				"contributes": map[string]any{"mcpServers": map[string]any{server: map[string]any{
					"type": "http", "url": "https://fixture.invalid/mcp",
					"headers": map[string]string{"Authorization": "Bearer author-fixture"},
					"env":     map[string]string{"TOKEN": "author-env", "EXISTING": "${RECIPIENT_EXISTING}"},
				}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			writeExportFile(t, filepath.Join(source, NativeManifest), string(manifest))
			if _, _, err := ParseDir(source); err != nil {
				t.Fatalf("source package is not accepted: %v", err)
			}
			archive, required, err := Export("numeric-export", source)
			if err != nil {
				t.Fatal(err)
			}
			root := InstallRoot(home, "numeric-export")
			exported := exportedEntries(t, archive)["numeric-export/"+NativeManifest]
			writeExportFile(t, filepath.Join(root, NativeManifest), exported)
			pkg, _, err := ParseDir(root)
			if err != nil {
				t.Fatalf("exported package is not accepted: %v", err)
			}
			srv := pkg.Manifest.MCPServers[server]
			if len(required) != 3 || !slices.Contains(required, "RECIPIENT_EXISTING") || srv.Env["EXISTING"] != "${RECIPIENT_EXISTING}" {
				t.Fatalf("export variables = %v, existing reference = %q", required, srv.Env["EXISTING"])
			}
			for reference, value := range map[string]string{srv.Headers["Authorization"]: "Bearer recipient-fixture", srv.Env["TOKEN"]: "recipient-env"} {
				name := strings.TrimSuffix(strings.TrimPrefix(reference, "${"), "}")
				if !slices.Contains(required, name) {
					t.Fatalf("reference %q is missing from required variables %v", reference, required)
				}
				t.Setenv(name, value)
			}
			t.Setenv("RECIPIENT_EXISTING", "recipient-existing")
			if err := Upsert(home, InstalledPlugin{Name: "numeric-export", Root: RelativeRoot(home, root), Enabled: true}); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadForRoot(testenv.TempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, entry := range cfg.Plugins {
				if entry.Name != server {
					continue
				}
				found = true
				expanded := entry.ExpandedPlugin()
				if expanded.Headers["Authorization"] != "Bearer recipient-fixture" || expanded.Env["TOKEN"] != "recipient-env" || expanded.Env["EXISTING"] != "recipient-existing" {
					t.Fatalf("recipient expansion: Authorization=%q, TOKEN=%q, EXISTING=%q", expanded.Headers["Authorization"], expanded.Env["TOKEN"], expanded.Env["EXISTING"])
				}
			}
			if !found {
				t.Fatalf("installed MCP server %q is missing", server)
			}
		})
	}
}
