package boot

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// A runtime outlives the request that built it: Studio opens a pane with one
// HTTP request, whose context ends when the handler returns. An MCP server that
// starts on its first call must still start after that.
func TestEffectAnMCPServerStartsAfterTheBuildingRequestEnded(t *testing.T) {
	home := isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &capabilityCallProvider{call: `{"action":"call","capability_id":"mcp-tool:screen/shot","arguments":{}}`}
	provider.Register("boot-runtime-ctx", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-runtime-ctx"
model = "x"
`)
	server := screenshotMCPServer(t)
	defer server.Close()
	// The user's own config: a server nobody has to vouch for, started lazily.
	rxHome := filepath.Join(home, "rx")
	t.Setenv("REASONIX_HOME", rxHome)
	writeFile(t, rxHome, "config.toml", `
[[plugins]]
name = "screen"
type = "http"
url = "`+server.URL+`"
`)
	approveWorkspace(t, dir)
	// The first runtime finds no cached schema and starts the server while it
	// builds; the one after it, like a second pane, starts it on first call.
	first, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := first.Run(context.Background(), "look at the screen"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	first.Close()
	request, requestDone := context.WithCancel(context.Background())
	ctrl, err := Build(request, Options{Sink: event.Discard})
	requestDone()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "look at the screen"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	results := effectToolResults(rec.last())
	if len(results) != 1 || !strings.Contains(results[0], "captured") {
		t.Fatalf("tool results = %q, want the server's answer", results)
	}
}

// capabilityCallProvider makes one call per turn, then answers: a
// use_capability call with arguments call, or the tool named direct.
type capabilityCallProvider struct {
	mu     sync.Mutex
	call   string
	direct string
	reqs   []provider.Request
}

func (p *capabilityCallProvider) Name() string { return "boot-runtime-ctx" }

func (p *capabilityCallProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	first := len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == provider.RoleUser
	ch := make(chan provider.Chunk, 2)
	switch {
	case first && p.direct != "":
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: p.direct, Arguments: `{}`}}
	case first:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "cap-1", Name: "use_capability", Arguments: p.call}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *capabilityCallProvider) last() provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reqs[len(p.reqs)-1]
}
