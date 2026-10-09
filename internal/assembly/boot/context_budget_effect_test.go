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
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting the earlier part") {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "## Standing facts\n- none recorded"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		return ch, nil
	}
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

func (p *budgetEffectProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

func isSummaryRequest(req provider.Request) bool {
	return len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting the earlier part")
}

func countBudgetBlocks(req provider.Request) int {
	n := 0
	for _, m := range req.Messages {
		n += strings.Count(m.Content, "<context-budget>")
	}
	return n
}

func budgetFixture(t *testing.T, kind, models, overrides string, sink event.Sink) (*budgetEffectProvider, func(prompt string)) {
	rec, run, _ := budgetFixtureAgent(t, kind, models, overrides, sink)
	return rec, run
}

func budgetFixtureAgent(t *testing.T, kind, models, overrides string, sink event.Sink) (*budgetEffectProvider, func(prompt string), func() tool.ContextBudget) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &budgetEffectProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
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

// retiredBudgetNoticeCode is the notice code the push used; no sink may see it.
const retiredBudgetNoticeCode = "context_budget"

const smallWindow = "model_overrides = { small = { context_window = 32000 } }\n"

// Context pressure is never pushed: through every rung and past the compaction
// trigger, no request carries a budget block and the frontend sees no warning.
func TestEffectContextPressureIsNeverPushed(t *testing.T) {
	var mu sync.Mutex
	var warns []event.Event
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Code == retiredBudgetNoticeCode {
			mu.Lock()
			warns = append(warns, e)
			mu.Unlock()
		}
	})
	rec, run := budgetFixture(t, "boot-budget-nopush", "small", smallWindow, sink)
	for _, prompt := range []string{"start the task", "keep going", "keep going", "keep going", "keep going"} {
		run(prompt)
	}
	folded := false
	for i, req := range rec.requests() {
		if n := countBudgetBlocks(req); n != 0 {
			t.Fatalf("request %d carried %d context-budget blocks; pressure must not be pushed", i, n)
		}
		folded = folded || isSummaryRequest(req)
	}
	if !folded {
		t.Fatal("the conversation never reached the compaction trigger; the fixture proves nothing")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(warns) != 0 {
		t.Fatalf("the frontend received %d context-budget notices, first: %+v", len(warns), warns[0])
	}
}

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

func TestEffectContextBudgetToolIsOffered(t *testing.T) {
	reqs := effectRun(t, "boot-budget-tool-surface", "", ablation.Set{})
	if !toolNames(reqs[0])["context_budget"] {
		t.Fatalf("context_budget absent from the provider tool surface: %v", toolNames(reqs[0]))
	}
}
