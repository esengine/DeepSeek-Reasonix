package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/session/control"
)

// pluginCtl is the controller half these routes touch: where a source path is
// resolved from, and the live-session disconnect an uninstall calls.
type pluginCtl struct {
	control.SessionAPI
	root         string
	disconnected []string
}

func (c *pluginCtl) SessionDir() string    { return "" }
func (c *pluginCtl) WorkspaceRoot() string { return c.root }

// Idle: every write here asks for a reload afterwards, and the reload refuses
// outright while a turn is running.
func (c *pluginCtl) RuntimeStatus() control.RuntimeStatus { return control.RuntimeStatus{} }

func (c *pluginCtl) DisconnectMCPServer(name string) bool {
	c.disconnected = append(c.disconnected, name)
	return true
}

// pluginHome isolates the state file and install root, and returns a server
// bound to a controller whose workspace is a throwaway directory.
func pluginHome(t *testing.T) (string, *pluginCtl, string) {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	ctl := &pluginCtl{root: testenv.TempDir(t)}
	srv := httptest.NewServer(operatorHandler(New(ctl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return home, ctl, srv.URL
}

// writePluginSource lays out a package that contributes one of everything the
// list has to separate: a skill, a hook, and an MCP server.
func writePluginSource(t *testing.T, root string) {
	t.Helper()
	writePluginFile(t, filepath.Join(root, "reasonix-plugin.json"), `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "demo",
  "version": "0.2.0",
  "description": "a demo package",
  "skills": ["skills"],
  "hooks": {"SessionStart": [{"command": "hooks/start.sh"}]},
  "mcpServers": {"docs": {"command": "docs-server", "args": ["--stdio"]}}
}`)
	writePluginFile(t, filepath.Join(root, "skills", "greet", "SKILL.md"),
		"---\nname: greet\ndescription: say hello\n---\nHello")
	writePluginFile(t, filepath.Join(root, "hooks", "start.sh"), "#!/bin/sh\n")
}

func writePluginFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func getPlugins(t *testing.T, base string) []pluginView {
	t.Helper()
	resp, err := http.Get(base + "/plugins")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /plugins = %d", resp.StatusCode)
	}
	var out []pluginView
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func decodeInstallSource(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestPluginPlanWritesNothing(t *testing.T) {
	home, _, base := pluginHome(t)
	src := testenv.TempDir(t)
	writePluginSource(t, src)

	resp := postJSON(t, base+"/plugins/plan", map[string]any{"source": src})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /plugins/plan = %d: %v", resp.StatusCode, decodeInstallSource(t, resp))
	}
	body := decodeInstallSource(t, resp)
	if body["status"] != "planned" {
		t.Fatalf("status = %v, want planned", body["status"])
	}
	if body["planId"] == "" || body["planId"] == nil {
		t.Fatal("a plan must carry the id an apply echoes back")
	}
	actions, _ := body["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions = %v, want one plugin action", body["actions"])
	}
	// Risk is what the confirmation page groups by, so it has to survive the
	// hop through this endpoint rather than being summarized away.
	act, _ := actions[0].(map[string]any)
	if act["riskLevel"] == nil || act["riskLevel"] == "" {
		t.Fatalf("action = %v, want a risk level", act)
	}
	if _, err := os.Stat(pluginpkg.InstallRoot(home, "demo")); !os.IsNotExist(err) {
		t.Fatalf("plan wrote an install root: %v", err)
	}
	if got := getPlugins(t, base); len(got) != 0 {
		t.Fatalf("plugins after plan = %v, want none", got)
	}
}

func TestPluginInstallListsItsContributions(t *testing.T) {
	_, _, base := pluginHome(t)
	src := testenv.TempDir(t)
	writePluginSource(t, src)

	resp := postJSON(t, base+"/plugins/install", map[string]any{"source": src})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /plugins/install = %d: %v", resp.StatusCode, decodeInstallSource(t, resp))
	}
	body := decodeInstallSource(t, resp)
	if body["status"] != "done" {
		t.Fatalf("status = %v, want done: %v", body["status"], body)
	}

	list := getPlugins(t, base)
	if len(list) != 1 {
		t.Fatalf("plugins = %v, want one", list)
	}
	p := list[0]
	if p.Name != "demo" || p.Version != "0.2.0" || !p.Enabled {
		t.Fatalf("installed plugin = %+v", p)
	}
	if len(p.Skills) != 1 || p.Skills[0].Invocation != "/demo:greet" {
		t.Fatalf("skills = %+v, want the qualified invocation", p.Skills)
	}
	// Hooks and servers are the two the page has to be able to single out.
	if len(p.Hooks) != 1 || p.Hooks[0].Event != "SessionStart" {
		t.Fatalf("hooks = %+v", p.Hooks)
	}
	if len(p.MCPServers) != 1 || p.MCPServers[0].Name != "docs" {
		t.Fatalf("mcpServers = %+v", p.MCPServers)
	}
}

// Updating is installing again with the recorded source and replace set, which
// is why there is no third endpoint. What it must not do is leave two rows.
func TestPluginUpdateReplacesInPlace(t *testing.T) {
	_, _, base := pluginHome(t)
	src := testenv.TempDir(t)
	writePluginSource(t, src)
	postJSON(t, base+"/plugins/install", map[string]any{"source": src})

	writePluginFile(t, filepath.Join(src, "reasonix-plugin.json"), `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "demo",
  "version": "0.3.0",
  "skills": ["skills"],
  "hooks": {"SessionStart": [{"command": "hooks/start.sh"}]},
  "mcpServers": {"docs": {"command": "docs-server", "args": ["--stdio"]}}
}`)
	resp := postJSON(t, base+"/plugins/install", map[string]any{
		"source": src, "name": "demo", "replace": true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update = %d: %v", resp.StatusCode, decodeInstallSource(t, resp))
	}
	if body := decodeInstallSource(t, resp); body["status"] != "done" {
		t.Fatalf("status = %v, want done: %v", body["status"], body)
	}
	list := getPlugins(t, base)
	if len(list) != 1 {
		t.Fatalf("plugins after update = %+v, want one row", list)
	}
	if list[0].Version != "0.3.0" {
		t.Fatalf("version = %q, want the new one", list[0].Version)
	}
}

func TestPluginEnabledPersists(t *testing.T) {
	home, _, base := pluginHome(t)
	src := testenv.TempDir(t)
	writePluginSource(t, src)
	postJSON(t, base+"/plugins/install", map[string]any{"source": src})

	resp := postJSON(t, base+"/plugins/enabled", map[string]any{"name": "demo", "enabled": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /plugins/enabled = %d: %v", resp.StatusCode, decodeInstallSource(t, resp))
	}
	st, err := pluginpkg.LoadState(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Plugins) != 1 || st.Plugins[0].Enabled {
		t.Fatalf("state = %+v, want demo disabled on disk", st.Plugins)
	}
	if list := getPlugins(t, base); len(list) != 1 || list[0].Enabled {
		t.Fatalf("plugins = %+v, want the switch reflected", list)
	}
}

func TestPluginEnabledRefusesUnknownName(t *testing.T) {
	_, _, base := pluginHome(t)
	resp := postJSON(t, base+"/plugins/enabled", map[string]any{"name": "nope", "enabled": true})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("POST /plugins/enabled for an unknown plugin = %d, want 422", resp.StatusCode)
	}
}

func TestPluginRemove(t *testing.T) {
	home, ctl, base := pluginHome(t)
	src := testenv.TempDir(t)
	writePluginSource(t, src)
	postJSON(t, base+"/plugins/install", map[string]any{"source": src})

	req, err := http.NewRequest(http.MethodDelete, base+"/plugins/demo", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /plugins/demo = %d", resp.StatusCode)
	}
	if body := decodeInstallSource(t, resp); body["status"] != "done" {
		t.Fatalf("status = %v, want done: %v", body["status"], body)
	}
	if list := getPlugins(t, base); len(list) != 0 {
		t.Fatalf("plugins after remove = %+v", list)
	}
	if _, err := os.Stat(pluginpkg.InstallRoot(home, "demo")); !os.IsNotExist(err) {
		t.Fatalf("install root survived the remove: %v", err)
	}
	// The package's server has to leave the live session too; a reload that is
	// refused mid-turn would otherwise keep answering from a removed package.
	if len(ctl.disconnected) != 1 || ctl.disconnected[0] != "docs" {
		t.Fatalf("disconnected = %v, want the package's MCP server", ctl.disconnected)
	}
}

func TestPluginExportStripsCredentials(t *testing.T) {
	_, _, base := pluginHome(t)
	src := testenv.TempDir(t)
	writePluginSource(t, src)
	writePluginFile(t, filepath.Join(src, ".mcp.json"),
		`{"mcpServers":{"docs":{"command":"docs","env":{"TOKEN":"sk-live-1"}}}}`)
	postJSON(t, base+"/plugins/install", map[string]any{"source": src})

	resp, err := http.Get(base + "/plugins/demo/export")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /plugins/demo/export = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if got := resp.Header.Get("X-Reasonix-Required-Env"); got != "DOCS_TOKEN" {
		t.Fatalf("X-Reasonix-Required-Env = %q, want the variable the other machine must supply", got)
	}
	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body.Bytes(), []byte("sk-live-1")) {
		t.Fatal("the exported archive carries the literal credential")
	}
}

func TestPluginExportUnknownPlugin(t *testing.T) {
	_, _, base := pluginHome(t)
	resp, err := http.Get(base + "/plugins/nope/export")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET export for an uninstalled plugin = %d, want 404", resp.StatusCode)
	}
}

// A name arrives as a URL segment, so the route validates it before anything
// resolves a path from it.
func TestPluginRemoveRejectsInvalidName(t *testing.T) {
	_, _, base := pluginHome(t)
	req, err := http.NewRequest(http.MethodDelete, base+"/plugins/..%2Fetc", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("DELETE with a path-shaped name = %d, want 400", resp.StatusCode)
	}
}
