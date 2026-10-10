package boot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

const readOnlyChildTask = "find a grep tool"

// readOnlyScriptProvider scripts the parent (dispatch the explore profile) and
// the read-only child (one use_capability call per round, indexed by how many
// tool results its own transcript already holds).
type readOnlyScriptProvider struct {
	mu      sync.Mutex
	child   []string
	results map[string]bool
}

func (p *readOnlyScriptProvider) Name() string { return "boot-readonly-script" }

func (p *readOnlyScriptProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	isChild, tools := false, 0
	p.mu.Lock()
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, readOnlyChildTask) && !strings.Contains(m.Content, "check what you can delegate to") {
			isChild = true
		}
		if m.Role == provider.RoleTool {
			tools++
			p.results[m.Content] = true
		}
	}
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	switch {
	case isChild && tools < len(p.child):
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: fmt.Sprintf("ro-%d", tools), Name: "use_capability", Arguments: p.child[tools]}}
	case !isChild && tools == 0:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "dispatch", Name: "use_capability",
			Arguments: `{"action":"call","capability_id":"task:subagent","arguments":{"description":"probe","profile":"ro-probe","prompt":"` + readOnlyChildTask + `"}}`,
		}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// TestEffectReadOnlyAgentSearchesCapabilities drives a real read-only child
// (a read-only subagent skill) through boot.Build. The tool description tells every
// model to search before declaring a tool unavailable, so the boundary must
// admit search, while decline, writers and undeclared actions stay refused.
func TestEffectReadOnlyAgentSearchesCapabilities(t *testing.T) {
	p := &readOnlyScriptProvider{results: map[string]bool{}, child: []string{
		`{"action":"search","query":"grep files by content"}`,
		`{"action":"decline","capability_id":"tool:grep","reason":"not needed"}`,
		`{"action":"call","capability_id":"tool:write_file","arguments":{"path":"x","content":"y"}}`,
		`{"action":"frobnicate"}`,
	}}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	provider.Register("boot-readonly-script", func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-readonly-script"
model = "x"
`)
	approveWorkspace(t, dir)
	writeFile(t, dir, ".reasonix/skills/ro-probe.md",
		"---\ndescription: read-only worker\nrunAs: subagent\nread-only: true\n---\nlook around\n")
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "check what you can delegate to"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var all []string
	for r := range p.results {
		all = append(all, r)
	}
	joined := strings.Join(all, "\n---\n")
	if strings.Contains(joined, "execute dynamic capability action") || strings.Contains(joined, "unknown dynamic capability action") {
		t.Fatalf("a read-only agent was blocked from an action the tool declares:\n%s", joined)
	}
	for _, want := range []string{
		"tool:grep",
		"read-only agent cannot decline a capability decision",
		"read-only agent cannot execute a state-changing dynamic capability",
		"unknown action",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in read-only child results:\n%s", want, joined)
		}
	}
}
