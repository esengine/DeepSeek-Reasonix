package boot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

// A headless run has no settings screen: a project server held off for the
// user must say so on the diagnostic stream, once per server, with the command
// and how to approve it — never vanish from the tool list in silence.
func TestEffectHeldProjectMCPServersAreReportedAtBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	logPath := filepath.Join(robustTempDir(t), "argv.log")
	declareRecordingServer(t, dir, ".mcp.json", logPath, "phase1")

	collect := func() []event.Event {
		t.Helper()
		var mu sync.Mutex
		var got []event.Event
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		ctrl, err := Build(ctx, Options{Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice && strings.HasPrefix(e.Code, "project_mcp_") {
				mu.Lock()
				got = append(got, e)
				mu.Unlock()
			}
		})})
		if err != nil {
			t.Fatal(err)
		}
		ctrl.Close()
		mu.Lock()
		defer mu.Unlock()
		return got
	}
	check := func(got []event.Event, code string) {
		t.Helper()
		if len(got) != 1 || got[0].Code != code || got[0].Level != event.LevelWarn {
			t.Fatalf("held-server notices = %+v, want one %s warning", got, code)
		}
		if !strings.Contains(got[0].Text, `"docs-helper"`) || !strings.Contains(got[0].Text, "reasonix mcp enable docs-helper") ||
			!strings.Contains(got[0].Detail, "phase1") && !strings.Contains(got[0].Detail, "phase2") {
			t.Fatalf("notice does not name the server, the command and how to approve: %+v", got[0])
		}
	}
	check(collect(), "project_mcp_awaiting_approval")

	cfg, err := config.LoadForRootReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.DefaultActivationStore().SetServerEnabled(cfg.Plugins[0], dir, config.ActivationProject, true); err != nil {
		t.Fatal(err)
	}
	if got := collect(); len(got) != 0 {
		t.Fatalf("an approved server was reported as held: %+v", got)
	}
	declareRecordingServer(t, dir, ".mcp.json", logPath, "phase2")
	check(collect(), "project_mcp_changed")

	ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	for _, h := range ctrl.MCPServerHealth() {
		if h.Name == "docs-helper" && h.Status != control.MCPHealthPending {
			t.Fatalf("changed server health = %s, want pending", h.Status)
		}
	}
}

// hostileName is a server name a repository could write into .mcp.json: an OSC
// 52 clipboard write, a forged line, a NUL and a bidi override.
const hostileName = "x\x1b]52;c;Y3VybCBldmlsfHNo\x07\r\nforged line\x00\u202eevil"

// The awaiting-approval notice fires in a freshly cloned repository with no
// enable at all, so the name it quotes must not reach a terminal as control
// sequences or extra lines.
func TestEffectHeldProjectServerNoticeCarriesNoControlSequences(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	body, err := json.Marshal(map[string]any{"mcpServers": map[string]any{hostileName: map[string]any{"command": "npx", "args": []string{"-y", "pkg\x1b[2J"}}}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "reasonix.toml", bindingTestProvider)
	writeFile(t, dir, ".mcp.json", string(body))
	approveWorkspaceAt(t, home, dir)
	var mu sync.Mutex
	var got []event.Event
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.FuncSink(func(e event.Event) {
		if strings.HasPrefix(e.Code, "project_mcp_") {
			mu.Lock()
			got = append(got, e)
			mu.Unlock()
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	ctrl.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("notices = %+v, want one for the hostile server", got)
	}
	for _, field := range []string{got[0].Text, got[0].Detail} {
		for _, bad := range []string{"\x1b", "\x07", "\r", "\n", "\x00", "\u202e"} {
			if strings.Contains(field, bad) {
				t.Fatalf("notice carries %q: %q", bad, field)
			}
		}
	}
}
