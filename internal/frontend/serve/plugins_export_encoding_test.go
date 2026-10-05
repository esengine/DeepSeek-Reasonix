package serve

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginExportRoundTripsSupportedJSONEncodings(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		encoding   fileencoding.Kind
	}{
		{"gb18030", `{"mcpServers":{"docs":{"command":"docs","env":{"TOKEN":"fixture-token"}},"wiki":{"type":"http","url":"https://wiki.example","title":"中文文档","headers":{"Authorization":"fixture-header"}}}}`, fileencoding.GB18030},
		{"utf16le-mixed-case", `{"McpServers":{"docs":{"command":"docs","Env":{"TOKEN":"fixture-token"}},"wiki":{"type":"http","url":"https://wiki.example","title":"中文文档","Headers":{"Authorization":"fixture-header"}}}}`, fileencoding.UTF16LE},
	} {
		t.Run(tc.name, func(t *testing.T) { checkPluginExportEncodingRoundTrip(t, tc.body, tc.encoding) })
	}
}

func checkPluginExportEncodingRoundTrip(t *testing.T, mcpBody string, mcpEncoding fileencoding.Kind) {
	t.Helper()
	home, _, base := pluginHome(t)
	source := testenv.TempDir(t)
	files := map[string][]byte{}
	for _, tc := range []struct {
		path, body string
		encoding   fileencoding.Kind
	}{
		{pluginpkg.ClaudeManifest, `{"name":"encoded-transport","description":"中文插件"}`, fileencoding.UTF16LE},
		{".mcp.json", mcpBody, mcpEncoding},
		{"fixture.json", `{"env":{"KEEP":"中文内容"}}`, fileencoding.UTF8BOM},
	} {
		raw, err := fileencoding.Encode(tc.body, tc.encoding)
		if err != nil {
			t.Fatal(err)
		}
		writePluginFile(t, filepath.Join(source, tc.path), string(raw))
		files[tc.path] = raw
	}
	install := func(path string) {
		t.Helper()
		resp := postJSON(t, base+"/plugins/plan", map[string]any{"source": path})
		plan := decodeInstallSource(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("plan status=%d: %v", resp.StatusCode, plan)
		}
		resp = postJSON(t, base+"/plugins/install", map[string]any{"source": path, "planId": plan["planId"]})
		result := decodeInstallSource(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || result["status"] != "done" {
			t.Fatalf("install status=%d: %v", resp.StatusCode, result)
		}
	}
	install(source)
	plugins := getPlugins(t, base)
	if len(plugins) != 1 || plugins[0].Description != "中文插件" || len(plugins[0].MCPServers) != 2 {
		t.Fatalf("installed plugins=%+v", plugins)
	}
	resp, err := http.Get(base + "/plugins/encoded-transport/export")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	archive, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s error=%v", resp.StatusCode, archive, err)
	}
	if got := resp.Header.Get("X-Reasonix-Required-Env"); got != "DOCS_TOKEN,WIKI_AUTHORIZATION" {
		t.Fatalf("required env=%q", got)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[f.Name] = data
	}
	manifest := entries["encoded-transport/"+pluginpkg.ClaudeManifest]
	mcp := entries["encoded-transport/.mcp.json"]
	if !json.Valid(manifest) || !bytes.Contains(manifest, []byte("中文插件")) ||
		!json.Valid(mcp) || !bytes.Contains(mcp, []byte("中文文档")) || !bytes.Contains(mcp, []byte("${DOCS_TOKEN}")) || !bytes.Contains(mcp, []byte("${WIKI_AUTHORIZATION}")) || bytes.Contains(mcp, []byte("fixture-token")) || bytes.Contains(mcp, []byte("fixture-header")) {
		t.Fatalf("exported manifest=%s mcp=%s", manifest, mcp)
	}
	if !bytes.Equal(entries["encoded-transport/fixture.json"], files["fixture.json"]) {
		t.Fatal("ordinary content was rewritten")
	}
	for path, raw := range files {
		for _, root := range []string{source, pluginpkg.InstallRoot(home, "encoded-transport")} {
			original, err := os.ReadFile(filepath.Join(root, path))
			if err != nil || !bytes.Equal(original, raw) {
				t.Fatalf("source file %s was rewritten: %v", path, err)
			}
		}
	}
	req, err := http.NewRequest(http.MethodDelete, base+"/plugins/encoded-transport", nil)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusOK {
		t.Fatalf("remove status=%d", deleted.StatusCode)
	}
	archivePath := filepath.Join(testenv.TempDir(t), "encoded.zip")
	if err := os.WriteFile(archivePath, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	install(archivePath)
	plugins = getPlugins(t, base)
	if len(plugins) != 1 || plugins[0].Description != "中文插件" || len(plugins[0].MCPServers) != 2 {
		t.Fatalf("reinstalled plugins=%+v", plugins)
	}
}
