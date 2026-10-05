package installsource

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIssue4038MCPManifestInChineseDirectory(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "中文目录")
	writeFile(t, filepath.Join(source, ".mcp.json"), `{"mcpServers":{"local-server":{"command":"/bin/echo","args":["ok"]}}}`)
	resp := execInstall(t, NewTool(Options{ProjectRoot: root, HomeDir: t.TempDir()}), map[string]any{"source": source, "kind": "mcp"})
	if !resp.OK || len(resp.Actions) != 1 || resp.Actions[0].Name != "local-server" {
		t.Fatalf("Chinese directory manifest plan: %+v", resp)
	}
}

func TestIssue4038MCPExecutableInChineseDirectory(t *testing.T) {
	root := t.TempDir()
	filename, body := "server", "#!/bin/sh\nprintf ok\n"
	if runtime.GOOS == "windows" {
		filename, body = "server.cmd", "@echo off\r\necho ok\r\n"
	}
	source := filepath.Join(root, "中文目录", filename)
	writeFile(t, source, body)
	if err := os.Chmod(source, 0o755); err != nil {
		t.Fatal(err)
	}
	resp := execInstall(t, NewTool(Options{ProjectRoot: root, HomeDir: t.TempDir()}), map[string]any{"source": source, "kind": "mcp"})
	if !resp.OK || len(resp.Actions) != 1 || resp.Actions[0].Name != "server" || resp.Actions[0].Command != source {
		t.Fatalf("Chinese directory executable plan: %+v", resp)
	}
}

func TestIssue4038MCPServerIDRemainsASCII(t *testing.T) {
	if _, _, err := parseMCPJSON([]byte(`{"mcpServers":{"中文服务":{"command":"/bin/echo"}}}`)); err == nil {
		t.Fatal("Unicode MCP server ID must not enter provider-visible tool names")
	}
}
