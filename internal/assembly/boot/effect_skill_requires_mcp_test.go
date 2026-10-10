package boot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

func TestEffectSkillMCPRequirementsFollowConnectionAndActivation(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	server := disabledToolsMCPServer(t)
	defer server.Close()
	const body = "Read the notes available from the configured notes server.\nReport the relevant entries and identify anything that could not be verified."
	const skillPath = ".agents/skills/notes-check/SKILL.md"
	const source = "---\nname: notes-check\ndescription: Check notes using the configured MCP reader\nrequires: mcp-server:notes, mcp-tool:notes/read\n---\n" + body + "\n"
	writeFile(t, workspace, skillPath, source)
	writeFile(t, workspace, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"
[agent]
system_prompt = "BASE"
[environment]
enabled = false
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-skill-mcp-requires"
model = "x"
[[plugins]]
name = "notes"
type = "http"
url = %q
auto_start = false
load = "deferred"
`, server.URL))
	approveWorkspace(t, workspace)
	approveProjectServer(t, workspace, "notes")
	var rec *scriptedCallProvider
	provider.Register("boot-skill-mcp-requires", func(provider.Config) (provider.Provider, error) { return rec, nil })
	var ctrl *control.Controller
	restart := func() {
		t.Helper()
		if ctrl != nil {
			ctrl.Close()
		}
		rec = &scriptedCallProvider{}
		var err error
		ctrl, err = Build(t.Context(), Options{Sink: event.Discard})
		if err != nil {
			t.Fatal(err)
		}
		ctrl.EnsureSessionPath()
	}
	t.Cleanup(func() {
		if ctrl != nil {
			ctrl.Close()
		}
	})
	connect := func() {
		t.Helper()
		entries := ctrl.ConfiguredMCPServers()
		if len(entries) != 1 || entries[0].Entry.Name != "notes" {
			t.Fatalf("configured MCP servers = %+v", entries)
		}
		if n, err := ctrl.ConnectMCPServer(entries[0].Entry); err != nil || n != 2 {
			t.Fatalf("connect notes: tools=%d, err=%v", n, err)
		}
	}
	calls := []scriptedCall{
		{"run_skill", `{"name":"notes-check","arguments":"check today's notes"}`},
		{"read_skill", `{"name":"notes-check"}`},
		{"use_capability", `{"action":"call","capability_id":"skill:notes-check","arguments":{"arguments":"check today's notes"}}`},
		{"slash_command", `{"command":"notes-check","arguments":"check today's notes"}`},
	}
	var prefix string
	phase := func(name string, ready, connected bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			if live := len(ctrl.Host().Servers()) > 0; live != connected {
				t.Fatalf("MCP connected=%t, expected=%t", live, connected)
			}
			rec.mu.Lock()
			first := rec.round
			for len(rec.calls) < first {
				rec.calls = append(rec.calls, scriptedCall{})
			}
			rec.calls = append(rec.calls, calls...)
			rec.mu.Unlock()
			if err := ctrl.Run(t.Context(), "Inspect the notes-check playbook"); err != nil {
				t.Fatal(err)
			}
			for i, call := range calls {
				result := rec.resultOf(first + i)
				if ready {
					if !strings.Contains(result, body) {
						t.Errorf("%s did not return the ready skill body: %s", call.name, result)
					}
				} else if strings.Contains(result, body) || !strings.Contains(result, "requires unavailable capabilities:") {
					t.Errorf("%s did not refuse the unavailable dependency: %s", call.name, result)
				}
			}
			rec.mu.Lock()
			requests := append([]provider.Request(nil), rec.reqs...)
			rec.mu.Unlock()
			for _, req := range requests {
				current := systemMessage(req.Messages)
				if strings.Contains(current, body) {
					t.Fatal("dependency-bound skill body entered the cached system prefix")
				}
				if prefix == "" {
					prefix = current
				} else if current != prefix {
					t.Fatal("MCP dependency state changed the cached system prefix")
				}
			}
		})
	}
	restart()
	waitForMCPServer(t, ctrl.Host(), "notes")
	waitForCond(t, "discovered notes tool catalog", 10*time.Second, func() bool { return ctrl.MCPCatalogTools()["notes"] == 2 })
	phase("connected-after-first-schema-discovery", true, true)
	writeFile(t, workspace, skillPath, strings.Replace(source, "mcp-tool:notes/read", "mcp-tool:notes/not-offered", 1))
	phase("connected-with-an-unavailable-tool", false, true)
	writeFile(t, workspace, skillPath, source)
	phase("corrected-tool-declaration-live", true, true)
	if err := ctrl.SetMCPServerEnabled("notes", config.ActivationProject, false); err != nil {
		t.Fatal(err)
	}
	phase("disabled-live", false, false)
	restart()
	phase("disabled-after-restart", false, false)
	if err := ctrl.SetMCPServerEnabled("notes", config.ActivationProject, true); err != nil {
		t.Fatal(err)
	}
	phase("enabled-with-cache-before-connection", false, false)
	connect()
	phase("enabled-and-connected", true, true)
	restart()
	if entries := ctrl.ConfiguredMCPServers(); len(entries) != 1 || len(entries[0].Tools) != 2 || entries[0].Stale {
		t.Fatalf("expected two valid cached tools after restart: %+v", entries)
	}
	phase("cached-without-connection", false, false)
	connect()
	phase("reconnected-after-restart", true, true)
	if err := ctrl.SetMCPServerEnabled("notes", config.ActivationProject, false); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	count := len(rec.reqs)
	rec.mu.Unlock()
	ctrl.SubmitHTTPOptions("/notes-check explain the missing setup", control.SubmitOptions{RefuseUnknownSlash: true})
	waitForCond(t, "explicit skill invocation", 10*time.Second, func() bool { return !ctrl.Running() })
	rec.mu.Lock()
	requests := append([]provider.Request(nil), rec.reqs...)
	rec.mu.Unlock()
	if len(requests) <= count {
		t.Fatal("explicit user invocation did not reach the provider")
	}
	var user string
	for _, msg := range requests[len(requests)-1].Messages {
		if msg.Role == provider.RoleUser {
			user = msg.Content
		}
	}
	if !strings.Contains(user, body) || !strings.Contains(user, "Arguments: explain the missing setup") {
		t.Fatalf("explicit invocation was not delivered: %s", user)
	}
	if len(ctrl.Host().Servers()) != 0 {
		t.Fatal("explicit skill invocation connected a disabled MCP server")
	}
	if got, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(skillPath))); err != nil || string(got) != source {
		t.Fatalf("connection changes altered the shared skill: %q, err=%v", got, err)
	}
}
