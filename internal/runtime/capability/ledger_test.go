package capability

import (
	"fmt"
	"slices"
	"testing"
)

func TestLedgerRequireAndPreferGates(t *testing.T) {
	l := NewLedger()
	l.SeedCandidates(RouteDecision{Candidates: []RouteCandidate{
		{Entry: Entry{ID: "skill:review"}, Policy: AutoUseRequire, Reason: "user asked"},
		{Entry: Entry{ID: "mcp-tool:github/search"}, Policy: AutoUsePrefer, Reason: "github"},
	}})

	gate := l.CheckFinalGate()
	if gate.Reason == "" || len(gate.RequireIDs) != 1 {
		t.Fatalf("expected require missing, got %+v", gate)
	}

	l.MarkSucceeded("skill:review")
	gate = l.CheckFinalGate()
	if !gate.PreferRemind || len(gate.PreferIDs) != 1 {
		t.Fatalf("expected prefer reminder, got %+v", gate)
	}
	l.MarkReminded("mcp-tool:github/search")
	gate = l.CheckFinalGate()
	if gate.PreferRemind || len(gate.PreferIDs) != 1 {
		t.Fatalf("expected prefer hard fail after reminder, got %+v", gate)
	}
	if err := l.MarkDeclined("mcp-tool:github/search", "not needed for this edit"); err != nil {
		t.Fatal(err)
	}
	gate = l.CheckFinalGate()
	if gate.Reason != "" {
		t.Fatalf("expected clear after decline, got %+v", gate)
	}
}

func TestLedgerDeclineCannotSkipRequire(t *testing.T) {
	l := NewLedger()
	l.SeedCandidates(RouteDecision{Candidates: []RouteCandidate{
		{Entry: Entry{ID: "skill:audit"}, Policy: AutoUseRequire},
	}})
	// MarkDeclined is allowed on ledger; host use_capability rejects require declines.
	if err := l.MarkDeclined("skill:audit", "skip"); err != nil {
		t.Fatal(err)
	}
	// After decline of require, CheckFinalGate still wants success unless unavailable.
	// Decline sets outcome declined; require path only accepts succeeded/unavailable.
	gate := l.CheckFinalGate()
	if gate.Reason == "" {
		t.Fatal("declined require should still block final delivery")
	}
}

// The pool is the router's input, not a second router. Nothing in it may turn
// on how the request is worded — an entry whose description shares no word with
// the task still reaches the model that is supposed to judge it.
func TestSemanticPoolFiltersOnlyOnStructure(t *testing.T) {
	entries := []Entry{
		{ID: "skill:review", Kind: KindSkill, Name: "review", Description: "code review", AutoUse: AutoUsePrefer},
		{ID: "skill:quiet", Kind: KindSkill, Name: "quiet", Description: "unrelated", AutoUse: AutoUseSuggest},
		{ID: "skill:off", Kind: KindSkill, Name: "off", Description: "code review", AutoUse: AutoUseOff},
		{ID: "skill:broken", Kind: KindSkill, Name: "broken", Description: "code review", Status: StatusFailed, AutoUse: AutoUseSuggest},
		{ID: "tool:builtin", Kind: KindTool, Name: "read", Description: "code review", AutoUse: AutoUseSuggest},
		{ID: "skill:declined", Kind: KindSkill, Name: "declined", Description: "code review", AutoUse: AutoUseSuggest, NegativeTriggers: []string{"prefix"}},
	}
	pool, dropped := semanticPool("capture request prefix", entries)
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0 under the cap", dropped)
	}
	got := poolIDs(pool)
	want := []string{"skill:review", "skill:quiet"}
	if !slices.Equal(got, want) {
		t.Fatalf("pool = %v, want %v (structural filters only, strongest policy first)", got, want)
	}
}

// Whitespace tokenization made the pool depend on the script the user writes
// in: a Han-script request is one token no description contains, so every
// candidate was filtered out before the router saw it. The same catalog must
// now offer the same candidates whatever the request is written in.
func TestSemanticPoolDoesNotDependOnTheRequestsScript(t *testing.T) {
	entries := []Entry{
		{ID: "skill:explore", Kind: KindSkill, Name: "explore", Source: "builtin", Description: "inspect architecture", AutoUse: AutoUseSuggest},
		{ID: "skill:custom", Kind: KindSkill, Name: "custom", Source: "project", Description: "unrelated custom workflow", AutoUse: AutoUseSuggest},
	}
	zh, _ := semanticPool("帮我梳理一下这个功能", entries)
	en, _ := semanticPool("help me map out this feature", entries)
	if !slices.Equal(poolIDs(zh), poolIDs(en)) {
		t.Fatalf("Han-script pool %v differs from Latin-script pool %v", poolIDs(zh), poolIDs(en))
	}
	if len(poolIDs(zh)) != 2 {
		t.Fatalf("pool = %v, want both eligible entries", poolIDs(zh))
	}
}

// A pool the model judged in full and a slice of one are different answers, so
// the cap has to say how much it kept back — and drop the weakest declared
// policy rather than the alphabetically last ID.
func TestSemanticPoolCapDropsWeakestPolicyAndReportsIt(t *testing.T) {
	var entries []Entry
	for i := range semanticMaxCandidates + 5 {
		e := Entry{ID: fmt.Sprintf("skill:s%03d", i), Kind: KindSkill, Name: "s", Description: "d", AutoUse: AutoUseSuggest}
		if i >= semanticMaxCandidates {
			e.AutoUse = AutoUseRequire // last by ID, strongest by policy
		}
		entries = append(entries, e)
	}
	pool, dropped := semanticPool("anything", entries)
	if len(pool) != semanticMaxCandidates || dropped != 5 {
		t.Fatalf("pool %d dropped %d, want %d and 5", len(pool), dropped, semanticMaxCandidates)
	}
	for _, e := range pool[:5] {
		if e.AutoUse != AutoUseRequire {
			t.Fatalf("cap kept %s (%s) over a required entry", e.ID, e.AutoUse)
		}
	}
}

func poolIDs(pool []Entry) []string {
	out := make([]string, 0, len(pool))
	for _, e := range pool {
		out = append(out, e.ID)
	}
	return out
}
