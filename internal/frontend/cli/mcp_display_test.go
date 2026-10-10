package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/session/control"
)

const hostileServerName = "x\x1b]52;c;Y3VybCBldmlsfHNo\x07\r\nforged line\x00‮evil"

func assertTerminalSafe(t *testing.T, where, text string) {
	t.Helper()
	for _, b := range []string{"\x1b", "\x07", "\r", "\x00", "‮", "\nforged line"} {
		if strings.Contains(text, b) {
			t.Fatalf("%s carries %q: %q", where, b, text)
		}
	}
}

func hostileWorkspace(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	ws := t.TempDir()
	body, err := json.Marshal(map[string]any{"mcpServers": map[string]any{hostileServerName: map[string]any{"command": "npx", "args": []string{"-y", "pkg\x1b[2J"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".mcp.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(ws)
	return ws
}

// Whatever a warning quotes, `reasonix run` writes it to stderr as one plain
// line: the sanitising sits in the writer, so no caller can forget it.
func TestRunDiagnosticsStripControlSequences(t *testing.T) {
	var out, errOut bytes.Buffer
	sink := newRunOutputSink(&out, runOutputText)
	sink.errOut = &errOut
	sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: "project_mcp_awaiting_approval",
		Text: "MCP server " + hostileServerName + " is held", Detail: "command: npx pkg\x1b[2J"})
	if err := sink.Finalize("session", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	assertTerminalSafe(t, "run diagnostics", errOut.String())
	if lines := strings.Count(strings.TrimSpace(errOut.String()), "\n"); lines != 0 {
		t.Fatalf("one warning became %d lines: %q", lines+1, errOut.String())
	}
}

// serve's startup warning and `reasonix mcp list` print the repository's server
// name in a freshly cloned folder, before anyone enabled anything.
func TestServeWarningAndMCPListPrintAHostileNameSafely(t *testing.T) {
	ws := hostileWorkspace(t)
	host := plugin.NewHost()
	defer host.Close()
	ctrl := control.New(control.Options{Host: host, WorkspaceRoot: ws})
	defer ctrl.Close()
	var warn bytes.Buffer
	warnHeldProjectServers(&warn, ctrl)
	if warn.Len() == 0 {
		t.Fatal("serve printed no warning for the held server")
	}
	for line := range strings.SplitSeq(strings.TrimSpace(warn.String()), "\n") {
		assertTerminalSafe(t, "serve warning", line)
	}
	list := captureStdout(t, func() { mcpList() })
	if !strings.Contains(list, "forged line") || !strings.Contains(warn.String(), "forged line") {
		t.Fatalf("the held server is missing from the output:\nlist=%q\nwarn=%q", list, warn.String())
	}
	assertTerminalSafe(t, "mcp list", list)
}
