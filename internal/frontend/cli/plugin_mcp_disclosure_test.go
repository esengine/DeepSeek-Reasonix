package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginShowRedactsMCPQueryCredentials(t *testing.T) {
	for _, format := range []string{"reasonix", "claude"} {
		t.Run(format, func(t *testing.T) {
			home := testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			name := "show-" + format
			root := pluginpkg.InstallRoot(home, name)
			servers := map[string]any{
				"token":    map[string]any{"type": "http", "url": "https://Example.test/Case/mcp?mode=KeepCase&token=fixture-token"},
				"encoded":  map[string]any{"type": "http", "url": "https://Example.test/mcp?%61pi_key=encoded%2Dsecret%2Fextra"},
				"repeated": map[string]any{"type": "sse", "url": "https://Example.test/sse?token=fixture-first&token=fixture-second"},
				"plain":    map[string]any{"type": "http", "url": "https://Example.test/Case/mcp?mode=KeepCase&limit=20"},
				"stdio":    map[string]any{"command": "fixture-mcp-server"},
			}
			files := map[string][]byte{}
			write := func(path string, value any) {
				t.Helper()
				body, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				files[path] = body
				writePluginTestFile(t, path, string(body))
			}
			if format == "reasonix" {
				write(filepath.Join(root, pluginpkg.NativeManifest), map[string]any{"apiVersion": "reasonix.io/plugin/v2", "name": name, "contributes": map[string]any{"mcpServers": servers}})
			} else {
				write(filepath.Join(root, ".claude-plugin", "plugin.json"), map[string]any{"name": name})
				write(filepath.Join(root, ".mcp.json"), map[string]any{"mcpServers": servers})
			}
			for _, enabled := range []bool{true, false} {
				if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: name, Root: pluginpkg.RelativeRoot(home, root), ManifestKind: format, Enabled: enabled}); err != nil {
					t.Fatal(err)
				}
				out := captureStdout(t, func() {
					if rc := pluginCommand([]string{"show", name}); rc != 0 {
						t.Fatalf("show rc=%d", rc)
					}
				})
				for _, secret := range []string{"fixture-token", "fixture-first", "fixture-second", "encoded%2Dsecret%2Fextra", "encoded-secret/extra"} {
					if strings.Contains(out, secret) {
						t.Errorf("enabled=%v show disclosed %q:\n%s", enabled, secret, out)
					}
				}
				for _, want := range []string{
					"name: " + name, fmt.Sprintf("enabled: %t", enabled), "mcpServers: 5",
					"https://Example.test/Case/mcp?mode=KeepCase&token=%3Credacted%3E",
					"https://Example.test/mcp?api_key=%3Credacted%3E",
					"https://Example.test/sse?token=%3Credacted%3E",
					"https://Example.test/Case/mcp?mode=KeepCase&limit=20", "stdio\tfixture-mcp-server",
				} {
					if !strings.Contains(out, want) {
						t.Errorf("enabled=%v show missing %q:\n%s", enabled, want, out)
					}
				}
				for path, want := range files {
					if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
						t.Errorf("show changed %s: err=%v", path, err)
					}
				}
			}
		})
	}
}
