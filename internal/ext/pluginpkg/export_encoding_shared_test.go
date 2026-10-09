package pluginpkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/testenv"
)

func TestExportEncodedEndpointAndArgumentCredentials(t *testing.T) {
	for _, encoding := range []fileencoding.Kind{fileencoding.UTF8BOM, fileencoding.UTF16LE, fileencoding.UTF16BE, fileencoding.GB18030} {
		t.Run(fmt.Sprintf("encoding=%d", encoding), func(t *testing.T) {
			const manifest = `{"apiVersion":"reasonix.io/plugin/v2","name":"encoded","description":"中文插件","McpServers":{"remote":{"type":"http","URL":"https://fixture-user:fixture-password@example.test/mcp?tenant=demo&token=fixture-token","Headers":{"Authorization":"fixture-header"}},"process":{"command":"node","Args":["--password","fixture-argument","--mode","read"],"Env":{"KEY":"${EXISTING_KEY}"}}}}`
			raw, err := fileencoding.Encode(manifest, encoding)
			if err != nil {
				t.Fatal(err)
			}
			root := testenv.TempDir(t)
			file := filepath.Join(root, NativeManifest)
			if err := os.WriteFile(file, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			pkg, _, err := ParseDir(root)
			if err != nil || pkg.Manifest.MCPServers["remote"].URL != "https://fixture-user:fixture-password@example.test/mcp?tenant=demo&token=fixture-token" || !slices.Equal(pkg.Manifest.MCPServers["process"].Args, []string{"--password", "fixture-argument", "--mode", "read"}) {
				t.Fatalf("operational manifest changed: %+v err=%v", pkg.Manifest, err)
			}
			archive, required, err := Export("encoded", root)
			if err != nil {
				t.Fatal(err)
			}
			body := exportedEntries(t, archive)["encoded/"+NativeManifest]
			for _, secret := range []string{"fixture-user", "fixture-password", "fixture-token", "fixture-header", "fixture-argument"} {
				if strings.Contains(body, secret) {
					t.Fatalf("encoded export leaked %q: %s", secret, body)
				}
			}
			var out map[string]any
			if err := json.Unmarshal([]byte(body), &out); err != nil {
				t.Fatal(err)
			}
			servers := out["McpServers"].(map[string]any)
			remote := servers["remote"].(map[string]any)
			process := servers["process"].(map[string]any)
			if remote["URL"] != "https://example.test/mcp?tenant=demo&token=%3Credacted%3E" || process["Args"].([]any)[1] != "<redacted>" || process["Args"].([]any)[2] != "--mode" || process["Args"].([]any)[3] != "read" || out["description"] != "中文插件" {
				t.Fatalf("shared projection/metadata changed: %s", body)
			}
			if !slices.Equal(required, []string{"EXISTING_KEY", "REMOTE_AUTHORIZATION"}) {
				t.Fatalf("required variable identities changed: %v", required)
			}
			original, err := os.ReadFile(file)
			if err != nil || string(original) != string(raw) {
				t.Fatalf("source rewritten: %v", err)
			}
		})
	}
}
