package agent

import (
	"context"
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
