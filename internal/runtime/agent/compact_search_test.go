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

// obscureConstraint is a fact stated once, early, that no later turn repeats.
// It is the thing a long session loses: the digest paraphrases it away, and
// the index line addressing it is eventually evicted or never minted.
const obscureConstraint = "the vendored zlib fork cannot be replaced because its checksum is pinned by the firmware loader"

// searchableSession buries the constraint in assistant reasoning at a known
// canonical position. Not a user turn: keepUserTurns holds old ones verbatim
// oldest-first, so a fact placed there stays visible and proves nothing.
func searchableSession(turns int) (*sessionstore.Session, int) {
	big := strings.Repeat("word ", 400)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "start the migration"},
	}
	constraintAt := len(msgs)
	msgs = append(msgs, provider.Message{Role: provider.RoleAssistant, Content: obscureConstraint})
	for i := range turns {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, Content: big},
			provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("continue %d", i)},
		)
	}
	return &sessionstore.Session{Messages: msgs}, constraintAt
}

// foldedOutOfSight reports that the model can no longer read the fact anywhere
// in its context — not in the digest, not in a kept turn, not as an index line.
// Every claim about search rests on this being true first.
func foldedOutOfSight(a *Agent, needle string) bool {
	canonical, _ := a.sess.conversation.SnapshotMessagesVersion()
	for _, m := range modelVisibleFromProjection(a.sess.win.compactionState.Projection, canonical) {
		if strings.Contains(m.Content, needle) {
			return false
		}
	}
	return true
}

func searchRecall(t *testing.T, a *Agent, query string) tool.RecallResult {
	t.Helper()
	res, err := a.RecallContext(context.Background(), tool.RecallRequest{Query: query})
	if err != nil {
		t.Fatalf("search %q = %v", query, err)
	}
	return res
}

func hitPositions(res tool.RecallResult) []int {
	out := make([]int, 0, len(res.Hits))
	for _, h := range res.Hits {
		out = append(out, h.Position)
	}
	return out
}

func containsPosition(positions []int, want int) bool {
	return slices.Contains(positions, want)
}

// The address a search returns is the canonical one, and a read at it returns
// the original words. This is the whole claim: the transcript is the source of
// truth, and the projection is not consulted to find anything in it.
func TestSearchFindsFoldedFactAndReadsItBack(t *testing.T) {
	sess, constraintAt := searchableSession(6)
	a := agentOverForce(t, &fakeProvider{reply: "Work continued."}, sess)
	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v", err)
	}

	// The premise, asserted rather than assumed: if a kept turn or the digest
	// still showed this, finding it would prove nothing.
	if !foldedOutOfSight(a, "vendored zlib") {
		t.Fatal("the fixture is still visible to the model; the test would pass for the wrong reason")
	}
	res := searchRecall(t, a, "vendored zlib firmware checksum")
	if !containsPosition(hitPositions(res), constraintAt) {
		t.Fatalf("search hits %v, want the constraint at #%d\n%s", hitPositions(res), constraintAt, res.Text)
	}
	read, err := a.RecallContext(context.Background(), tool.RecallRequest{Positions: []int{constraintAt}})
	if err != nil {
		t.Fatalf("read #%d = %v", constraintAt, err)
	}
	if !strings.Contains(read.Text, obscureConstraint) {
		t.Errorf("read #%d returned %q, want the original wording", constraintAt, read.Text)
	}
}

// Generation after generation, the fact stays out of sight and stays findable.
// canonicalOriginFor returns -1 for anything inside a previous projection, so
// re-folding cannot re-derive an address; the index only carries one forward
// while its budget holds. Search answers regardless, because it reads the
// transcript rather than asking the projection where anything came from.
func TestSearchSurvivesGenerationsThatLostTheAddress(t *testing.T) {
	sess, constraintAt := searchableSession(6)
	a := agentOverForce(t, &fakeProvider{reply: "Work continued."}, sess)

	const generations = 6
	for gen := range generations {
		if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
			t.Fatalf("generation %d prepare = %v", gen, err)
		}
		// Grow the session so the next generation has something to fold.
		for i := range 6 {
			a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("word ", 400)})
			a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("gen %d step %d", gen, i)})
		}
		if !foldedOutOfSight(a, "vendored zlib") {
			t.Fatalf("generation %d still shows the fixture; the test would pass for the wrong reason", gen)
		}
		// The budget resets per generation, so a search here is affordable.
		res := searchRecall(t, a, "vendored zlib firmware checksum")
		if !containsPosition(hitPositions(res), constraintAt) {
			t.Fatalf("generation %d lost the address: hits %v, want #%d", gen, hitPositions(res), constraintAt)
		}
	}
	// And it is still exactly readable at the end of all of it.
	read, err := a.RecallContext(context.Background(), tool.RecallRequest{Positions: []int{constraintAt}})
	if err != nil {
		t.Fatalf("read after %d generations = %v", generations, err)
	}
	if !strings.Contains(read.Text, obscureConstraint) {
		t.Errorf("read returned %q, want the original wording", read.Text)
	}
}

