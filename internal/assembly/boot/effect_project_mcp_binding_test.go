package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

const argvLogEnv = "REASONIX_TEST_MCP_ARGV_LOG"

// TestArgvRecordingMCPHelper is the stdio MCP server the binding tests launch:
// it appends the words after "--" to the log its environment names, then
// answers the protocol like TestHelperProcess.
func TestArgvRecordingMCPHelper(t *testing.T) {
	logPath := os.Getenv(argvLogEnv)
	if logPath == "" || os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	words := os.Args
	for i, w := range os.Args {
		if w == "--" {
			words = os.Args[i+1:]
			break
		}
	}
	if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_, _ = f.WriteString(strings.Join(words, " ") + "\n")
		_ = f.Close()
	}
	TestHelperProcess(t)
}

const bindingTestProvider = `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`

// declareRecordingServer writes a project declaration of server "docs-helper"
// through the given project file, launching the argv-recording helper with
// phase as its only argument.
func declareRecordingServer(t *testing.T, dir, file, logPath, phase string) {
	t.Helper()
	args := []string{"-test.run=^TestArgvRecordingMCPHelper$", "--", phase}
	env := map[string]string{"GO_WANT_HELPER_PROCESS": "1", argvLogEnv: logPath}
	switch file {
	case ".mcp.json":
		body, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
			"docs-helper": map[string]any{"command": os.Args[0], "args": args, "env": env},
		}})
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, "reasonix.toml", bindingTestProvider)
		writeFile(t, dir, ".mcp.json", string(body))
	case "reasonix.toml":
		quoted := make([]string, len(args))
		for i, a := range args {
			quoted[i] = `"` + a + `"`
		}
		writeFile(t, dir, "reasonix.toml", bindingTestProvider+`
[[plugins]]
name = "docs-helper"
command = "`+filepath.ToSlash(os.Args[0])+`"
args = [`+strings.Join(quoted, ", ")+`]
env = { GO_WANT_HELPER_PROCESS = "1", `+argvLogEnv+` = "`+filepath.ToSlash(logPath)+`" }
`)
	default:
		t.Fatalf("unknown project file %q", file)
	}
	approveWorkspace(t, dir)
}

func launchedPhases(logPath string) []string {
	body, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	return strings.Fields(string(body))
}

func waitForPhase(logPath, phase string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if slices.Contains(launchedPhases(logPath), phase) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func buildForBinding(t *testing.T) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	ctrl, err := Build(ctx, Options{Sink: event.Discard})
	if err != nil {
		cancel()
		t.Fatalf("Build: %v", err)
	}
	return func() { ctrl.Close(); cancel() }
}

// The user's enable decision covers the declaration they enabled. A project
// that keeps the server's name and rewrites what it launches must not inherit
// that decision: the rewritten command stays off until the user answers again
// through the same switch, and then it runs.
func TestEffectEnabledProjectMCPDoesNotLaunchARewrittenDeclaration(t *testing.T) {
	for _, file := range []string{".mcp.json", "reasonix.toml"} {
		t.Run(file, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			logPath := filepath.Join(robustTempDir(t), "argv.log")
			declareRecordingServer(t, dir, file, logPath, "phase1")

			closeFirst := buildForBinding(t)
			time.Sleep(500 * time.Millisecond)
			if got := launchedPhases(logPath); len(got) != 0 {
				closeFirst()
				t.Fatalf("project server launched before any decision: %v", got)
			}
			closeFirst()

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			ctrl, err := Build(ctx, Options{Sink: event.Discard})
			if err != nil {
				cancel()
				t.Fatalf("Build: %v", err)
			}
			if err := ctrl.SetMCPServerEnabled("docs-helper", config.ActivationProject, true); err != nil {
				t.Fatalf("enable through the settings switch: %v", err)
			}
			ctrl.Close()
			cancel()

			closeApproved := buildForBinding(t)
			if !waitForPhase(logPath, "phase1", 5*time.Second) {
				closeApproved()
				t.Fatalf("enabled project server never launched; log=%v", launchedPhases(logPath))
			}
			closeApproved()

			declareRecordingServer(t, dir, file, logPath, "phase2")
			closeRewritten := buildForBinding(t)
			ran := waitForPhase(logPath, "phase2", 3*time.Second)
			closeRewritten()
			if ran {
				t.Fatalf("rewritten declaration of an enabled project server launched with no new decision; log=%v", launchedPhases(logPath))
			}
			cfg, err := config.LoadForRootReadOnly(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range cfg.Plugins {
				if p.Name == "docs-helper" && !config.DefaultActivationStore().AwaitingDecision(p, dir) {
					t.Fatal("rewritten project server is not reported as awaiting the user's decision")
				}
			}

			ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			ctrl, err = Build(ctx, Options{Sink: event.Discard})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()
			for _, h := range ctrl.MCPServerHealth() {
				if h.Name == "docs-helper" && h.Status != control.MCPHealthPending {
					t.Fatalf("rewritten server health = %s, want pending", h.Status)
				}
			}
			if err := ctrl.SetMCPServerEnabled("docs-helper", config.ActivationProject, true); err != nil {
				t.Fatalf("re-enable through the settings switch: %v", err)
			}
			if _, err := ctrl.ConnectConfiguredMCPServer("docs-helper"); err != nil {
				t.Fatalf("connect after re-approval: %v", err)
			}
			if !waitForPhase(logPath, "phase2", 5*time.Second) {
				t.Fatalf("re-approved declaration never launched; log=%v", launchedPhases(logPath))
			}
		})
	}
}

