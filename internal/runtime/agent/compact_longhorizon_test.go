package agent

import (
	"context"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// Long-horizon retrievability: after N compactions, can the agent still reach a
// fact stated once at the start? The carriers reach it differently.

// Assistant reasoning never enters the fold index (buildFoldIndex records user
// turns and tool calls, not conclusions), so search is its only route from the
// first fold on. A tool call gets a line, trimmed later out of a bounded budget.

// retrievalCarrier plants one findable fact and says what should reach it.
type retrievalCarrier struct {
	name  string
	query string
	// needle is the exact text a read must return.
	needle string
	// plant appends the carrier and returns its canonical position.
	plant func(msgs []provider.Message) ([]provider.Message, int)
	// indexed is whether this kind of message ever gets a fold-index line.
	indexed bool
}

const (
	horizonDecision = "timeout ownership belongs to the controller because middleware cannot observe cancellation"
	horizonCommand  = "go test ./internal/hydration/ -run TestSeedOrdering"
	horizonFailure  = "--- FAIL: TestSeedOrdering: seeds applied before the schema migration"
	horizonCJK      = "决定由控制器持有超时所有权，因为中间件观察不到取消信号"
)

func plantMessage(m provider.Message) func([]provider.Message) ([]provider.Message, int) {
	return func(msgs []provider.Message) ([]provider.Message, int) {
		return append(msgs, m), len(msgs)
	}
}

func retrievalCarriers() []retrievalCarrier {
	return []retrievalCarrier{
		{
			name: "assistant_reasoning", query: "timeout ownership controller middleware cancellation",
			needle:  horizonDecision,
			plant:   plantMessage(provider.Message{Role: provider.RoleAssistant, Content: horizonDecision}),
			indexed: false,
		},
		{
			name: "assistant_reasoning_cjk", query: "超时所有权 中间件",
			needle:  horizonCJK,
			plant:   plantMessage(provider.Message{Role: provider.RoleAssistant, Content: horizonCJK}),
			indexed: false,
		},
		{
			name: "tool_input", query: "TestSeedOrdering hydration",
			needle: horizonCommand,
			plant: func(msgs []provider.Message) ([]provider.Message, int) {
				at := len(msgs)
				return append(msgs,
					provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
						{ID: "h1", Name: "bash", Arguments: fmt.Sprintf(`{"command":%q}`, horizonCommand)},
					}},
					provider.Message{Role: provider.RoleTool, ToolCallID: "h1", Name: "bash", Content: "ok"},
				), at
			},
			indexed: true,
		},
		{
			name: "failed_tool_output", query: "seeds applied before schema migration",
			needle: horizonFailure,
			plant: func(msgs []provider.Message) ([]provider.Message, int) {
				at := len(msgs)
				return append(msgs,
					provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
						{ID: "h2", Name: "bash", Arguments: fmt.Sprintf(`{"command":%q}`, horizonCommand)},
					}},
					provider.Message{Role: provider.RoleTool, ToolCallID: "h2", Name: "bash", Content: "error: " + horizonFailure},
				), at
			},
			indexed: true,
		},
	}
}

// horizonSession plants the carrier early, then buries it under bulk the free
// prune cannot reclaim.
func horizonSession(c retrievalCarrier, turns int) (*sessionstore.Session, int) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "start the hydration work"},
	}
	msgs, at := c.plant(msgs)
	big := strings.Repeat("word ", 400)
	for i := range turns {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, Content: big},
			provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("continue %d", i)},
		)
	}
	return &sessionstore.Session{Messages: msgs}, at
}

// growSession adds another generation's worth of foldable work.
func growSession(a *Agent, gen int) {
	for i := range 6 {
		a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("word ", 400)})
		a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("gen %d step %d", gen, i)})
	}
}

// indexCarriesAddress reports whether any visible fold index still addresses
// this position. It is an observation, not a requirement: search must work
// whether or not the index happens to have kept the line.
func indexCarriesAddress(a *Agent, pos int) bool {
	canonical, _ := a.sess.conversation.SnapshotMessagesVersion()
	for _, m := range modelVisibleFromProjection(a.sess.win.compactionState.Projection, canonical) {
		_, index := splitFoldIndex(m.Content)
		if index == "" {
			continue
		}
		for _, line := range indexBodyLines(index) {
			if strings.HasPrefix(strings.TrimPrefix(line, "- "), fmt.Sprintf("#%d ", pos)) {
				return true
			}
		}
	}
	return false
}

func hitRank(res tool.RecallResult, want int) int {
	for i, h := range res.Hits {
		if h.Position == want {
			return i + 1
		}
	}
	return 0
}