// A tool result is found by its own output and addressed by the call that
// produced it, so one read returns the command and what it printed.
func TestSearchAddressesToolOutputByItsCaller(t *testing.T) {
	region := []provider.Message{
		{Role: provider.RoleUser, Content: "run the suite"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "b1", Name: "bash", Arguments: `{"command":"go test ./internal/parser/"}`},
		}},
		{Role: provider.RoleTool, ToolCallID: "b1", Name: "bash", Content: "--- FAIL: TestLexerBacktrack"},
	}
	hits, err := searchFoldedRegion(region, "TestLexerBacktrack", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("the failure text was not found")
	}
	if hits[0].Position != 1 {
		t.Errorf("hit position = %d, want #1 (the call), not #2 (its result)", hits[0].Position)
	}
	if hits[0].Kind != recallKindToolOut || hits[0].Tool != "bash" {
		t.Errorf("hit = %+v, want a bash tool_output", hits[0])
	}
}

// Live tail messages are already in context. Returning them would spend recall
// budget to show the model what it can already read.
func TestSearchNeverReturnsTheLiveTail(t *testing.T) {
	sess, _ := searchableSession(6)
	a := agentOverForce(t, &fakeProvider{reply: "Work continued."}, sess)
	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	const tailMarker = "quaggaplinth"
	a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: "note: " + tailMarker})

	res := searchRecall(t, a, tailMarker)
	covered := a.sess.win.compactionState.Projection.CoveredCount
	for _, h := range res.Hits {
		if h.Position >= covered {
			t.Errorf("search returned #%d from the live tail (covered=%d)", h.Position, covered)
		}
	}
}

// The tokenizer's CJK bigrams are what make a Chinese session searchable at
// all; a recall path that lost them would answer only English queries.
func TestSearchFindsCJKText(t *testing.T) {
	region := []provider.Message{
		{Role: provider.RoleUser, Content: "决定保留控制器持有超时所有权，不要交给中间件"},
		{Role: provider.RoleAssistant, Content: "understood"},
	}
	hits, err := searchFoldedRegion(region, "超时所有权", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Position != 0 {
		t.Fatalf("hits = %+v, want the Chinese user turn at #0", hits)
	}
}

// Search and read draw on one budget. Two would let a search refill what a read
// was just refused, which is the loop the budget exists to stop.
func TestSearchSpendsTheSameBudgetAsRead(t *testing.T) {
	sess, _ := searchableSession(6)
	a := agentOverForce(t, &fakeProvider{reply: "Work continued."}, sess)
	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	first := searchRecall(t, a, "vendored zlib firmware checksum")
	if first.Tokens <= 0 {
		t.Fatalf("search reported no cost: %+v", first)
	}
	spent := a.sess.win.compactionState.Recall.SpentTokens
	if spent != first.Tokens {
		t.Errorf("ledger spent %d, search cost %d", spent, first.Tokens)
	}
	second := searchRecall(t, a, "vendored zlib firmware checksum")
	if second.BudgetLeft >= first.BudgetLeft {
		t.Errorf("budget did not drain across searches: %d then %d", first.BudgetLeft, second.BudgetLeft)
	}
}

// A query nothing matches is an answer, not a failure: knowing a fact is not in
// the folded region is what stops the model hunting for it.
func TestSearchWithNoMatchIsAnAnswer(t *testing.T) {
	sess, _ := searchableSession(6)
	a := agentOverForce(t, &fakeProvider{reply: "Work continued."}, sess)
	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	res := searchRecall(t, a, "zzzznothinglikethis")
	if len(res.Hits) != 0 {
		t.Fatalf("hits = %+v, want none", res.Hits)
	}
	if !strings.Contains(res.Text, "No folded message matches") {
		t.Errorf("text = %q, want it to say the region was searched", res.Text)
	}
	if res.Tokens != 0 {
		t.Errorf("an empty result cost %d tokens", res.Tokens)
	}
}

// Ambiguous requests are refused rather than resolved: either reading has the
// tool doing something the caller did not ask for.
func TestRecallRefusesBothPositionsAndQuery(t *testing.T) {
	sess, _ := searchableSession(6)
	a := agentOverForce(t, &fakeProvider{reply: "Work continued."}, sess)
	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	if _, err := a.RecallContext(context.Background(), tool.RecallRequest{
		Positions: []int{2}, Query: "zlib",
	}); err == nil {
		t.Fatal("a request with both positions and a query was accepted")
	}
}

func BenchmarkSearchFoldedRegion(b *testing.B) {
	for _, size := range []int{1000, 5000, 20000} {
		region := make([]provider.Message, 0, size)
		for i := range size {
			region = append(region, provider.Message{
				Role:    provider.RoleAssistant,
				Content: fmt.Sprintf("step %d: adjusted the parser table and re-ran the suite for module %d", i, i%97),
			})
		}
		region[size/2].Content = obscureConstraint
		b.Run(fmt.Sprintf("messages=%d", size), func(b *testing.B) {
			for b.Loop() {
				if _, err := searchFoldedRegion(region, "vendored zlib firmware checksum", 8); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
