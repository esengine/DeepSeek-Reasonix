package agent

import (
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/model/openai"
	"reasonix/internal/state/sessionstore"
)

// gen names one model-visible history build; a fold moves the projection.
func gen(projection uint64) ContextGeneration {
	return ContextGeneration{Projection: projection}
}

func budgetAt(used, compactAt, window int) tool.ContextBudget {
	return tool.ContextBudget{
		Status:          "ok",
		TokensRemaining: max(0, compactAt-used),
		TokensUsed:      used,
		CompactAt:       compactAt,
		Window:          window,
	}
}

func TestContextBudgetRungCrossings(t *testing.T) {
	const compactAt = 10_000
	cases := []struct {
		used int
		want int
	}{
		{0, 0},
		{7_499, 0},
		{7_500, 1}, // 0.75
		{9_199, 1},
		{9_200, 2}, // 0.92
		{20_000, 2},
	}
	for _, tc := range cases {
		if got := contextBudgetRung(budgetAt(tc.used, compactAt, 12_000)); got != tc.want {
			t.Fatalf("used %d: rung = %d, want %d", tc.used, got, tc.want)
		}
	}
}

// A rung must fire exactly once. The notice occupies the context it reports on,
// so a re-fire on every step would spend the remaining window describing it.
func TestBudgetNoticeFiresOncePerRung(t *testing.T) {
	var latch budgetNoticeLatch
	notice, latch := advanceBudgetNotice(latch, budgetAt(7_600, 10_000, 12_000), gen(3))
	if notice == "" {
		t.Fatal("first crossing produced no notice")
	}
	if !strings.Contains(notice, "<context-budget>") {
		t.Fatalf("notice is not a tagged fragment: %q", notice)
	}
	for _, used := range []int{7_700, 8_000, 9_000} {
		var again string
		again, latch = advanceBudgetNotice(latch, budgetAt(used, 10_000, 12_000), gen(3))
		if again != "" {
			t.Fatalf("used %d re-fired the same rung: %q", used, again)
		}
	}
	notice, latch = advanceBudgetNotice(latch, budgetAt(9_500, 10_000, 12_000), gen(3))
	if notice == "" {
		t.Fatal("second rung did not fire")
	}
	if latch.rung != 2 {
		t.Fatalf("latch.rung = %d, want 2", latch.rung)
	}
}

// A fold drops usage and starts a new history build. Every rung must re-arm:
// the model that just survived a compaction has not been told about the next one.
func TestBudgetNoticeReArmsAfterFold(t *testing.T) {
	var latch budgetNoticeLatch
	_, latch = advanceBudgetNotice(latch, budgetAt(9_500, 10_000, 12_000), gen(1))
	if latch.rung != 2 {
		t.Fatalf("setup: latch.rung = %d, want 2", latch.rung)
	}
	notice, latch := advanceBudgetNotice(latch, budgetAt(7_600, 10_000, 12_000), gen(2))
	if notice == "" {
		t.Fatal("rung 1 did not re-arm after the history build changed")
	}
	if latch.rung != 1 || latch.generation != gen(2) {
		t.Fatalf("latch = %+v, want rung 1 at generation 2", latch)
	}
}

// An unmeasured budget must stay silent rather than report zero room left.
func TestBudgetNoticeSilentWhenUnmeasured(t *testing.T) {
	notice, latch := advanceBudgetNotice(budgetNoticeLatch{}, unmeasuredContextBudget("no window"), gen(1))
	if notice != "" {
		t.Fatalf("unmeasured budget produced a notice: %q", notice)
	}
	if latch != (budgetNoticeLatch{}) {
		t.Fatalf("unmeasured budget moved the latch: %+v", latch)
	}
}

func TestContextBudgetUnmeasuredWithoutWindow(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 0, compactRatio: defaultCompactRatio}}
	budget := a.ContextBudget()
	if budget.Known() {
		t.Fatalf("budget reported as known without a window: %+v", budget)
	}
	if budget.Status != "unmeasured" || budget.Reason == "" {
		t.Fatalf("unmeasured budget must name a reason: %+v", budget)
	}
}

