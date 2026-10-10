package control

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
)

// Connecting a project server by name answers "may it run" only while nothing
// was decided. Once the user enabled one declaration and the project changed
// it, a connect or a reconnect must not approve the new one unseen: both are
// refused with the typed reason and the command, and nothing is contacted.
// /mcp says the same to a terminal user.
func TestConnectRefusesAProjectServerThatChangedSinceItWasEnabled(t *testing.T) {
	isolateControlConfigHome(t)
	workspace := testenv.TempDir(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	declare := func(url string) {
		if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"),
			fmt.Appendf(nil, "[[plugins]]\nname = \"project-docs\"\ntype = \"http\"\nurl = %q\n", url), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	declare("https://approved.invalid/mcp")
	loaded, err := config.LoadForRootReadOnly(workspace)
	if err != nil || len(loaded.Plugins) != 1 {
		t.Fatalf("load: %v", err)
	}
	if err := config.DefaultActivationStore().SetServerEnabled(loaded.Plugins[0], workspace, config.ActivationProject, true); err != nil {
		t.Fatal(err)
	}
	declare(server.URL)

	host := plugin.NewHost()
	defer host.Close()
	ctrl := New(Options{Host: host, WorkspaceRoot: workspace})
	defer ctrl.Close()

	for name, connect := range map[string]func(string) (int, error){
		"/mcp connect": ctrl.ConnectConfiguredMCPServer,
		"reconnect":    ctrl.ReconnectMCPServer,
	} {
		_, err := connect("project-docs")
		var owed *MCPApprovalOwedError
		if !errors.Is(err, ErrMCPApprovalOwed) || !errors.As(err, &owed) || owed.Reason != MCPApprovalChanged {
			t.Fatalf("%s = %v, want the typed approval-owed refusal", name, err)
		}
		if !strings.Contains(err.Error(), server.URL) || !strings.Contains(err.Error(), "reasonix mcp enable project-docs") {
			t.Fatalf("%s refusal does not show the command and how to approve it: %v", name, err)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("the changed declaration was contacted %d times", n)
	}
	if enabled, _ := ctrl.MCPServerEnabled("project-docs"); enabled {
		t.Fatal("a refused connect recorded an approval")
	}
	var reason MCPApprovalReason
	for _, h := range ctrl.MCPServerHealth() {
		if h.Name == "project-docs" {
			reason = h.Reason
		}
	}
	if reason != MCPApprovalChanged {
		t.Fatalf("health reason = %q, want %q", reason, MCPApprovalChanged)
	}
	list := ctrl.mcpListText()
	for _, want := range []string{"project-docs", string(MCPApprovalChanged), server.URL, "reasonix mcp enable project-docs"} {
		if !strings.Contains(list, want) {
			t.Fatalf("/mcp lacks %q:\n%s", want, list)
		}
	}
}

// Every pending state of a repository-declared server refuses a connect by
// name: never approved, and approved under another source that the same name
// now moved away from (which reads as never approved for the new source). A
// server from the user's own files keeps today's rule: connecting it is the
// answer, and it is written down.
func TestConnectRefusesEveryPendingProjectServerButAnswersForTheUsersOwn(t *testing.T) {
	isolateControlConfigHome(t)
	workspace := testenv.TempDir(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".mcp.json", `{"mcpServers":{"docs":{"type":"http","url":"https://approved.invalid/mcp"},"fresh":{"type":"http","url":"`+server.URL+`/fresh"}}}`)
	loaded, err := config.LoadForRootReadOnly(workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range loaded.Plugins {
		if p.Name == "docs" {
			if err := config.DefaultActivationStore().SetServerEnabled(p, workspace, config.ActivationProject, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(".mcp.json", `{"mcpServers":{"fresh":{"type":"http","url":"`+server.URL+`/fresh"}}}`)
	write("reasonix.toml", fmt.Sprintf("[[plugins]]\nname = \"docs\"\ntype = \"http\"\nurl = %q\n", server.URL+"/moved"))
	userConfig := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, fmt.Appendf(nil, "[[plugins]]\nname = \"mine\"\ntype = \"http\"\nurl = %q\nauto_start = false\n", server.URL+"/mine"), 0o600); err != nil {
		t.Fatal(err)
	}

	host := plugin.NewHost()
	defer host.Close()
	ctrl := New(Options{Host: host, WorkspaceRoot: workspace})
	defer ctrl.Close()
	for _, name := range []string{"docs", "fresh"} {
		_, err := ctrl.ConnectConfiguredMCPServer(name)
		var owed *MCPApprovalOwedError
		if !errors.As(err, &owed) || owed.Reason != MCPApprovalAwaiting || !strings.Contains(err.Error(), server.URL) {
			t.Fatalf("connect %s = %v, want the approval-owed refusal with its endpoint", name, err)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("a refused project server was contacted %d times", n)
	}
	_, _ = ctrl.ConnectConfiguredMCPServer("mine")
	if enabled, _ := ctrl.MCPServerEnabled("mine"); !enabled {
		t.Fatal("connecting a server from the user's own config no longer records the answer")
	}
}

// A project server the user switched off is not reopened by a connect or a
// reconnect by name, whether or not the project changed it since: that act
// shows no command, so it is refused with the one it would run.
func TestConnectRefusesAProjectServerTheUserSwitchedOff(t *testing.T) {
	isolateControlConfigHome(t)
	workspace := testenv.TempDir(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	declare := func(url string) {
		if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"),
			fmt.Appendf(nil, "[[plugins]]\nname = \"project-docs\"\ntype = \"http\"\nurl = %q\n", url), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	declare(server.URL + "/approved")
	loaded, err := config.LoadForRootReadOnly(workspace)
	if err != nil || len(loaded.Plugins) != 1 {
		t.Fatalf("load: %v", err)
	}
	store := config.DefaultActivationStore()
	if err := store.SetServerEnabled(loaded.Plugins[0], workspace, config.ActivationProject, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetServerEnabled(loaded.Plugins[0], workspace, config.ActivationProject, false); err != nil {
		t.Fatal(err)
	}
	host := plugin.NewHost()
	defer host.Close()
	for _, url := range []string{server.URL + "/approved", server.URL + "/changed"} {
		declare(url)
		ctrl := New(Options{Host: host, WorkspaceRoot: workspace})
		for name, connect := range map[string]func(string) (int, error){
			"/mcp connect": ctrl.ConnectConfiguredMCPServer,
			"reconnect":    ctrl.ReconnectMCPServer,
		} {
			_, err := connect("project-docs")
			if !errors.Is(err, ErrMCPApprovalOwed) || !strings.Contains(err.Error(), url) {
				t.Fatalf("%s of a switched-off server declaring %s = %v, want the approval-owed refusal with its endpoint", name, url, err)
			}
		}
		ctrl.Close()
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("a switched-off project server was contacted %d times", n)
	}
	if enabled, _ := store.IsEnabled(loaded.Plugins[0], workspace); enabled {
		t.Fatal("a refused connect turned the server back on")
	}
}
