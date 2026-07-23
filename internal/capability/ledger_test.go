package capability

import "testing"

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

func TestSemanticPoolRequiresLexicalOverlap(t *testing.T) {
	entries := []Entry{
		{ID: "skill:review", Kind: KindSkill, Name: "review", Description: "code review", AutoUse: AutoUsePrefer},
		{ID: "skill:quiet", Kind: KindSkill, Name: "quiet", Description: "unrelated", AutoUse: AutoUseSuggest},
	}
	pool := semanticPool("please review this change", entries)
	if len(pool) != 1 || pool[0].ID != "skill:review" {
		t.Fatalf("pool = %+v, want only review", pool)
	}
	if pool2 := semanticPool("capture request prefix", entries); len(pool2) != 0 {
		t.Fatalf("unrelated text should not open semantic pool: %+v", pool2)
	}
}

func TestSemanticPoolAllowsBoundedChineseBuiltinFallback(t *testing.T) {
	entries := []Entry{
		{ID: "skill:explore", Kind: KindSkill, Name: "explore", Source: "builtin", Description: "inspect architecture", AutoUse: AutoUseSuggest},
		{ID: "skill:custom", Kind: KindSkill, Name: "custom", Source: "project", Description: "unrelated custom workflow", AutoUse: AutoUseSuggest},
	}
	pool := semanticPool("帮我梳理一下这个功能", entries)
	if len(pool) != 1 || pool[0].ID != "skill:explore" {
		t.Fatalf("Chinese fallback pool = %+v, want only the bounded built-in candidate", pool)
	}
}