func TestContextBudgetNoticeEventCarriesTypedFigures(t *testing.T) {
	ev := contextBudgetNoticeEvent(budgetAt(83, 100, 200))
	if ev.Kind != event.Notice || ev.Level != event.LevelWarn || ev.Code != event.NoticeCodeContextBudget {
		t.Fatalf("want a warn notice coded context_budget, got kind=%v level=%v code=%q", ev.Kind, ev.Level, ev.Code)
	}
	figures, ok := event.DecodeContextBudgetFigures(ev.Detail)
	if !ok || figures.Percent != 83 || figures.Remaining != 17 {
		t.Fatalf("Detail must decode to the percent and remaining tokens, got %+v ok=%v from %q", figures, ok, ev.Detail)
	}
}

func TestContextBudgetNoticeEventForUnmeasuredBudgetHasNoCode(t *testing.T) {
	ev := contextBudgetNoticeEvent(unmeasuredContextBudget("no window"))
	if ev.Code != "" || ev.Text == "" {
		t.Fatalf("an unmeasured budget has no figures to localize, got code=%q text=%q", ev.Code, ev.Text)
	}
}

// Both rungs must say that the fold is automatic and the task goes on: a notice
// that only reports shrinking room reads as the end of the window, and the model
// hands the work back to the user.
func TestBudgetNoticeStatesCompactionContinuesTheTask(t *testing.T) {
	for rung := 1; rung <= len(contextBudgetNoticeRatios); rung++ {
		text := contextBudgetNoticeText(budgetAt(9_500, 10_000, 12_000), rung)
		if !strings.Contains(text, budgetContinuationClause) {
			t.Fatalf("rung %d omits the continuation clause:\n%s", rung, text)
		}
		for _, figure := range []string{"500 tokens", "12000"} {
			if !strings.Contains(text, figure) {
				t.Fatalf("rung %d omits %q:\n%s", rung, figure, text)
			}
		}
	}
}

// Local bookkeeping (a tool call's diff, a decision receipt) bumps the session's
// rewrite counter without changing a byte the provider sees. The notice must not
// re-arm on it: that repeated the same warning on every tool round.
func TestBudgetNoticeIgnoresBookkeepingRewrites(t *testing.T) {
	prov, err := openai.New(provider.Config{
		Name: "deepseek", BaseURL: "http://127.0.0.1:1", Model: "deepseek-reasoner", APIKey: "test",
		Extra: map[string]any{"api_key_env": "DEEPSEEK_API_KEY"},
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	sess := sessionstore.NewSession(systemPrompt)
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 20000, CompactRatio: 0.5, MaxSteps: 4}, &collectSink{})
	for rung := 0; rung < 1; {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("filler words here. ", 100)})
		rung = contextBudgetRung(a.ContextBudget())
		if sess.Len() > 400 {
			t.Fatal("never reached the first rung")
		}
	}
	if a.window().contextBudgetNotice() == "" {
		t.Fatal("first crossing produced no notice")
	}
	for range 3 {
		sess.IncrementRewrite()
		if again := a.window().contextBudgetNotice(); again != "" {
			t.Fatalf("bookkeeping rewrite re-armed the notice: %q", again)
		}
	}
}

// Usage falling back under a rung (a rewind) re-arms it.
func TestBudgetNoticeReArmsWhenUsageFallsBelowRung(t *testing.T) {
	var latch budgetNoticeLatch
	_, latch = advanceBudgetNotice(latch, budgetAt(9_500, 10_000, 12_000), gen(1))
	_, latch = advanceBudgetNotice(latch, budgetAt(3_000, 10_000, 12_000), gen(1))
	if latch.rung != 0 {
		t.Fatalf("latch.rung = %d, want 0 after usage fell", latch.rung)
	}
	if notice, _ := advanceBudgetNotice(latch, budgetAt(7_600, 10_000, 12_000), gen(1)); notice == "" {
		t.Fatal("rung 1 stayed silent after a rewind")
	}
}
