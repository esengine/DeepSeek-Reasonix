package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func pruneFixture(toolContent string) *Session {
	return &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "read_file", Content: toolContent},
		{Role: provider.RoleAssistant, Content: "step done"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
}

func TestPruneStaleToolResults(t *testing.T) {
	big := strings.Repeat("x", 5000)
	sess := pruneFixture(big)
	dir := t.TempDir()
	a := New(nil, tool.NewRegistry(), sess, Options{ContextWindow: 1000, RecentKeep: 2, ArchiveDir: dir}, event.Discard)

	st, err := a.PruneStaleToolResults()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if st.Results != 1 {
		t.Fatalf("Results = %d, want 1", st.Results)
	}
	if st.SavedChars < 4000 {
		t.Errorf("SavedChars = %d, want > 4000", st.SavedChars)
	}
	msgs := sess.Snapshot()
	if len(msgs) != 7 {
		t.Fatalf("message count changed: %d", len(msgs))
	}
	pruned := msgs[3]
	if !strings.HasPrefix(pruned.Content, prunedMarker) {
		t.Errorf("tool content not elided: %.60q", pruned.Content)
	}
	if pruned.ToolCallID != "1" || pruned.Name != "read_file" || pruned.Role != provider.RoleTool {
		t.Errorf("tool pairing fields changed: %+v", pruned)
	}
	if len(msgs[2].ToolCalls) != 1 || msgs[2].ToolCalls[0].ID != "1" {
		t.Errorf("assistant tool_calls touched: %+v", msgs[2])
	}
	if got := sess.RewriteVersion(); got != 1 {
		t.Errorf("RewriteVersion = %d, want 1", got)
	}
	if st.Archive == "" {
		t.Fatal("no archive written")
	}
	raw, err := os.ReadFile(st.Archive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !strings.Contains(string(raw), big[:64]) {
		t.Error("archive does not contain the original tool output")
	}
	if filepath.Dir(st.Archive) != dir {
		t.Errorf("archive outside dir: %s", st.Archive)
	}

	st2, err := a.PruneStaleToolResults()
	if err != nil {
		t.Fatalf("second prune: %v", err)
	}
	if st2.Results != 0 {
		t.Errorf("second pass pruned %d, want 0 (idempotent)", st2.Results)
	}
	if got := sess.RewriteVersion(); got != 1 {
		t.Errorf("no-op pass bumped RewriteVersion to %d", got)
	}
}

func TestPruneNeverRewritesLocalInterruptedDisplay(t *testing.T) {
	m := provider.Message{
		Role: provider.RoleTool, LocalOnly: true,
		ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
		Content: strings.Repeat("partial output", 1024),
	}
	if shouldMaintainToolResult(m, toolResultSnip) || shouldMaintainToolResult(m, toolResultPrune) {
		t.Fatal("local interrupted display must remain verbatim across tool-result maintenance")
	}
}

func TestSnipStaleToolResults(t *testing.T) {
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, "line")
	}
	big := strings.Join(lines, "\n")
	sess := pruneFixture(big)
	dir := t.TempDir()
	a := New(nil, tool.NewRegistry(), sess, Options{ContextWindow: 1000, RecentKeep: 2, ArchiveDir: dir}, event.Discard)

	st, err := a.SnipStaleToolResults()
	if err != nil {
		t.Fatalf("snip: %v", err)
	}
	if st.Results != 1 {
		t.Fatalf("Results = %d, want 1", st.Results)
	}
	snipped := sess.Snapshot()[3].Content
	if !strings.HasPrefix(snipped, snippedMarker) {
		t.Fatalf("tool content not snipped: %.80q", snipped)
	}
	if !strings.Contains(snipped, "[... ") || !strings.Contains(snipped, "lines omitted") {
		t.Fatalf("snipped content missing omission marker: %.120q", snipped)
	}
	if st.SavedChars <= 0 {
		t.Fatalf("SavedChars = %d, want positive", st.SavedChars)
	}
	if st.Archive == "" {
		t.Fatal("no archive written")
	}

	st2, err := a.SnipStaleToolResults()
	if err != nil {
		t.Fatalf("second snip: %v", err)
	}
	if st2.Results != 0 {
		t.Fatalf("second pass snipped %d, want 0", st2.Results)
	}
}

