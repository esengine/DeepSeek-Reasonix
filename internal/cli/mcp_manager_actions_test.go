package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/plugin"
)

func TestSplitEditorCommandUsesStaticShellWords(t *testing.T) {
	got, err := splitEditorCommand(`code --goto "dir/file name.go:12"`)
	if err != nil {
		t.Fatalf("splitEditorCommand: %v", err)
	}
	want := []string{"code", "--goto", "dir/file name.go:12"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestSplitEditorCommandRejectsShellControl(t *testing.T) {
	if _, err := splitEditorCommand(`vim file; rm -rf tmp`); err == nil {
		t.Fatal("splitEditorCommand accepted shell control syntax")
	}
}

func TestMCPActionsOfferOAuthOnlyForEligibleHTTPServers(t *testing.T) {
	tests := []struct {
		name           string
		transport      string
		url            string
		authConfigured bool
		want           mcpAction
	}{
		{name: "streamable HTTP", transport: "http", url: "https://mcp.example.test/mcp", want: mcpActionAuth},
		{name: "stdio", transport: "stdio", want: mcpActionConnect},
		{name: "legacy SSE", transport: "sse", url: "https://mcp.example.test/sse", want: mcpActionConnect},
		{name: "static authentication", transport: "http", url: "https://mcp.example.test/mcp", authConfigured: true, want: mcpActionConnect},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actions := mcpActionsFor(mcpServerView{
				Name: "server", Transport: tc.transport, URL: tc.url, Status: "failed",
				Error: "authentication required", AuthStatus: "required", authConfigured: tc.authConfigured,
			}, "")
			if len(actions) == 0 || actions[0].kind != tc.want {
				t.Fatalf("actions = %+v, want first action %q", actions, tc.want)
			}
		})
	}
}

func TestClearMCPAuthenticationUsesControllerWorkspace(t *testing.T) {
	isolateCLIConfigHome(t)
	controllerRoot := t.TempDir()
	cwdRoot := t.TempDir()
	const pluginConfig = `
[[plugins]]
name = "dida"
type = "http"
url = "https://example.test/mcp?access_token=TOKEN&workspace=main"
auto_start = false
`
	writeConfig := func(root, token string) {
		t.Helper()
		raw := minimalTestModelTOML + strings.ReplaceAll(pluginConfig, "TOKEN", token)
		if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig(controllerRoot, "controller-token")
	writeConfig(cwdRoot, "cwd-token")
	t.Chdir(cwdRoot)

	ctrl, err := setupProfile(context.Background(), "", 0, false, event.Discard, controllerRoot)
	if err != nil {
		t.Fatalf("setupProfile: %v", err)
	}
	defer ctrl.Close()
	oauthState := filepath.Join(plugin.MCPStateDir(config.ReasonixHomeDir(), controllerRoot, "dida"), "oauth.json")
	if err := os.MkdirAll(filepath.Dir(oauthState), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oauthState, []byte(`{"version":1,"access_token":"private"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	pending := []string{}
	model := chatTUI{ctrl: ctrl, pendingCommit: &pending}
	model.clearMCPAuthentication(mcpServerView{Name: "dida"})

	controllerRaw, err := os.ReadFile(filepath.Join(controllerRoot, "reasonix.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(controllerRaw), "controller-token") ||
		!strings.Contains(string(controllerRaw), "workspace=main") {
		t.Fatalf("controller config authentication was not cleared:\n%s", controllerRaw)
	}
	cwdRaw, err := os.ReadFile(filepath.Join(cwdRoot, "reasonix.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cwdRaw), "cwd-token") {
		t.Fatalf("cwd config was unexpectedly modified:\n%s", cwdRaw)
	}
	if _, err := os.Stat(oauthState); !os.IsNotExist(err) {
		t.Fatalf("controller OAuth state was not cleared: %v", err)
	}
}