// The pin: whatever a projection lost, a search finds and a read returns
// verbatim, for as many generations as the session runs.
func TestLongHorizonRetrievability(t *testing.T) {
	for _, c := range retrievalCarriers() {
		t.Run(c.name, func(t *testing.T) {
			sess, at := horizonSession(c, 6)
			a := agentOverForce(t, &fakeProvider{reply: "Work continued."}, sess)

			const generations = 50
			indexHeldUntil, lostAt, proven := -1, -1, 0
			for gen := range generations {
				if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
					t.Fatalf("generation %d prepare = %v", gen, err)
				}
				growSession(a, gen)
				if indexCarriesAddress(a, at) {
					indexHeldUntil = gen
				}
				// A tool call's index line quotes its command, so an indexed
				// carrier stays legible until that line is trimmed. Only once
				// the fact is genuinely out of sight does a hit prove anything.
				if !foldedOutOfSight(a, c.needle) {
					continue
				}
				if lostAt < 0 {
					lostAt = gen
				}
				proven++
				res := searchRecall(t, a, c.query)
				rank := hitRank(res, at)
				if rank == 0 {
					t.Fatalf("generation %d: search %q returned %v, want #%d\n%s",
						gen, c.query, hitPositions(res), at, res.Text)
				}
				if rank > 5 {
					t.Errorf("generation %d: #%d ranked %d, outside the first five hits", gen, at, rank)
				}
			}
			if proven == 0 {
				t.Fatalf("the fact never left the model's sight in %d generations; this run tested nothing", generations)
			}
			t.Logf("out of sight from generation %d; search verified %d generations", lostAt, proven)
			// Exact recovery after every generation of loss.
			read, err := a.RecallContext(context.Background(), tool.RecallRequest{Positions: []int{at}})
			if err != nil {
				t.Fatalf("read #%d after %d generations = %v", at, generations, err)
			}
			if !strings.Contains(read.Text, c.needle) {
				t.Errorf("read #%d returned %q, want the original wording", at, read.Text)
			}
			switch {
			case !c.indexed && indexHeldUntil >= 0:
				t.Errorf("an unindexed carrier held an index address through generation %d", indexHeldUntil)
			case c.indexed:
				t.Logf("index addressed #%d through generation %d of %d; search covered all %d",
					at, indexHeldUntil, generations, generations)
			}
		})
	}
}

// Rank must not decay as the corpus grows: a long session is exactly where the
// fact is hardest to find and most worth finding.
func TestLongHorizonRankHoldsAcrossCorpusSizes(t *testing.T) {
	c := retrievalCarriers()[0]
	for _, turns := range []int{6, 60, 300} {
		t.Run(fmt.Sprintf("turns=%d", turns), func(t *testing.T) {
			sess, at := horizonSession(c, turns)
			a := agentOverForceWindow(t, &fakeProvider{reply: "Work continued."}, sess, 60000)
			if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
				t.Fatalf("prepare = %v", err)
			}
			if !foldedOutOfSight(a, c.needle) {
				t.Skip("this corpus size does not fold the carrier away; nothing to prove here")
			}
			res := searchRecall(t, a, c.query)
			rank := hitRank(res, at)
			if rank == 0 {
				t.Fatalf("search returned %v, want #%d", hitPositions(res), at)
			}
			folded := a.sess.win.compactionState.Projection.CoveredCount
			t.Logf("corpus=%d folded=%d rank=%d cost=%d tokens", turns, folded, rank, res.Tokens)
			if rank > 3 {
				t.Errorf("rank %d in a %d-message folded region; the fact is being crowded out", rank, folded)
			}
		})
	}
}

// A search must not quietly cost more as the session grows: it is charged to
// the same generation budget a read draws on.
func TestLongHorizonSearchCostStaysBounded(t *testing.T) {
	c := retrievalCarriers()[0]
	sess, _ := horizonSession(c, 300)
	a := agentOverForceWindow(t, &fakeProvider{reply: "Work continued."}, sess, 60000)
	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	res := searchRecall(t, a, c.query)
	budget := a.window().recallBudget()
	if res.Tokens > budget/4 {
		t.Errorf("one search cost %d tokens of a %d budget; three would exhaust it", res.Tokens, budget)
	}
	t.Logf("search over %d folded messages cost %d tokens (budget %d)",
		a.sess.win.compactionState.Projection.CoveredCount, res.Tokens, budget)
}

// Every carrier is reachable from a cold read of the transcript, with no index
// and no projection consulted. This is the invariant the other tests rest on.
func TestSearchIsIndependentOfProjectionState(t *testing.T) {
	for _, c := range retrievalCarriers() {
		msgs, at := c.plant([]provider.Message{{Role: provider.RoleSystem, Content: "sys"}})
		hits, err := searchFoldedRegion(msgs, c.query, 8)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		positions := make([]int, 0, len(hits))
		for _, h := range hits {
			positions = append(positions, h.Position)
		}
		if !slices.Contains(positions, at) {
			t.Errorf("%s: hits %v, want #%d", c.name, positions, at)
		}
	}
}
