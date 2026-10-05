package agent

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

// coverageTools answers from the shipped registry rather than a copy of it, so
// this test reads the contracts the agent reads.
var coverageRegistry = sync.OnceValue(builtinToolRegistry)

func coverageTools(name string) evidence.ToolFacts {
	t, ok := coverageRegistry().Get(name)
	if !ok {
		return readOnlyFacts
	}
	return toolFacts(t)
}

func coverageRegion() []provider.Message {
	return []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "w1", Name: "write_file", Arguments: `{"path":"internal/parser/lexer.go"}`},
			{ID: "w2", Name: "edit_file", Arguments: `{"path":"internal/parser/reader.go","old_string":"a","new_string":"b"}`},
			{ID: "r1", Name: "read_file", Arguments: `{"path":"internal/unrelated/notes.md"}`},
			{ID: "b1", Name: "bash", Arguments: `{"command":"go test ./internal/parser/ -run TestLexer"}`},
		}},
		{Role: provider.RoleTool, ToolCallID: "w1", Name: "write_file", Content: "wrote"},
		{Role: provider.RoleTool, ToolCallID: "w2", Name: "edit_file", Content: "applied"},
		{Role: provider.RoleTool, ToolCallID: "r1", Name: "read_file", Content: "contents"},
		{Role: provider.RoleTool, ToolCallID: "b1", Name: "bash", Content: "error: FAIL TestLexer"},
	}
}

// The facts a fold must carry are derived from what it did, not from its text:
// changes made and commands that failed. Reads stay out — the workspace still
// holds what they found, and demanding them back would push the summarizer
// toward listing paths to pass a check.
func TestFoldFactsAreChangesAndFailures(t *testing.T) {
	cov := foldFacts(coverageRegion(), coverageTools)
	if len(cov.Mutations) != 2 {
		t.Fatalf("mutations = %v, want the two written files", cov.Mutations)
	}
	for _, unwanted := range cov.Mutations {
		if strings.Contains(unwanted, "notes.md") {
			t.Errorf("a read was counted as a change: %v", cov.Mutations)
		}
	}
	if len(cov.Failures) != 1 || cov.Failures[0] != "go test" {
		t.Fatalf("failures = %v, want the failed command reduced to its signature", cov.Failures)
	}
}

// A digest that names the file it changed has carried the change, whether it
// wrote the full path or the bare name.
func TestCoverageAcceptsFullPathOrBareName(t *testing.T) {
	region := coverageRegion()
	full := measureFoldCoverage(region, coverageTools, "changed internal/parser/lexer.go and internal/parser/reader.go; go test failed")
	if full.Missing() != 0 {
		t.Fatalf("full paths not credited: %+v", full)
	}
	bare := measureFoldCoverage(region, coverageTools, "split lexer.go, then reader.go picked it up; go test still red")
	if bare.Missing() != 0 {
		t.Fatalf("bare file names not credited: %+v", bare)
	}
}

// The severity line is the mechanism's own: a forgotten change makes the agent
// wrong, a forgotten failure only makes it slow.
func TestCoverageSeparatesChangesFromFailures(t *testing.T) {
	region := coverageRegion()
	lostFailure := measureFoldCoverage(region, coverageTools, "changed lexer.go and reader.go")
	if lostFailure.LostAChange() {
		t.Errorf("a digest carrying every change must not read as having lost one: %+v", lostFailure)
	}
	if lostFailure.Missing() != 1 {
		t.Errorf("the dropped failure should still be counted: %+v", lostFailure)
	}
	lostChange := measureFoldCoverage(region, coverageTools, "reader.go was touched; go test failed")
	if !lostChange.LostAChange() || lostChange.LostEveryChange() {
		t.Errorf("one missing change should be partial, not total: %+v", lostChange)
	}
	empty := measureFoldCoverage(region, coverageTools, "The user asked for a refactor. Work proceeded.")
	if !empty.LostEveryChange() {
		t.Errorf("a digest naming no change at all must read as broken: %+v", empty)
	}
}

// A fold with nothing to carry cannot fail the check.
func TestCoverageIsSatisfiedByAnEmptyFold(t *testing.T) {
	cov := measureFoldCoverage([]provider.Message{
		{Role: provider.RoleUser, Content: "what does this project do?"},
		{Role: provider.RoleAssistant, Content: "It is a coding agent."},
	}, coverageTools, "the user asked what the project does")
	if cov.Required() != 0 || cov.LostAChange() || cov.LostEveryChange() {
		t.Fatalf("a fold that changed nothing owes nothing: %+v", cov)
	}
}

// The card that shows a fold's quality can only be as honest as the event
// behind it. Coverage is measured during the fold and would read as a clean
// zero at every frontend if it were not carried out with the result.
func TestCompactionDoneCarriesWhatTheDigestKept(t *testing.T) {
	sess := foldableSessionOverForce(40)
	// Early enough to land in the fold rather than the verbatim tail.
	sess.Messages = slices.Insert(sess.Messages, 2,
		provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "w1", Name: "write_file", Arguments: `{"path":"internal/parser/lexer.go"}`},
		}},
		provider.Message{Role: provider.RoleTool, ToolCallID: "w1", Name: "write_file", Content: "wrote"})

	var done []event.Compaction
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.CompactionDone {
			done = append(done, e.Compaction)
		}
	})
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", writesPaths: true})
	a := New(&fakeProvider{reply: "## Files & code\n- internal/parser/lexer.go rewritten"}, reg, sess,
		Options{ContextWindow: 60_000, CompactRatio: 0.5, RecentKeep: 2, ArchiveDir: testenv.TempDir(t)}, sink)

	if _, _, err := a.window().compactToProjection(context.Background(), CompactionTriggerManual, "", compactionScope{ignoreThreshold: true, ignoreEconomics: true}, false); err != nil {
		t.Fatalf("compactToProjection: %v", err)
	}
	if len(done) != 1 {
		t.Fatalf("CompactionDone events = %d, want 1", len(done))
	}
	got := done[0]
	if got.CoverageRequired == 0 {
		t.Fatalf("the fold rewrote a file but the event reports no coverage: %+v", got)
	}
	if got.CoverageMissing != 0 {
		t.Fatalf("the digest named the file it rewrote; missing = %d", got.CoverageMissing)
	}
	if got.SourceTokens <= got.ProjectionTokens || got.ProjectionTokens == 0 {
		t.Fatalf("sizes = %d → %d, want a real shrink", got.SourceTokens, got.ProjectionTokens)
	}
}