func TestSnipCanUpgradeToPrune(t *testing.T) {
	big := strings.Join([]string{strings.Repeat("a\n", 800), strings.Repeat("b\n", 800)}, "")
	sess := pruneFixture(big)
	a := New(nil, tool.NewRegistry(), sess, Options{ContextWindow: 1000, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	snipStats, err := a.SnipStaleToolResults()
	if err != nil || snipStats.Results != 1 {
		t.Fatalf("snip st=%+v err=%v, want one result", snipStats, err)
	}
	if pruneStats, err := a.PruneStaleToolResults(); err != nil || pruneStats.Results != 1 {
		t.Fatalf("prune st=%+v err=%v, want one upgraded result", pruneStats, err)
	}
	if got := sess.Snapshot()[3].Content; !strings.HasPrefix(got, prunedMarker) {
		t.Fatalf("snipped result was not upgraded to prune: %.80q", got)
	} else if !strings.Contains(got, snipStats.Archive) {
		t.Fatalf("pruned marker did not preserve original archive path %q: %.120q", snipStats.Archive, got)
	}
}

func TestPruneNoopWithoutWindow(t *testing.T) {
	sess := pruneFixture(strings.Repeat("x", 5000))
	a := New(nil, tool.NewRegistry(), sess, Options{RecentKeep: 2}, event.Discard)
	st, err := a.PruneStaleToolResults()
	if err != nil || st.Results != 0 {
		t.Fatalf("st=%+v err=%v, want no-op", st, err)
	}
}

func TestPruneSkipsSmallResults(t *testing.T) {
	sess := pruneFixture(strings.Repeat("x", 200))
	a := New(nil, tool.NewRegistry(), sess, Options{ContextWindow: 1000, RecentKeep: 2}, event.Discard)
	st, err := a.PruneStaleToolResults()
	if err != nil || st.Results != 0 {
		t.Fatalf("st=%+v err=%v, want small result kept", st, err)
	}
	if got := sess.Snapshot()[3].Content; !strings.HasPrefix(got, "xxx") {
		t.Errorf("small tool result was rewritten: %.40q", got)
	}
}

func TestMaybeCompactPruneAvoidsFold(t *testing.T) {
	prov := &fakeProvider{reply: "summary"}
	sess := pruneFixture(strings.Repeat("x", 5000))
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 1000, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 850})

	if prov.got != nil {
		t.Fatal("summarizer was called although pruning cleared the trigger")
	}
	if got := sess.Snapshot()[3].Content; !strings.HasPrefix(got, prunedMarker) {
		t.Errorf("tool result not pruned: %.60q", got)
	}
}

func TestMaybeCompactSnipsAtSnipRatioWithoutFold(t *testing.T) {
	prov := &fakeProvider{reply: "summary"}
	sess := pruneFixture(strings.Repeat("line\n", 1000))
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 1000, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 650})

	if prov.got != nil {
		t.Fatal("summarizer was called at snip ratio")
	}
	if got := sess.Snapshot()[3].Content; !strings.HasPrefix(got, snippedMarker) {
		t.Errorf("tool result not snipped at snip ratio: %.80q", got)
	}
}

func TestMaybeCompactPruneFallsThroughWhenStillOverThreshold(t *testing.T) {
	prov := &fakeProvider{reply: "summary"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("foldable assistant work\n", 500)},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "read_file", Content: strings.Repeat("x", 1200)},
		{Role: provider.RoleAssistant, Content: "step done"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 10000, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 8900})

	if prov.got == nil {
		t.Fatal("summarizer was not called although pruning still left prompt above compact threshold")
	}
	foundSummary := false
	for _, m := range sess.Snapshot() {
		if strings.Contains(m.Content, summaryTagOpen) {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Fatal("summary compaction did not update the session")
	}
}

func TestMaybeCompactForceRatioStillFolds(t *testing.T) {
	prov := &fakeProvider{reply: "summary"}
	// A big assistant turn in the foldable region (after the pinned task, before the
	// recent tail) survives pruning — only tool results prune — so the forced fold
	// has real content to compact while the task turn stays pinned verbatim.
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("y", 5000)},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "read_file", Content: strings.Repeat("x", 5000)},
		{Role: provider.RoleAssistant, Content: "step done"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 1000, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 950})

	if prov.got == nil {
		t.Fatal("force ratio crossed but summarizer never called")
	}
	if got := sess.Snapshot()[1].Content; got != "task" {
		t.Errorf("first user turn not pinned verbatim: %.40q", got)
	}
	found := false
	for _, m := range sess.Snapshot() {
		if strings.Contains(m.Content, summaryTagOpen) {
			found = true
		}
	}
	if !found {
		t.Error("no compaction summary in session after forced fold")
	}
}

func TestPruneSkipsRecentTail(t *testing.T) {
	old := strings.Repeat("old\n", 1000)
	recent := strings.Repeat("recent\n", 1000)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "old", Name: "read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "old", Name: "read_file", Content: old},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "recent", Name: "read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "recent", Name: "read_file", Content: recent},
	}}
	a := New(nil, tool.NewRegistry(), sess, Options{ContextWindow: 1000, RecentKeep: 3, ArchiveDir: t.TempDir()}, event.Discard)

	st, err := a.PruneStaleToolResults()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if st.Results != 1 {
		t.Fatalf("Results = %d, want only the stale result pruned", st.Results)
	}
	msgs := sess.Snapshot()
	if !strings.HasPrefix(msgs[3].Content, prunedMarker) {
		t.Fatalf("old result was not pruned: %.80q", msgs[3].Content)
	}
	if msgs[6].Content != recent {
		t.Fatalf("recent tail tool result was rewritten")
	}
}

