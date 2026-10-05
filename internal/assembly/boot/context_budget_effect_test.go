package boot

import (
	"context"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// budgetEffectProvider pads every reply so the transcript climbs toward the
// compaction trigger, and answers summarizer turns with an empty digest so a
// fold cannot be mistaken for the notice under test.
type budgetEffectProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
	bulk string
}

func (p *budgetEffectProvider) Name() string { return "boot-budget-effect" }

func (p *budgetEffectProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	text := p.bulk
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting the earlier part") {
		text = "## Standing facts\n- none recorded"
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: text}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *budgetEffectProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

func countBudgetNotices(req provider.Request) int {
	n := 0
	for _, m := range req.Messages {
		n += strings.Count(m.Content, "<context-budget>")
	}
	return n
}

// TestEffectContextBudgetNoticeReachesTheModel is the final-boundary proof: a
// conversation climbing toward its fold must carry the budget notice into an
// actual provider request, and must not carry one before the pressure exists.
func TestEffectContextBudgetNoticeReachesTheModel(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &budgetEffectProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
	provider.Register("boot-budget-effect", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
compact_ratio = 0.5
recent_keep = 2

[[providers]]
name = "test-model"
kind = "boot-budget-effect"
model = "x"
context_window = 32000
`)
	approveWorkspace(t, dir)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	for _, prompt := range []string{"start the task", "keep going", "keep going", "keep going", "keep going"} {
		if err := ctrl.Run(context.Background(), prompt); err != nil {
			t.Fatalf("Run(%q): %v", prompt, err)
		}
	}

	reqs := rec.requests()
	if got := countBudgetNotices(reqs[0]); got != 0 {
		t.Fatalf("the first request already carried %d budget notices; the fixture starts under pressure", got)
	}
	warned := -1
	for i, req := range reqs {
		if countBudgetNotices(req) > 0 {
			warned = i
			break
		}
	}
	if warned < 0 {
		t.Fatalf("no request carried a budget notice across %d requests; the model was never told", len(reqs))
	}
	if got := countBudgetNotices(reqs[warned]); got != 1 {
		t.Fatalf("first warned request carried %d notices, want 1", got)
	}

	var notice string
	for _, m := range reqs[warned].Messages {
		if strings.Contains(m.Content, "<context-budget>") {
			notice = m.Content
		}
	}
	if !strings.Contains(notice, "Keep working; do not stop, hand the work back") {
		t.Fatalf("the notice that reached the provider does not tell the model the task continues across the fold:\n%s", notice)
	}

	// The rungs are edge-triggered, so the run can accumulate at most one notice
	// per rung per fold — never one per step.
	for i, req := range reqs {
		if got := countBudgetNotices(req); got > len(reqs) {
			t.Fatalf("request %d carried %d notices; the latch is re-firing", i, got)
		}
	}
}

// The pull side has to reach the model too: a notice the model cannot follow up
// on leaves it guessing between the two rungs.
func TestEffectContextBudgetToolIsOffered(t *testing.T) {
	reqs := effectRun(t, "boot-budget-tool-surface", "", ablation.Set{})
	if !toolNames(reqs[0])["context_budget"] {
		t.Fatalf("context_budget absent from the provider tool surface: %v", toolNames(reqs[0]))
	}
}

// TestEffectContextBudgetNoticeReachesTheFrontendCoded proves the user-facing
// half at the frontend sink: the notice that tells the user the model was warned
// carries its code and figures, so every frontend can word it in its own language.
func TestEffectContextBudgetNoticeReachesTheFrontendCoded(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &budgetEffectProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
	provider.Register("boot-budget-effect-sink", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
compact_ratio = 0.5
recent_keep = 2

[[providers]]
name = "test-model"
kind = "boot-budget-effect-sink"
model = "x"
context_window = 32000
`)
	approveWorkspace(t, dir)

	var mu sync.Mutex
	var coded []event.Event
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Code == event.NoticeCodeContextBudget {
			mu.Lock()
			coded = append(coded, e)
			mu.Unlock()
		}
	})
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	for _, prompt := range []string{"start the task", "keep going", "keep going", "keep going", "keep going"} {
		if err := ctrl.Run(context.Background(), prompt); err != nil {
			t.Fatalf("Run(%q): %v", prompt, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(coded) == 0 {
		t.Fatal("the frontend never received a coded context_budget notice")
	}
	figures, ok := event.DecodeContextBudgetFigures(coded[0].Detail)
	if !ok || figures.Percent < 75 {
		t.Fatalf("Detail must carry the figures at or past the first rung, got %+v ok=%v", figures, ok)
	}
}
