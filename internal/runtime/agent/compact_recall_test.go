package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// recallAgent holds a transcript whose first `covered` messages have been
// folded away, which is the only state where an index address means anything.
func recallAgent(t *testing.T, covered int, window int) *Agent {
	t.Helper()
	sess := sessionstore.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "never rewrite history, only append"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{ID: "c1", Name: "read_file", Arguments: `{"path":"internal/runtime/agent/compact.go"}`},
	}})
	sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "c1", Name: "read_file", Content: "func estimateTextTokens(s string) int {"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "keep going"})
	a := New(nil, nil, sess, Options{ContextWindow: window}, event.Discard)
	a.sess.win.compactionState.Generation = 1
	a.sess.win.compactionState.Projection = sessionstore.ContextProjection{CoveredCount: covered}
	return a
}

// An index line is an address; this is the read. What the user said is the
// thing recall exists for — no tool can re-derive it.
func TestRecallReturnsFoldedPositions(t *testing.T) {
	a := recallAgent(t, 3, 128_000)
	res, err := a.RecallContext(context.Background(), tool.RecallRequest{Positions: []int{1}})
	if err != nil {
		t.Fatalf("RecallContext: %v", err)
	}
	if !strings.Contains(res.Text, "never rewrite history") {
		t.Fatalf("recall lost the user turn it addressed:\n%s", res.Text)
	}
	if res.Tokens <= 0 || res.BudgetLeft >= a.window().recallBudget() {
		t.Fatalf("recall did not draw on the budget: tokens=%d left=%d of %d", res.Tokens, res.BudgetLeft, a.window().recallBudget())
	}
}

// The index addresses the call, so the call alone would be the half that says
// least: what read_file returned is the reason to recall it at all.
func TestRecallCarriesToolResultsWithTheirCall(t *testing.T) {
	a := recallAgent(t, 4, 128_000)
	res, err := a.RecallContext(context.Background(), tool.RecallRequest{Positions: []int{2}})
	if err != nil {
		t.Fatalf("RecallContext: %v", err)
	}
	if !strings.Contains(res.Text, "estimateTextTokens") {
		t.Fatalf("recalled a tool call without its result:\n%s", res.Text)
	}
}

// A position still in the model's context is not folded, and recalling it
// would pay twice for one fact.
func TestRecallRefusesWhatIsStillVisible(t *testing.T) {
	a := recallAgent(t, 2, 128_000)
	_, err := a.RecallContext(context.Background(), tool.RecallRequest{Positions: []int{3}})
	if err == nil || !strings.Contains(err.Error(), "still in your context") {
		t.Fatalf("err = %v, want a refusal naming the visible position", err)
	}
}

// The budget is what stops a recall loop from undoing the fold that freed the
// window. It refuses whole: half a span reads as the whole of what was there.
func TestRecallBudgetRefusesRatherThanTruncates(t *testing.T) {
	a := recallAgent(t, 3, 128_000)
	a.sess.win.compactionState.Recall = sessionstore.RecallLedger{Generation: 1, SpentTokens: a.window().recallBudget() - 1}
	_, err := a.RecallContext(context.Background(), tool.RecallRequest{Positions: []int{1}})
	if err == nil || !strings.Contains(err.Error(), "recall budget") {
		t.Fatalf("err = %v, want the budget to refuse the request", err)
	}
}

// Each compaction grants a fresh budget, so a long session keeps the ability to
// look back. The ledger carries its generation, so nothing has to reset it.
func TestRecallBudgetResetsWithTheGeneration(t *testing.T) {
	a := recallAgent(t, 3, 128_000)
	a.sess.win.compactionState.Recall = sessionstore.RecallLedger{Generation: 1, SpentTokens: a.window().recallBudget()}
	a.sess.win.compactionState.Generation = 2

	res, err := a.RecallContext(context.Background(), tool.RecallRequest{Positions: []int{1}})
	if err != nil {
		t.Fatalf("a new generation did not refresh the recall budget: %v", err)
	}
	if res.BudgetLeft <= 0 {
		t.Fatalf("budget left = %d after the first recall of a new generation", res.BudgetLeft)
	}
}

// The tool reaches the agent through the call context, so that one binding is
// the whole wiring: without it recall is a tool that can only report itself
// unavailable, and every test above would still pass.
func TestToolCallContextCarriesTheRecaller(t *testing.T) {
	prov := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "c1", Name: "probe", Arguments: `{}`}},
		{Type: provider.ChunkDone},
	}}
	probe := &recallerProbe{}
	reg := tool.NewRegistry()
	reg.Add(probe)
	a := New(prov, reg, sessionstore.NewSession(""), Options{MaxSteps: 1}, event.Discard)
	_ = a.Run(context.Background(), "go") // errors at the 1-step cap; the probe is the point

	if !probe.bound {
		t.Fatal("a tool call ran with no ContextRecaller bound to its context")
	}
}

type recallerProbe struct{ bound bool }

func (*recallerProbe) Name() string            { return "probe" }
func (*recallerProbe) Description() string     { return "probe" }
func (*recallerProbe) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*recallerProbe) ReadOnly() bool          { return true }

func (p *recallerProbe) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	_, p.bound = tool.ContextRecallerFromContext(ctx)
	return "ok", nil
}

// Nothing folded means every address is still in context; saying so beats
// returning content the caller already has.
func TestRecallBeforeAnyFoldIsRefused(t *testing.T) {
	a := recallAgent(t, 0, 128_000)
	_, err := a.RecallContext(context.Background(), tool.RecallRequest{Positions: []int{1}})
	if err == nil || !strings.Contains(err.Error(), "nothing has been folded") {
		t.Fatalf("err = %v, want a refusal explaining that nothing is folded", err)
	}
}
