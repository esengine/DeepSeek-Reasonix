package boot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/config"
	"reasonix/internal/event"
)

func countingMCPStub(t *testing.T, name string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	inner := mcpHostSessionStub(t, name, nil)
	handler := inner.Config.Handler
	inner.Close()
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

// enableProjectMCPForTest records the user's enable decision for each server
// root's project files declare with auto_start on.
func enableProjectMCPForTest(t testing.TB, root string) {
	t.Helper()
	cfg, err := config.LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Plugins {
		if p.Source.ProjectScoped() && p.ShouldAutoStart() {
			if err := config.DefaultMCPActivationStore().SetServerEnabled(p, root, true); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func waitForHit(hits *atomic.Int32, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if hits.Load() > 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return hits.Load() > 0
}

// A project's configuration cannot choose MCP servers the host starts: a
// repository-declared server stays idle until the user enables it, while a
// user-level server keeps starting as before.
func TestEffectProjectDeclaredMCPDoesNotStartThroughRealBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	userSrv, userHits := countingMCPStub(t, "user")
	tomlSrv, tomlHits := countingMCPStub(t, "repo-toml")
	jsonSrv, jsonHits := countingMCPStub(t, "repo-json")

	userConfig := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, []byte("[[plugins]]\nname = \"user-http\"\ntype = \"http\"\nurl = \""+userSrv.URL+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "stdio-started")
	stdioPlugin := ""
	if runtime.GOOS != "windows" {
		stdioPlugin = "\n[[plugins]]\nname = \"repo-stdio\"\ncommand = \"sh\"\nargs = [\"-c\", \"echo started > " + marker + "\"]\n"
	}
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"

[[plugins]]
name = "repo-toml-http"
type = "http"
url = "`+tomlSrv.URL+`"
auto_start = true
`+stdioPlugin)
	approveWorkspace(t, dir)
	writeFile(t, dir, ".mcp.json", `{"mcpServers":{"repo-json-http":{"type":"http","url":"`+jsonSrv.URL+`"}}}`)

	build := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		t.Cleanup(cancel)
		ctrl, err := Build(ctx, Options{})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		t.Cleanup(ctrl.Close)
	}

	build()
	if !waitForHit(userHits, 5*time.Second) {
		t.Fatal("user-level MCP server was never contacted; the probe cannot tell idle from broken")
	}
	time.Sleep(500 * time.Millisecond)
	if n := tomlHits.Load(); n != 0 {
		t.Fatalf("project reasonix.toml MCP server received %d requests with no user decision", n)
	}
	if n := jsonHits.Load(); n != 0 {
		t.Fatalf("project .mcp.json MCP server received %d requests with no user decision", n)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("project stdio MCP command ran with no user decision")
	}

	cfg, err := config.LoadForRootReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Plugins {
		if p.Name == "repo-toml-http" {
			if err := config.DefaultMCPActivationStore().SetServerEnabled(p, dir, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	build()
	if !waitForHit(tomlHits, 5*time.Second) {
		t.Fatal("project MCP server stayed idle after the user enabled it")
	}
	if n := jsonHits.Load(); n != 0 {
		t.Fatalf("enabling one project server started another: .mcp.json server got %d requests", n)
	}
}

// An enable decision holds for the declaration the user approved; rewriting
// the command afterwards leaves the server off until they approve again.
func TestEffectRewrittenProjectMCPCommandDoesNotRunThroughRealBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("marker command is POSIX shell")
	}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	base := `
default_model = "test-model"
[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`
	approved := filepath.Join(dir, "approved-ran")
	rewritten := filepath.Join(dir, "rewritten-ran")
	writeFile(t, dir, "reasonix.toml", base+"\n[[plugins]]\nname = \"repo-stdio\"\ncommand = \"sh\"\nargs = [\"-c\", \"echo ok > "+approved+"\"]\n")
	approveWorkspace(t, dir)
	enableProjectMCPForTest(t, dir)
	writeFile(t, dir, "reasonix.toml", base+"\n[[plugins]]\nname = \"repo-stdio\"\ncommand = \"sh\"\nargs = [\"-c\", \"echo changed > "+rewritten+"\"]\n")
	approveWorkspace(t, dir)

	var notices []event.Event
	var mu sync.Mutex
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	ctrl, err := Build(ctx, Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			mu.Lock()
			notices = append(notices, e)
			mu.Unlock()
		}
	})})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(rewritten); err == nil {
			t.Fatal("rewritten command of an enabled project server ran with no new decision")
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, n := range notices {
		if strings.Contains(n.Detail, "repo-stdio [changed_since_enabled]") && strings.Contains(n.Detail, "echo changed > "+rewritten) {
			return
		}
	}
	t.Fatalf("no notice named the changed server and its new command; notices=%+v", notices)
}

// The model learns why a project server is off, as a typed cause it can report,
// rather than a generic disabled state it might try to route around.
func TestEffectModelSeesProjectMCPAwaitingUserDecision(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	srv, hits := countingMCPStub(t, "repo")
	writeFile(t, dir, "reasonix.toml", mcpCapabilityTestProviderConfig+`
[[plugins]]
name = "repo-http"
type = "http"
url = "`+srv.URL+`"
`)
	approveWorkspace(t, dir)
	out := runUseCapabilityCalls(t, Options{Sink: event.Discard}, "mcp-server:repo-http", "mcp-tool:repo-http/ping")
	if !strings.Contains(out, "[awaiting_user_decision]") {
		t.Fatalf("model-visible refusal lacks the typed cause:\n%s", out)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("pending project server received %d requests during the turn", n)
	}
}

// Connecting a pending project server by hand is the user's approval, so the
// next session starts it without asking again.
func TestConnectingPendingProjectMCPRecordsTheDecision(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	srv, hits := countingMCPStub(t, "repo")
	writeFile(t, dir, "reasonix.toml", mcpCapabilityTestProviderConfig+`
[[plugins]]
name = "repo-http"
type = "http"
url = "`+srv.URL+`"
`)
	approveWorkspace(t, dir)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("connect", testutil.Turn{Text: "done"}))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	ctrl, err := Build(ctx, Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	if _, err := ctrl.ConnectConfiguredMCPServer("repo-http"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if hits.Load() == 0 {
		t.Fatal("explicit connect never reached the server")
	}
	cfg, err := config.LoadForRootReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Plugins {
		if p.Name == "repo-http" {
			if d := config.MCPServerDecision(p, ctrl.WorkspaceRoot()); d != config.MCPDecisionOn {
				t.Fatalf("decision after explicit connect = %s, want enabled", d.Code())
			}
			return
		}
	}
	t.Fatal("repo-http not loaded")
}
