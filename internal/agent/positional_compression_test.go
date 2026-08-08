package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestSummarizeFromPreservesLocalOnlyOutsideModelAndArchive(t *testing.T) {
	archiveDir := t.TempDir()
	local := provider.Message{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
		LocalOnly: true, Content: "visible interrupted output", ReasoningContent: "private interrupted reasoning",
		InterruptedTurn: &provider.InterruptedTurnRecovery{Pending: true, InterruptedTools: []string{"bash"}},
	}
	prov := &fakeProvider{reply: "later summary"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		local,
		{Role: provider.RoleAssistant, Content: "safe answer " + strings.Repeat("work ", 200)},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{ArchiveDir: archiveDir}, event.Discard)
	before := sess.Snapshot()

	if err := a.SummarizeFrom(context.Background(), 1); err != nil {
		t.Fatalf("SummarizeFrom: %v", err)
	}
	if !reflect.DeepEqual(sess.Snapshot(), before) {
		t.Fatalf("canonical transcript changed: before=%+v after=%+v", before, sess.Snapshot())
	}
	if len(a.compactionState.Projection.Messages) == 0 {
		t.Fatal("expected summarize-from projection")
	}
	assertLocalOnlyAbsentFromSummaryAndArchive(t, prov, archiveDir, local)
}

func TestSummarizeUpToPreservesLocalOnlyOutsideModelAndArchive(t *testing.T) {
	archiveDir := t.TempDir()
	local := provider.Message{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
		LocalOnly: true, Content: "visible earlier interruption", ReasoningContent: "private earlier reasoning",
		InterruptedTurn: &provider.InterruptedTurnRecovery{Pending: true, InterruptedTools: []string{"read_file"}},
	}
	prov := &fakeProvider{reply: "earlier summary"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "old task"},
		local,
		{Role: provider.RoleAssistant, Content: "old answer " + strings.Repeat("work ", 200)},
		{Role: provider.RoleUser, Content: "new task"},
		{Role: provider.RoleAssistant, Content: "new answer"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{ArchiveDir: archiveDir}, event.Discard)
	before := sess.Snapshot()

	if err := a.SummarizeUpTo(context.Background(), 4); err != nil {
		t.Fatalf("SummarizeUpTo: %v", err)
	}
	if !reflect.DeepEqual(sess.Snapshot(), before) {
		t.Fatalf("canonical transcript changed: before=%+v after=%+v", before, sess.Snapshot())
	}
	if len(a.compactionState.Projection.Messages) == 0 {
		t.Fatal("expected summarize-up-to projection")
	}
	assertLocalOnlyAbsentFromSummaryAndArchive(t, prov, archiveDir, local)
}

func TestSummarizeAtProjectionBoundaryReportsFoldedAnchor(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "old boundary"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old work ", 200)},
		{Role: provider.RoleUser, Content: "retained boundary"},
		{Role: provider.RoleAssistant, Content: "retained answer"},
	}}
	before := sess.Snapshot()
	a := New(&fakeProvider{reply: "old work summarized"}, tool.NewRegistry(), sess, Options{}, event.Discard)

	if err := a.SummarizeUpTo(context.Background(), 3); err != nil {
		t.Fatalf("initial SummarizeUpTo: %v", err)
	}
	version := a.compactionState.Projection.ProjectionVersion
	err := a.SummarizeFrom(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "no longer present in the model context") {
		t.Fatalf("folded-boundary error = %v", err)
	}
	if a.compactionState.Projection.ProjectionVersion != version {
		t.Fatal("folded-boundary retry replaced the projection")
	}
	if !reflect.DeepEqual(sess.Snapshot(), before) {
		t.Fatal("folded-boundary retry changed canonical history")
	}
}

func TestSummarizeAtProjectionBoundaryReportsNoSavings(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "tiny"},
		{Role: provider.RoleUser, Content: "keep boundary"},
	}}
	a := New(&fakeProvider{reply: strings.Repeat("long summary ", 30)}, tool.NewRegistry(), sess, Options{}, event.Discard)

	err := a.SummarizeUpTo(context.Background(), 2)
	if err == nil || !strings.Contains(err.Error(), "compressed context would not be smaller") {
		t.Fatalf("no-savings error = %v", err)
	}
	if len(a.compactionState.Projection.Messages) != 0 {
		t.Fatal("no-savings positional compression installed a projection")
	}
}

func assertLocalOnlyAbsentFromSummaryAndArchive(t *testing.T, prov *fakeProvider, archiveDir string, local provider.Message) {
	t.Helper()
	if len(prov.got) < 2 || strings.Contains(prov.got[1].Content, local.Content) || strings.Contains(prov.got[1].Content, local.ReasoningContent) {
		t.Fatalf("local-only output leaked into summarizer prompt: %+v", prov.got)
	}
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("ReadDir archive: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("archive entries = %d, want 1", len(entries))
	}
	b, err := os.ReadFile(filepath.Join(archiveDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile archive: %v", err)
	}
	if strings.Contains(string(b), local.Content) || strings.Contains(string(b), local.ReasoningContent) {
		t.Fatalf("local-only output leaked into archive: %s", b)
	}
}