func TestPruneHonorsKeepErrors(t *testing.T) {
	// KeepErrors must carry error/blocked tool results through pruning verbatim;
	// eliding here rewrites Content to the [elided ...] marker, so compact()'s
	// KeepErrors predicate sees only the placeholder and the failure is lost on
	// the next fold.
	for _, prefix := range []string{"error:", "blocked:"} {
		content := prefix + strings.Repeat(" detail", 200)
		sess := pruneFixture(content)
		a := New(nil, tool.NewRegistry(), sess, Options{ContextWindow: 1000, RecentKeep: 2, KeepPolicy: KeepErrors}, event.Discard)

		st, err := a.PruneStaleToolResults()
		if err != nil {
			t.Fatalf("prune (%s): %v", prefix, err)
		}
		if st.Results != 0 {
			t.Errorf("%s: Results = %d, want 0 (KeepErrors preserves error tool results)", prefix, st.Results)
		}
		if got := sess.Snapshot()[3].Content; !strings.HasPrefix(got, prefix) {
			t.Errorf("%s: error tool result was elided: %.60q", prefix, got)
		}
	}
}

func TestPruneElidesErrorsWithoutKeepPolicy(t *testing.T) {
	// Without KeepErrors, a large error tool result prunes like any other — a
	// regression guard for the policy-gated skip.
	content := "error: build failed\n" + strings.Repeat("x", 5000)
	sess := pruneFixture(content)
	a := New(nil, tool.NewRegistry(), sess, Options{ContextWindow: 1000, RecentKeep: 2}, event.Discard)

	st, err := a.PruneStaleToolResults()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if st.Results != 1 {
		t.Errorf("Results = %d, want 1 (no keep policy)", st.Results)
	}
	if got := sess.Snapshot()[3].Content; !strings.HasPrefix(got, prunedMarker) {
		t.Errorf("error tool result not elided without keep policy: %.60q", got)
	}
}

// hintingTool is a read-only tool that advertises a distinctive snip geometry,
// so a test can prove the maintainer honored the tool's own SnipHint rather
// than a name-keyed table or a generic default.
type hintingTool struct {
	name string
	hint tool.SnipHint
}

func (h hintingTool) Name() string                                           { return h.name }
func (hintingTool) Description() string                                      { return "" }
func (hintingTool) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (hintingTool) ReadOnly() bool                                           { return true }
func (hintingTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
func (h hintingTool) SnipHint() tool.SnipHint                                { return h.hint }

func snipFixtureFor(toolName, content string) *Session {
	return &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: toolName, Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "1", Name: toolName, Content: content},
		{Role: provider.RoleAssistant, Content: "step done"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
}

func TestSnipUsesRegisteredToolHint(t *testing.T) {
	// 600 numbered lines; a SnipHinter keeping head=3, tail=2 must yield exactly
	// those boundary lines, which no default geometry would produce.
	var lines []string
	for i := 0; i < 600; i++ {
		lines = append(lines, fmt.Sprintf("L%d", i))
	}
	content := strings.Join(lines, "\n")
	sess := snipFixtureFor("custom_reader", content)

	reg := tool.NewRegistry()
	reg.Add(hintingTool{name: "custom_reader", hint: tool.SnipHint{Head: 3, Tail: 2, HeadChars: 100, TailChars: 100}})
	a := New(nil, reg, sess, Options{ContextWindow: 1000, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	if st, err := a.SnipStaleToolResults(); err != nil || st.Results != 1 {
		t.Fatalf("snip st=%+v err=%v, want one result", st, err)
	}
	got := sess.Snapshot()[3].Content
	if !strings.Contains(got, "showing first 3 lines and last 2 lines") {
		t.Fatalf("snip did not honor the tool's SnipHint geometry: %.120q", got)
	}
	if !strings.Contains(got, "\nL0\nL1\nL2\n") {
		t.Errorf("kept head is not the first 3 lines: %.160q", got)
	}
	if !strings.Contains(got, "\nL598\nL599") {
		t.Errorf("kept tail is not the last 2 lines: %.160q", got)
	}
}

func TestSnipFallsBackByReadOnlyTier(t *testing.T) {
	var lines []string
	for i := 0; i < 600; i++ {
		lines = append(lines, fmt.Sprintf("L%d", i))
	}
	content := strings.Join(lines, "\n")

	// A side-effecting tool with no SnipHint takes the even-split default
	// (head==tail), while a read-only one keeps a longer head than tail.
	cases := []struct {
		name     string
		readOnly bool
		evenEnds bool
	}{
		{"side_effecting", false, true},
		{"read_only", true, false},
	}
	for _, tc := range cases {
		sess := snipFixtureFor(tc.name, content)
		reg := tool.NewRegistry()
		reg.Add(fakeTool{name: tc.name, readOnly: tc.readOnly})
		a := New(nil, reg, sess, Options{ContextWindow: 1000, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

		if st, err := a.SnipStaleToolResults(); err != nil || st.Results != 1 {
			t.Fatalf("%s: snip st=%+v err=%v, want one result", tc.name, st, err)
		}
		got := sess.Snapshot()[3].Content
		want := "showing first 40 lines and last 40 lines"
		if !tc.evenEnds {
			want = "showing first 80 lines and last 12 lines"
		}
		if !strings.Contains(got, want) {
			t.Errorf("%s: fallback geometry wrong, want %q in: %.120q", tc.name, want, got)
		}
	}
}
