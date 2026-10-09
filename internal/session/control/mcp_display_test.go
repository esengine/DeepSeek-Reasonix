package control

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/plugin"
)

// hostileServerName is a name a repository could put in .mcp.json.
const hostileServerName = "x\x1b]52;c;Y3VybCBldmlsfHNo\x07\r\nforged line\x00‮evil"

func assertPrintable(t *testing.T, where, text string, allowNewlines bool) {
	t.Helper()
	bad := []string{"\x1b", "\x07", "\r", "\x00", "‮"}
	if !allowNewlines {
		bad = append(bad, "\n")
	}
	for _, b := range bad {
		if strings.Contains(text, b) {
			t.Fatalf("%s carries %q: %q", where, b, text)
		}
	}
	if strings.Contains(text, "\nforged line") {
		t.Fatalf("%s lets a repository forge a line: %q", where, text)
	}
}

// What /mcp prints and what a refused connect says quote the repository's own
// server name; neither may carry it to a terminal as control sequences.
func TestMCPTextQuotesARepositoryServerNameSafely(t *testing.T) {
	isolateControlConfigHome(t)
	workspace := testenv.TempDir(t)
	body, err := json.Marshal(map[string]any{"mcpServers": map[string]any{hostileServerName: map[string]any{"command": "npx", "args": []string{"-y", "pkg\x1b[2J"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".mcp.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	host := plugin.NewHost()
	defer host.Close()
	ctrl := New(Options{Host: host, WorkspaceRoot: workspace})
	defer ctrl.Close()
	assertPrintable(t, "/mcp", ctrl.mcpListText(), true)
	_, err = ctrl.ConnectConfiguredMCPServer(hostileServerName)
	if !errors.Is(err, ErrMCPApprovalOwed) {
		t.Fatalf("connect = %v, want the approval-owed refusal", err)
	}
	assertPrintable(t, "the refusal", err.Error(), false)
}
