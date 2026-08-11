package boot

// Effect tests assert final-boundary behavior through the real Build stack:
// a scripted provider records what actually reaches the provider boundary.
// Component correctness is not system effectiveness (see REASONIX.md).

import (
	"context"
	"fmt"
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

// budgetRunawayProvider never repeats itself and never fails, so every
// adaptive guard stays quiet. Only the spend gate can stop it.
type budgetRunawayProvider struct {
	mu     sync.Mutex
	rounds int
}

func (p *budgetRunawayProvider) Name() string { return "boot-budget-runaway" }

func (p *budgetRunawayProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 4)
	ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
		ID:        fmt.Sprintf("call-%d", round),
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"file%d.txt"}`, round),
	}}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{
		PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100, RequestCount: 1,
	}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *budgetRunawayProvider) roundCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rounds
}

// TestEffectTaskBudgetLandsARunawayThroughRealBuild pins the gate at its final
// boundary: a configured spend budget must stop a wandering turn through the
// real Build assembly. Nothing else would stop it: ordinary chat has no round
// ceiling, and this provider never repeats itself.
func TestEffectTaskBudgetLandsARunawayThroughRealBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &budgetRunawayProvider{}
	provider.Register("boot-budget-gate", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
task_time_budget_minutes = 0.0005

[[providers]]
name = "test-model"
kind = "boot-budget-gate"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	_ = ctrl.Run(context.Background(), "read every file you can find")

	// Ordinary chat has no round ceiling, so a gate that never reached the
	// executor would leave this provider looping until the test times out.
	if got := rec.roundCount(); got > 50 {
		t.Fatalf("provider saw %d rounds; the configured spend budget never reached the executor", got)
	}
	if rec.roundCount() == 0 {
		t.Fatal("no round reached the provider; the run never started")
	}
}
