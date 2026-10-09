package pluginpkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/testenv"
)

func TestExportUsesSupportedJSONEncodings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		encoding fileencoding.Kind
	}{
		{"utf8", fileencoding.UTF8}, {"utf8-bom", fileencoding.UTF8BOM},
		{"utf16le", fileencoding.UTF16LE}, {"utf16be", fileencoding.UTF16BE},
		{"utf16le-no-bom", fileencoding.UTF16LENoBOM}, {"utf16be-no-bom", fileencoding.UTF16BENoBOM},
		{"gb18030", fileencoding.GB18030}, {"gbk", fileencoding.GBK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const manifest = `{"apiVersion":"reasonix.io/plugin/v2","name":"encoded","description":"中文插件","mcpServers":{"docs":{"command":"docs","env":{"TOKEN":"fixture-value"}}}}`
			raw, err := fileencoding.Encode(manifest, tc.encoding)
			if err != nil {
				t.Fatal(err)
			}
			root := testenv.TempDir(t)
			path := filepath.Join(root, NativeManifest)
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			pkg, _, err := ParseDir(root)
			if err != nil || pkg.Manifest.Description != "中文插件" {
				t.Fatalf("installed parser: %+v, %v", pkg, err)
			}
			archive, required, err := Export("encoded", root)
			if err != nil {
				t.Fatal(err)
			}
			body := exportedEntries(t, archive)["encoded/"+NativeManifest]
			var out struct {
				Description string `json:"description"`
				MCPServers  map[string]struct {
					Env map[string]string `json:"env"`
				} `json:"mcpServers"`
			}
			if err := json.Unmarshal([]byte(body), &out); err != nil {
				t.Fatal(err)
			}
			if out.Description != "中文插件" || out.MCPServers["docs"].Env["TOKEN"] != "${DOCS_TOKEN}" || strings.Join(required, ",") != "DOCS_TOKEN" {
				t.Fatalf("export=%s required=%v", body, required)
			}
			original, err := os.ReadFile(path)
			if err != nil || string(original) != string(raw) {
				t.Fatalf("source rewritten: %v", err)
			}
		})
	}
}

func TestExportMixedCaseCredentialFields(t *testing.T) {
	for _, tc := range []struct {
		name, servers, env, headers string
		encoding                    fileencoding.Kind
	}{
		{"utf16le-title", "McpServers", "Env", "Headers", fileencoding.UTF16LE},
		{"utf16le-upper", "MCPSERVERS", "ENV", "HEADERS", fileencoding.UTF16LE},
		{"utf16le-fold", "mcpServerſ", "eNv", "headerſ", fileencoding.UTF16LE},
		{"gb18030-mixed", "mCpSeRvErS", "eNv", "hEaDeRs", fileencoding.GB18030},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`{"apiVersion":"reasonix.io/plugin/v2","name":"encoded","description":"中文插件",%q:{"docs":{"command":"docs",%q:{"TOKEN":"fixture-token"}},"wiki":{"type":"http","url":"https://wiki.example",%q:{"Authorization":"fixture-header"}}},"runtime":{"command":"fixture-sidecar",%q:{"KEY":"fixture-runtime"}}}`, tc.servers, tc.env, tc.headers, tc.env)
			raw, err := fileencoding.Encode(manifest, tc.encoding)
			if err != nil {
				t.Fatal(err)
			}
			root := testenv.TempDir(t)
			file := filepath.Join(root, NativeManifest)
			if err := os.WriteFile(file, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			pkg, _, err := ParseDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if pkg.Manifest.MCPServers["docs"].Env["TOKEN"] != "fixture-token" || pkg.Manifest.MCPServers["wiki"].Headers["Authorization"] != "fixture-header" || pkg.Manifest.Runtime == nil || pkg.Manifest.Runtime.Env["KEY"] != "fixture-runtime" {
				t.Fatalf("accepted fields not decoded: %+v", pkg.Manifest)
			}
			archive, required, err := Export("encoded", root)
			if err != nil {
				t.Fatal(err)
			}
			body := exportedEntries(t, archive)["encoded/"+NativeManifest]
			if strings.Join(required, ",") != "DOCS_TOKEN,KEY,WIKI_AUTHORIZATION" || strings.Contains(body, "fixture-token") || strings.Contains(body, "fixture-header") || strings.Contains(body, "fixture-runtime") {
				t.Fatalf("exported body=%s, required=%v", body, required)
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(body), &doc); err != nil {
				t.Fatal(err)
			}
			servers := doc[tc.servers].(map[string]any)
			if servers["docs"].(map[string]any)[tc.env].(map[string]any)["TOKEN"] != "${DOCS_TOKEN}" || servers["wiki"].(map[string]any)[tc.headers].(map[string]any)["Authorization"] != "${WIKI_AUTHORIZATION}" || doc["runtime"].(map[string]any)[tc.env].(map[string]any)["KEY"] != "${KEY}" || doc["description"] != "中文插件" {
				t.Fatalf("field spelling/metadata/value changed: %s", body)
			}
			original, err := os.ReadFile(file)
			if err != nil || string(original) != string(raw) {
				t.Fatalf("source rewritten: %v", err)
			}
		})
	}
}
