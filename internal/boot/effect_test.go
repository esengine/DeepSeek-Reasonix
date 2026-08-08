package boot

// Effect tests assert final-boundary behavior through the real Build stack:
// a scripted provider records what actually reaches the provider boundary.
// Component correctness is not system effectiveness (see REASONIX.md).

import (
	"context"
	"sync"
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type effectRecordingProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *effectRecordingProvider) Name() string { return "boot-effect-test" }

func (p *effectRecordingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *effectRecordingProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

// effectRun builds the real stack around a recording provider, runs one
// prompt, and returns every request that reached the provider boundary.
func effectRun(t *testing.T, kind, tokenMode string, arm ablation.Set) []provider.Request {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &effectRecordingProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: tokenMode, Ablation: arm})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := rec.requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached the provider boundary")
	}
	return reqs
}

func toolNames(req provider.Request) map[string]bool {
	names := make(map[string]bool, len(req.Tools))
	for _, tool := range req.Tools {
		names[tool.Name] = true
	}
	return names
}

// TestEffectEconomyShrinksProviderToolSurface pins the economy tier's whole
// point at the boundary it must appear: the schema set the provider is
// actually sent, not the registry the host assembles.
func TestEffectEconomyShrinksProviderToolSurface(t *testing.T) {
	full := effectRun(t, "boot-effect-full", "", ablation.Set{})
	economy := effectRun(t, "boot-effect-economy", "economy", ablation.Set{})

	fullTools := len(full[0].Tools)
	economyTools := len(economy[0].Tools)
	if economyTools >= fullTools {
		t.Fatalf("economy sent %d tool schemas, full sent %d — the lean surface never reached the provider", economyTools, fullTools)
	}
	if economyTools > 15 {
		t.Fatalf("economy sent %d tool schemas; the core surface contract is a small fixed set", economyTools)
	}
	if !toolNames(economy[0])["connect_tool_source"] {
		t.Fatal("economy surface must still offer connect_tool_source to grow on demand")
	}
}

// TestEffectSubagentAblationRemovesChildToolSchemas asserts the ablation at
// the provider boundary: with subagents off the model cannot even see the
// spawning tools, so no trajectory can ever contain a child request.
func TestEffectSubagentAblationRemovesChildToolSchemas(t *testing.T) {
	control := effectRun(t, "boot-effect-sub-on", "", ablation.Set{})
	ablated := effectRun(t, "boot-effect-sub-off", "", ablation.New(ablation.Subagent))

	if names := toolNames(control[0]); !names["task"] {
		t.Fatal("control surface must offer the task tool; the ablation assertion below would be vacuous")
	}
	names := toolNames(ablated[0])
	for _, spawn := range []string{"task", "parallel_tasks", "fleet"} {
		if names[spawn] {
			t.Fatalf("subagent-ablated surface still offers %q — the model can spawn children the arm claims to disable", spawn)
		}
	}
}
