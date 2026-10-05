package agent

import (
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

func indexRegion() []provider.Message {
	return []provider.Message{
		{Role: provider.RoleUser, Content: "数据库迁移的约束是不能停机，而且必须可回滚"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "r1", Name: "read_file", Arguments: `{"path":"internal/parser/lexer.go"}`},
			{ID: "g1", Name: "grep", Arguments: `{"pattern":"tokenize"}`},
			{ID: "b1", Name: "bash", Arguments: `{"command":"go build ./..."}`},
			{ID: "b2", Name: "bash", Arguments: `{"command":"go test ./internal/parser/"}`},
			{ID: "w1", Name: "write_file", Arguments: `{"path":"internal/parser/lexer.go"}`},
		}},
		{Role: provider.RoleTool, ToolCallID: "r1", Name: "read_file", Content: "contents"},
		{Role: provider.RoleTool, ToolCallID: "g1", Name: "grep", Content: "3 matches"},
		{Role: provider.RoleTool, ToolCallID: "b1", Name: "bash", Content: "ok"},
		{Role: provider.RoleTool, ToolCallID: "b2", Name: "bash", Content: "error: FAIL"},
		{Role: provider.RoleTool, ToolCallID: "w1", Name: "write_file", Content: "wrote"},
	}
}

func identityOrigin(i int) int { return i }

// Index budgets are token budgets, so the lines are costed the way the window
// is: an uncalibrated agent carries the ~4 chars per token fallback.
func indexAgent() *Agent { return &Agent{agentConfig: agentConfig{contextWindow: 128_000}} }

// The budget is a token share of the window. Costing its lines in characters
// spent it four times over, leaving the address list a quarter of the size it
// was budgeted for — and the fold's only addresses along with it.
func TestFoldIndexBudgetIsSpentInTokens(t *testing.T) {
	a := indexAgent()
	entries := make([]foldIndexEntry, 40)
	for i := range entries {
		entries[i] = foldIndexEntry{Canonical: i, Kind: "read_file", Subject: "internal/runtime/agent/compact_index.go", rank: rankRead}
	}
	digest := a.window().attachFoldIndex("## Goal\nship it", "", entries)
	if kept := strings.Count(digest, "read_file"); kept < len(entries) {
		t.Fatalf("index kept %d of %d entries under a %d-token budget", kept, len(entries), a.window().foldIndexBudget())
	}
}

// The index carries what the digest does not: reads, searches, and user turns
// the retention budget could not hold. Changes are the digest's contract, so
// indexing them again would pay for the same fact twice.
func TestFoldIndexCoversWhatTheDigestDoesNot(t *testing.T) {
	entries := buildFoldIndex(indexRegion(), make([]bool, 7), coverageTools, identityOrigin)
	rendered := indexAgent().window().renderFoldIndex(entries, 4000)
	for _, want := range []string{"read_file", "grep", "go build", "#0"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("index missing %q:\n%s", want, rendered)
		}
	}
	// A change and a failed command are what the digest is on the hook for, so
	// neither belongs here — the two halves must not both pay for one fact.
	if strings.Contains(rendered, "write_file") {
		t.Errorf("a change was indexed as well as digested:\n%s", rendered)
	}
	if strings.Contains(rendered, "go test") {
		t.Errorf("a failed command was indexed as well as digested:\n%s", rendered)
	}
	if !strings.Contains(rendered, "不能停机") {
		t.Errorf("the folded user turn was not indexed:\n%s", rendered)
	}
}

// A user turn held verbatim by the keep policy is still in the projection, so
// an index line for it would be a second copy of the same words.
func TestFoldIndexSkipsUserTurnsHeldVerbatim(t *testing.T) {
	kept := make([]bool, 7)
	kept[0] = true
	rendered := indexAgent().window().renderFoldIndex(buildFoldIndex(indexRegion(), kept, coverageTools, identityOrigin), 4000)
	if strings.Contains(rendered, "不能停机") {
		t.Errorf("a verbatim-kept turn was indexed anyway:\n%s", rendered)
	}
}

// When the budget binds, a user turn nobody can re-derive outranks a read
// whose file is still on disk.
func TestFoldIndexBudgetKeepsTheIrreplaceableFirst(t *testing.T) {
	entries := buildFoldIndex(indexRegion(), make([]bool, 7), coverageTools, identityOrigin)
	rendered := indexAgent().window().renderFoldIndex(entries, 40)
	if rendered == "" {
		t.Fatal("a small budget should still hold the top-ranked entry")
	}
	if !strings.Contains(rendered, "不能停机") {
		t.Errorf("the budget dropped the user turn before the reads:\n%s", rendered)
	}
	if strings.Contains(rendered, "read_file") {
		t.Errorf("a read survived a budget that could not hold everything:\n%s", rendered)
	}
}

// The index round-trips out of a digest and back, so a later fold can
// re-summarize the prose without asking a model to rewrite host-written lines.
func TestFoldIndexSplitsAndMergesAcrossFolds(t *testing.T) {
	first := indexAgent().window().renderFoldIndex(buildFoldIndex(indexRegion(), make([]bool, 7), coverageTools, identityOrigin), 4000)
	digest := "## Goal\nship the parser\n\n" + first

	prose, index := splitFoldIndex(digest)
	if strings.Contains(prose, indexSectionHeading) || !strings.Contains(prose, "ship the parser") {
		t.Fatalf("prose and index did not separate:\n%s", prose)
	}
	if index != first {
		t.Fatalf("index did not round-trip:\n%s", index)
	}

	fresh := indexAgent().window().renderFoldIndex([]foldIndexEntry{
		{Canonical: 40, Kind: "read_file", Subject: "internal/other.go", rank: rankRead},
	}, 4000)
	merged := indexAgent().window().mergeFoldIndex(index, fresh, 4000)
	if !strings.Contains(merged, "internal/other.go") || !strings.Contains(merged, "不能停机") {
		t.Fatalf("merge lost one side:\n%s", merged)
	}
	if strings.Count(merged, indexSectionHeading) != 1 {
		t.Fatalf("merge produced more than one section:\n%s", merged)
	}
}

// A merged index that outgrows its budget drops from the oldest and says so,
// because an entry that survived more folds is the one furthest out of reach.
func TestFoldIndexMergeTrimsOldestAndSaysSo(t *testing.T) {
	var old []foldIndexEntry
	for i := range 40 {
		old = append(old, foldIndexEntry{Canonical: i, Kind: "read_file", Subject: "internal/pkg/file.go", rank: rankRead})
	}
	previous := indexAgent().window().renderFoldIndex(old, 8000)
	fresh := indexAgent().window().renderFoldIndex([]foldIndexEntry{
		{Canonical: 99, Kind: "read_file", Subject: "internal/newest.go", rank: rankRead},
	}, 8000)

	merged := indexAgent().window().mergeFoldIndex(previous, fresh, 200)
	if !strings.Contains(merged, "internal/newest.go") {
		t.Fatalf("trimming dropped the newest entry:\n%s", merged)
	}
	if !strings.Contains(merged, "older entries dropped") {
		t.Fatalf("the trim was silent:\n%s", merged)
	}
}
