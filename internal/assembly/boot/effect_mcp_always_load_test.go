package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/plugin"
)

func requestToolNames(req provider.Request) []string {
	names := make([]string, 0, len(req.Tools))
	for _, t := range req.Tools {
		names = append(names, t.Name)
	}
	return names
}

func alwaysLoadConfig(url string, alwaysLoad bool) string {
	load := ""
	if alwaysLoad {
		load = "load = \"always\"\n"
	}
	return fmt.Sprintf(`
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-mcp-always-load"
model = "x"

[[plugins]]
name = "ops"
type = "http"
url = %q
%s`, url, load)
}

// An always-loaded server's tools reach the provider from the first request of
// the first session that knows its schema, stay byte-identical across turns,
// and are callable by their own name, and the server is connected before the
// first call. A deferred server's tools never reach it, and it stays idle.
func TestEffectAlwaysLoadedMCPToolsReachTheProviderSchema(t *testing.T) {
	var rec *capabilityCallProvider
	provider.Register("boot-mcp-always-load", func(provider.Config) (provider.Provider, error) { return rec, nil })
	for _, alwaysLoad := range []bool{true, false} {
		isolateConfigHome(t)
		dir := robustTempDir(t)
		t.Chdir(dir)
		server := deployMCPServer(t, "")
		writeFile(t, dir, "reasonix.toml", alwaysLoadConfig(server.URL, alwaysLoad))
		approveWorkspace(t, dir)
		approveProjectServer(t, dir, "ops")

		// The first session has no cached schema: nothing to show yet, so the
		// server stays deferred while boot discovers and caches its tools.
		rec = &capabilityCallProvider{}
		ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		waitForMCPServer(t, ctrl.Host(), "ops")
		if err := ctrl.Run(context.Background(), "hello"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if names := requestToolNames(rec.last()); slices.Contains(names, "mcp__ops__deploy") {
			t.Fatalf("alwaysLoad=%v: a server with no cached schema reached the first session's schema: %v", alwaysLoad, names)
		}
		ctrl.Close()

		rec = &capabilityCallProvider{direct: "mcp__ops__deploy"}
		if !alwaysLoad {
			rec.direct = ""
		}
		ctrl, err = Build(context.Background(), Options{Sink: event.Discard})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		// An always-loaded server is connected before anything calls it; a
		// deferred one stays idle until its first call.
		if alwaysLoad {
			waitForMCPServer(t, ctrl.Host(), "ops")
		} else {
			time.Sleep(300 * time.Millisecond)
			for _, srv := range ctrl.Host().Servers() {
				if srv.Name == "ops" {
					t.Fatal("a deferred server connected before anything called it")
				}
			}
		}
		if err := ctrl.Run(context.Background(), "deploy"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if err := ctrl.Run(context.Background(), "again"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		reqs := rec.requests()
		ctrl.Close()
		server.Close()

		names := requestToolNames(reqs[0])
		if got := slices.Contains(names, "mcp__ops__deploy"); got != alwaysLoad {
			t.Fatalf("alwaysLoad=%v: mcp__ops__deploy in schema = %v; tools: %v", alwaysLoad, got, names)
		}
		first, _ := json.Marshal(reqs[0].Tools)
		for i, req := range reqs[1:] {
			if again, _ := json.Marshal(req.Tools); string(again) != string(first) {
				t.Fatalf("alwaysLoad=%v: request %d changed the tool schema", alwaysLoad, i+1)
			}
		}
		if alwaysLoad {
			results := effectToolResults(reqs[1])
			if len(results) != 1 || !strings.Contains(results[0], "deployed") {
				t.Fatalf("direct call to the always-loaded tool: results = %q", results)
			}
		}
	}
}

func (p *capabilityCallProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.reqs)
}

func waitForMCPServer(t *testing.T, host *plugin.Host, name string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, srv := range host.Servers() {
			if srv.Name == name {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("MCP server %q never connected", name)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
