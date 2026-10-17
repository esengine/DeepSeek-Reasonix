package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
)

type compositionBoundaryProvider struct{ requests []provider.Request }

func (p *compositionBoundaryProvider) Name() string { return "composition-boundary" }
func (p *compositionBoundaryProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	calls := []provider.ToolCall{
		{ID: "shell", Name: "bash", Arguments: `{"command":"touch forbidden"}`},
		{ID: "proxy", Name: "use_capability", Arguments: `{"action":"call","id":"tool:bash","arguments":{"command":"touch forbidden"}}`},
		{ID: "child", Name: "task", Arguments: `{"prompt":"touch forbidden"}`},
	}
	ch := make(chan provider.Chunk, 2)
	if len(p.requests) <= len(calls) {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &calls[len(p.requests)-1]}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "review complete"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func compositionTestWorkspace(t *testing.T, kind string) (string, string) {
	t.Helper()
	home := statedBootHome(t)
	root := robustTempDir(t)
	writeFile(t, root, "reasonix.toml", `default_model = "test-model"
[[providers]]
name = "test-model"
kind = "`+kind+`"
base_url = "https://example.invalid"
model = "x"
`)
	approveWorkspaceAt(t, home, root)
	return home, root
}

func TestCompositionDeniesProxyLateRegistrationAndDelegationAtProviderBoundary(t *testing.T) {
	p := &compositionBoundaryProvider{}
	provider.Register("composition-boundary", func(provider.Config) (provider.Provider, error) { return p, nil })
	home, root := compositionTestWorkspace(t, "composition-boundary")
	saveReadOnlyComposition(t, home, root)
	res, err := BuildRuntime(context.Background(), Options{Home: home, WorkspaceRoot: root, Model: "test-model", AgentComposition: "user:review", resolvedShell: pinnedEffectShell()})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Controller.Close()
	res.Controller.EnsureSessionPath()
	for _, candidate := range tool.Builtins() {
		if candidate.Name() == "bash" {
			res.Assembly.Registry.Add(candidate)
		}
	}
	if _, ok := res.Assembly.Registry.Get("bash"); ok {
		t.Fatal("late registration widened composition")
	}
	if err := res.Controller.RunTurn(context.Background(), "Review this workspace"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "forbidden")); !os.IsNotExist(err) {
		t.Fatal("forbidden tool executed")
	}
	if len(p.requests) < 2 {
		t.Fatal("test did not reach rejected-call continuation")
	}
	var schema []byte
	for _, req := range p.requests {
		for _, s := range req.Tools {
			if s.Name == "bash" || s.Name == "task" || s.Name == "use_capability" || s.Name == "write_file" {
				t.Fatalf("forbidden schema %s", s.Name)
			}
		}
		encoded, _ := json.Marshal(req.Tools)
		if schema != nil && string(schema) != string(encoded) {
			t.Fatal("same binding changed provider tool bytes")
		}
		schema = encoded
		for _, m := range req.Messages {
			if m.AgentComposition != nil {
				t.Fatal("local binding leaked into provider request")
			}
		}
		if !strings.Contains(req.Messages[0].Content, "rigorous code reviewer") {
			t.Fatal("resolved persona missing at provider boundary")
		}
	}
}

func TestCompositionReviewDoesNotStartHostSuppliedMCP(t *testing.T) {
	home, root := compositionTestWorkspace(t, "openai")
	saveReadOnlyComposition(t, home, root)
	res, err := BuildRuntime(context.Background(), Options{Home: home, WorkspaceRoot: root, Model: "test-model", AgentComposition: "user:review", resolvedShell: pinnedEffectShell(), ExtraPlugins: []plugin.Spec{{Name: "excluded", Type: "stdio", Command: "/composition-must-not-start"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Controller.Close()
	if len(res.Controller.CompositionBinding().Resolved.MCP.Members) != 0 {
		t.Fatal("host MCP escaped selection")
	}
}

func TestCompositionGeneralKeepsLegacyProviderPrefix(t *testing.T) {
	home, root := compositionTestWorkspace(t, "openai")
	opts := Options{Home: home, WorkspaceRoot: root, Model: "test-model", resolvedShell: pinnedEffectShell()}
	legacy, err := BuildRuntime(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Controller.Close()
	opts.AgentComposition = "builtin:general"
	composed, err := BuildRuntime(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer composed.Controller.Close()
	if legacy.Snapshot.CacheHash() != composed.Snapshot.CacheHash() {
		t.Fatal("default composition changed the cached provider prefix")
	}
}