// A deferred server starts on its first call, which can come long after the
// session read the user's decision. A workspace file the declaration names
// that changed in between (a pull while the session is open) must not run:
// the call is refused before any process starts, and the server reads as
// waiting for the user.
func TestEffectEnabledProjectMCPRefusesAWorkspaceFileChangedMidSession(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the launcher script is POSIX shell")
	}
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	logPath := filepath.Join(testenv.TempDir(t), "argv.log")
	launcher := func(phase string) {
		writeFile(t, dir, "run.sh", "#!/bin/sh\nexec \""+os.Args[0]+"\" -test.run='^TestArgvRecordingMCPHelper$' -- "+phase+"\n")
	}
	launcher("approved")
	body, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"docs-helper": map[string]any{
		"command": "sh", "args": []string{"./run.sh"},
		"env": map[string]string{"GO_WANT_HELPER_PROCESS": "1", argvLogEnv: logPath},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, ".mcp.json", string(body))
	rec := &scriptedCallProvider{}
	provider.Register("boot-mcp-binding-midsession", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
[[providers]]
name = "test-model"
kind = "boot-mcp-binding-midsession"
model = "x"
`)
	approveWorkspaceAt(t, home, dir)

	first, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetMCPServerEnabled("docs-helper", config.ActivationProject, true); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ConnectConfiguredMCPServer("docs-helper"); err != nil {
		t.Fatalf("connect the enabled server: %v", err)
	}
	if !waitForPhase(logPath, "approved", 5*time.Second) {
		t.Fatalf("enabled server never launched; log=%v", launchedPhases(logPath))
	}
	first.Close()
	// The next session discovers and caches the schema, so the one after it
	// registers the server from cache and starts nothing until a call.
	discovery, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	waitForCond(t, "schema discovery", 10*time.Second, func() bool { return discovery.MCPCatalogTools()["docs-helper"] > 0 })
	discovery.Close()
	launches := len(launchedPhases(logPath))

	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	time.Sleep(300 * time.Millisecond)
	if got := len(launchedPhases(logPath)); got != launches {
		t.Fatalf("a cached deferred server started at boot (%d launches, want %d); the test cannot reach a lazy start", got, launches)
	}
	launcher("swapped")
	rec.mu.Lock()
	rec.calls = []scriptedCall{{"use_capability", `{"action":"call","capability_id":"mcp-tool:docs-helper/echo","arguments":{"msg":"hi"}}`}}
	rec.mu.Unlock()
	if err := ctrl.Run(t.Context(), "use the docs helper"); err != nil {
		t.Fatal(err)
	}
	if waitForPhase(logPath, "swapped", time.Second) {
		t.Fatalf("a workspace launcher rewritten after the session started ran on the old decision; log=%v", launchedPhases(logPath))
	}
	if got := rec.resultOf(0); strings.Contains(got, "echo: hi") {
		t.Fatalf("the call reached a server: %q; log=%v", got, launchedPhases(logPath))
	}
	for _, h := range ctrl.MCPServerHealth() {
		if h.Name == "docs-helper" && h.Status != control.MCPHealthPending {
			t.Fatalf("docs-helper status = %s (%s), want pending", h.Status, h.Error)
		}
	}
}

// The launcher looks a bare command up in the declaration's own PATH. When
// that PATH points into the workspace, the file it finds is repository code
// the user approved, and swapping it must not start on that approval.
func TestEffectEnabledProjectMCPDoesNotLaunchASwappedDeclaredPathExecutable(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the launcher script is POSIX shell")
	}
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	logPath := filepath.Join(testenv.TempDir(t), "argv.log")
	launcher := func(phase string) {
		writeFile(t, dir, "tools/rxhelper", "#!/bin/sh\nexec \""+os.Args[0]+"\" -test.run='^TestArgvRecordingMCPHelper$' -- "+phase+"\n")
		if err := os.Chmod(filepath.Join(dir, "tools", "rxhelper"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	launcher("approved")
	body, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"docs-helper": map[string]any{
		"command": "rxhelper",
		"env": map[string]string{"PATH": "${CLAUDE_PROJECT_DIR}/tools:/usr/bin:/bin",
			"GO_WANT_HELPER_PROCESS": "1", argvLogEnv: logPath},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, ".mcp.json", string(body))
	rec := &scriptedCallProvider{}
	provider.Register("boot-mcp-binding-declared-path", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
[[providers]]
name = "test-model"
kind = "boot-mcp-binding-declared-path"
model = "x"
`)
	approveWorkspaceAt(t, home, dir)

	first, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetMCPServerEnabled("docs-helper", config.ActivationProject, true); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ConnectConfiguredMCPServer("docs-helper"); err != nil {
		t.Fatalf("connect the enabled server: %v", err)
	}
	if !waitForPhase(logPath, "approved", 5*time.Second) {
		t.Fatalf("enabled server never launched; log=%v", launchedPhases(logPath))
	}
	first.Close()

	launcher("swapped")
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	rec.mu.Lock()
	rec.calls = []scriptedCall{{"use_capability", `{"action":"call","capability_id":"mcp-tool:docs-helper/echo","arguments":{"msg":"hi"}}`}}
	rec.mu.Unlock()
	if err := ctrl.Run(t.Context(), "use the docs helper"); err != nil {
		t.Fatal(err)
	}
	if waitForPhase(logPath, "swapped", time.Second) {
		t.Fatalf("a swapped executable the declared PATH reaches ran on the earlier approval; log=%v", launchedPhases(logPath))
	}
}
