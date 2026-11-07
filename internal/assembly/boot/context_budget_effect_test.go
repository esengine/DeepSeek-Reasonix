package boot

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// budgetEffectProvider pads every reply so the transcript climbs through and
// past the compaction trigger. A "check the budget" prompt makes it call the
// context_budget tool and keep what the tool returned.
type budgetEffectProvider struct {
	mu     sync.Mutex
	reqs   []provider.Request
	bulk   string
	budget string
	probe  func() tool.ContextBudget
	before tool.ContextBudget
}

func (p *budgetEffectProvider) Name() string { return "boot-budget-effect" }

const budgetQueryPrompt = "check the budget"

func (p *budgetEffectProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	defer close(ch)
	asked, answered := "", ""
	for _, m := range req.Messages {
		switch {
		case m.Role == provider.RoleUser && strings.Contains(m.Content, budgetQueryPrompt):
			asked, answered = m.Content, ""
		case m.Role == provider.RoleTool && m.ToolCallID == "c1" && asked != "":
			answered = m.Content
		}
	}
	switch {
	case answered != "":
		p.mu.Lock()
		p.budget = answered
		p.mu.Unlock()
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "noted"}
	case asked != "":
		if p.probe != nil {
			p.mu.Lock()
			p.before = p.probe()
			p.mu.Unlock()
		}
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "c1", Name: "context_budget", Arguments: "{}"}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: p.bulk}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	return ch, nil
}

func budgetFixtureAgent(t *testing.T, kind, models, overrides string, sink event.Sink) (*budgetEffectProvider, func(prompt string), func() tool.ContextBudget) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &budgetEffectProvider{bulk: strings.Repeat("work output line with detail. ", 380)}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "conn/`+strings.Split(models, ",")[0]+`"

[agent]
system_prompt = "BASE"
compact_ratio = 0.5
recent_keep = 2

[[providers]]
name = "conn"
kind = "`+kind+`"
models = [`+quoteList(models)+`]
context_window = 1000000
`+overrides)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { ctrl.Close() })
	budget := func() tool.ContextBudget { return ctrl.Executor().ContextBudget() }
	rec.probe = budget
	return rec, func(prompt string) {
		t.Helper()
		if err := ctrl.Run(context.Background(), prompt); err != nil {
			t.Fatalf("Run(%q): %v", prompt, err)
		}
	}, budget
}

func quoteList(csv string) string {
	parts := strings.Split(csv, ",")
	for i, p := range parts {
		parts[i] = `"` + p + `"`
	}
	return strings.Join(parts, ", ")
}

const smallWindow = "model_overrides = { small = { context_window = 32000 } }\n"

// The model can still ask: under the same pressure the tool answers ok, and its
// figures are the ones the compaction trigger compares against.
func TestEffectContextBudgetToolAnswersUnderPressure(t *testing.T) {
	rec, run, budgetNow := budgetFixtureAgent(t, "boot-budget-query", "small", smallWindow, event.Discard)
	for _, prompt := range []string{"start the task", "keep going", "keep going"} {
		run(prompt)
	}
	run(budgetQueryPrompt)
	rec.mu.Lock()
	raw, before := rec.budget, rec.before
	rec.mu.Unlock()
	after := budgetNow()
	var got tool.ContextBudget
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("tool result %q is not a budget: %v", raw, err)
	}
	if got.Status != "ok" || got.Window != 32000 || got.CompactAt <= 0 || got.CompactAt >= got.Window || got.TokensUsed <= 0 {
		t.Fatalf("budget = %+v", got)
	}
	if got.CompactAt != after.CompactAt || got.Window != after.Window || got.CompactAt != before.CompactAt {
		t.Fatalf("tool %+v disagrees with the compaction trigger's own figures (before %+v, after %+v)", got, before, after)
	}
	if got.TokensUsed < before.TokensUsed || got.TokensUsed > after.TokensUsed {
		t.Fatalf("tool used=%d outside the compaction estimate's range [%d, %d]", got.TokensUsed, before.TokensUsed, after.TokensUsed)
	}
	if got.TokensUsed < got.CompactAt && got.TokensUsed+got.TokensRemaining != got.CompactAt {
		t.Fatalf("remaining does not count down to the compaction trigger: %+v", got)
	}
}

func TestEffectContextBudgetToolIsIndexedNotOffered(t *testing.T) {
	reqs := effectRun(t, "boot-budget-tool-surface", "", ablation.Set{})
	if toolNames(reqs[0])["context_budget"] {
		t.Fatalf("context_budget is in the first-round schema: %v", toolNames(reqs[0]))
	}
	if !strings.Contains(capabilityDescription(reqs[0]), "tool:context_budget") {
		t.Fatalf("use_capability does not index context_budget: %s", capabilityDescription(reqs[0]))
	}
}
