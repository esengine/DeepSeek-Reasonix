package history

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func TestSearchRanksSavedSessionHistory(t *testing.T) {
	sessionDir := t.TempDir()
	archiveDir := t.TempDir()

	writeSession(t, filepath.Join(sessionDir, "first.jsonl"), []provider.Message{
		{Role: provider.RoleUser, Content: "We need a cache-first implementation."},
		{Role: provider.RoleAssistant, Content: "Decision: keep the prefix stable and avoid CGO SQLite for Reasonix history retrieval."},
	})
	writeSession(t, filepath.Join(sessionDir, "second.jsonl"), []provider.Message{
		{Role: provider.RoleUser, Content: "Talk about dashboard colors."},
		{Role: provider.RoleAssistant, Content: "No database decision here."},
	})

	searcher := NewSearcher(Options{SessionDir: sessionDir, ArchiveDir: archiveDir})
	hits, err := searcher.Search(context.Background(), SearchRequest{Query: "SQLite CGO cache", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Search() returned no hits")
	}
	if got := filepath.Base(hits[0].SessionPath); got != "first.jsonl" {
		t.Fatalf("top hit path = %q, want first.jsonl", got)
	}
	if hits[0].Kind != KindAssistantText {
		t.Fatalf("top hit kind = %q, want %q", hits[0].Kind, KindAssistantText)
	}
}

func TestSearchGlobalIncludesArchives(t *testing.T) {
	sessionDir := t.TempDir()
	archiveDir := t.TempDir()
	writeSession(t, filepath.Join(archiveDir, "archive.jsonl"), []provider.Message{
		{Role: provider.RoleUser, Content: "Old decision: Obelisk retrieval query runtime stays code-driven."},
	})

	searcher := NewSearcher(Options{SessionDir: sessionDir, ArchiveDir: archiveDir})
	projectHits, err := searcher.Search(context.Background(), SearchRequest{Query: "Obelisk runtime", Scope: "project"})
	if err != nil {
		t.Fatalf("project Search() error = %v", err)
	}
	if len(projectHits) != 0 {
		t.Fatalf("project Search() hits = %d, want 0", len(projectHits))
	}
	globalHits, err := searcher.Search(context.Background(), SearchRequest{Query: "Obelisk runtime", Scope: "global"})
	if err != nil {
		t.Fatalf("global Search() error = %v", err)
	}
	if len(globalHits) != 1 {
		t.Fatalf("global Search() hits = %d, want 1", len(globalHits))
	}
	if globalHits[0].Source != "archive" {
		t.Fatalf("global hit source = %q, want archive", globalHits[0].Source)
	}
}

func TestSearchGlobalIncludesGlobalSessionDir(t *testing.T) {
	projectDir := t.TempDir()
	globalDir := t.TempDir()
	writeSession(t, filepath.Join(projectDir, "project.jsonl"), []provider.Message{
		{Role: provider.RoleUser, Content: "Project-only decision about local UI spacing."},
	})
	globalPath := filepath.Join(globalDir, "global.jsonl")
	writeSession(t, globalPath, []provider.Message{
		{Role: provider.RoleUser, Content: "Global-only decision about synthesis cache reuse."},
	})

	searcher := NewSearcher(Options{SessionDir: projectDir, GlobalSessionDir: globalDir})
	projectHits, err := searcher.Search(context.Background(), SearchRequest{Query: "synthesis cache reuse", Scope: "project"})
	if err != nil {
		t.Fatalf("project Search() error = %v", err)
	}
	if len(projectHits) != 0 {
		t.Fatalf("project Search() hits = %d, want 0", len(projectHits))
	}
	globalHits, err := searcher.Search(context.Background(), SearchRequest{Query: "synthesis cache reuse", Scope: "global"})
	if err != nil {
		t.Fatalf("global Search() error = %v", err)
	}
	if len(globalHits) != 1 {
		t.Fatalf("global Search() hits = %d, want 1", len(globalHits))
	}
	if globalHits[0].Source != "global" || globalHits[0].SessionPath != globalPath {
		t.Fatalf("global hit = %+v, want source=global path=%s", globalHits[0], globalPath)
	}
	if _, err := searcher.Around(context.Background(), AroundRequest{SessionPath: globalPath, MessageIndex: 0}); err != nil {
		t.Fatalf("Around() for global session path failed: %v", err)
	}
}

func TestSearchIndexesToolInputsAndErrors(t *testing.T) {
	sessionDir := t.TempDir()
	writeSession(t, filepath.Join(sessionDir, "tools.jsonl"), []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "bash", Arguments: `{"cmd":"go test ./internal/history"}`}}},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "bash", Content: "error: command exited: exit status 1\nFAIL"},
	})

	searcher := NewSearcher(Options{SessionDir: sessionDir})
	hits, err := searcher.Search(context.Background(), SearchRequest{Query: "go test fail", ToolName: "bash", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("Search() hits = %d, want at least 2", len(hits))
	}
	kinds := map[Kind]bool{}
	for _, hit := range hits {
		kinds[hit.Kind] = true
	}
	if !kinds[KindToolInput] || !kinds[KindToolError] {
		t.Fatalf("hits kinds = %#v, want tool input and tool error", kinds)
	}
}

func TestSearchDropsCommonWordNoise(t *testing.T) {
	sessionDir := t.TempDir()
	writeSession(t, filepath.Join(sessionDir, "rare.jsonl"), []provider.Message{
		{Role: provider.RoleUser, Content: "rareterm common common common"},
	})
	for i := 0; i < 12; i++ {
		writeSession(t, filepath.Join(sessionDir, "common-"+string(rune('a'+i))+".jsonl"), []provider.Message{
			{Role: provider.RoleUser, Content: "common"},
		})
	}

	searcher := NewSearcher(Options{SessionDir: sessionDir})
	hits, err := searcher.Search(context.Background(), SearchRequest{Query: "rareterm common", Limit: 20})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("Search() hits = %d, want only rare hit: %+v", len(hits), hits)
	}
	if got := filepath.Base(hits[0].SessionPath); got != "rare.jsonl" {
		t.Fatalf("hit path = %q, want rare.jsonl", got)
	}
}

func TestSearchSkipsCleanupPending(t *testing.T) {
	sessionDir := t.TempDir()
	visiblePath := filepath.Join(sessionDir, "visible.jsonl")
	pendingPath := filepath.Join(sessionDir, "pending.jsonl")
	writeSession(t, visiblePath, []provider.Message{
		{Role: provider.RoleUser, Content: "ordinary alpha visible decision"},
	})
	writeSession(t, pendingPath, []provider.Message{
		{Role: provider.RoleUser, Content: "hidden alpha cleanup pending secret"},
	})
	if err := agent.MarkCleanupPending(pendingPath, "delete"); err != nil {
		t.Fatal(err)
	}

	searcher := NewSearcher(Options{SessionDir: sessionDir})
	hits, err := searcher.Search(context.Background(), SearchRequest{Query: "alpha", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 1 || hits[0].SessionPath != visiblePath {
		t.Fatalf("Search() hits = %+v, want only visible path %s", hits, visiblePath)
	}
	hits, err = searcher.Search(context.Background(), SearchRequest{Query: "cleanup pending secret", Limit: 5})
	if err != nil {
		t.Fatalf("Search() pending-only query error = %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("Search() pending-only hits = %+v, want none", hits)
	}
}

func TestAroundRejectsCleanupPending(t *testing.T) {
	sessionDir := t.TempDir()
	path := filepath.Join(sessionDir, "pending.jsonl")
	writeSession(t, path, []provider.Message{{Role: provider.RoleUser, Content: "pending secret"}})
	if err := agent.MarkCleanupPending(path, "delete"); err != nil {
		t.Fatal(err)
	}

	searcher := NewSearcher(Options{SessionDir: sessionDir})
	if _, err := searcher.Around(context.Background(), AroundRequest{SessionPath: path, MessageIndex: 0}); err == nil {
		t.Fatal("Around() cleanup-pending error = nil, want rejection")
	}
}

func TestHistorySkipsSubagentsOwnedByCleanupPendingParent(t *testing.T) {
	sessionDir := t.TempDir()
	parentPath := filepath.Join(sessionDir, "parent.jsonl")
	writeSession(t, parentPath, []provider.Message{{Role: provider.RoleUser, Content: "parent prompt"}})
	visibleParentPath := filepath.Join(sessionDir, "visible-parent.jsonl")
	writeSession(t, visibleParentPath, []provider.Message{{Role: provider.RoleUser, Content: "visible parent prompt"}})

	pendingSubagentPath := writeSubagentSession(t, sessionDir, "sa_pending", agent.BranchID(parentPath), "subagent hidden orchid result")
	visibleSubagentPath := writeSubagentSession(t, sessionDir, "sa_visible", agent.BranchID(visibleParentPath), "subagent visible orchid result")
	if err := agent.MarkCleanupPending(parentPath, "delete"); err != nil {
		t.Fatal(err)
	}

	searcher := NewSearcher(Options{SessionDir: sessionDir})
	hits, err := searcher.Search(context.Background(), SearchRequest{Query: "orchid result", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 1 || hits[0].SessionPath != visibleSubagentPath {
		t.Fatalf("Search() hits = %+v, want only visible subagent %s", hits, visibleSubagentPath)
	}
	if _, err := searcher.Around(context.Background(), AroundRequest{SessionPath: pendingSubagentPath, MessageIndex: 0}); err == nil {
		t.Fatal("Around() cleanup-pending parent subagent error = nil, want rejection")
	}
	if _, err := searcher.Around(context.Background(), AroundRequest{SessionPath: visibleSubagentPath, MessageIndex: 0}); err != nil {
		t.Fatalf("Around() visible subagent error = %v", err)
	}
}

func TestAroundRequiresPathUnderHistoryRoots(t *testing.T) {
	sessionDir := t.TempDir()
	outside := t.TempDir()
	path := filepath.Join(outside, "outside.jsonl")
	writeSession(t, path, []provider.Message{{Role: provider.RoleUser, Content: "secret"}})

	searcher := NewSearcher(Options{SessionDir: sessionDir})
	if _, err := searcher.Around(context.Background(), AroundRequest{SessionPath: path, MessageIndex: 0}); err == nil {
		t.Fatal("Around() error = nil, want path confinement error")
	}
}

func TestAroundRendersNearbyMessages(t *testing.T) {
	sessionDir := t.TempDir()
	path := filepath.Join(sessionDir, "nearby.jsonl")
	writeSession(t, path, []provider.Message{
		{Role: provider.RoleUser, Content: "first"},
		{Role: provider.RoleAssistant, Content: "second"},
		{Role: provider.RoleUser, Content: "third"},
	})

	searcher := NewSearcher(Options{SessionDir: sessionDir})
	msgs, err := searcher.Around(context.Background(), AroundRequest{SessionPath: path, MessageIndex: 1, Before: 1, After: 1})
	if err != nil {
		t.Fatalf("Around() error = %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("Around() returned %d messages, want 3", len(msgs))
	}
	joined := msgs[0].Text + "\n" + msgs[1].Text + "\n" + msgs[2].Text
	for _, want := range []string{"[0 user]", "[1 assistant]", "[2 user]"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("Around() output missing %q:\n%s", want, joined)
		}
	}
}

func writeSession(t *testing.T, path string, msgs []provider.Message) {
	t.Helper()
	sess := agent.NewSession("")
	for _, msg := range msgs {
		sess.Add(msg)
	}
	if err := sess.Save(path); err != nil {
		t.Fatalf("Save(%s) error = %v", path, err)
	}
}

func writeSubagentSession(t *testing.T, sessionDir, ref, parentSession, content string) string {
	t.Helper()
	dir := filepath.Join(sessionDir, "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ref+".jsonl")
	writeSession(t, path, []provider.Message{{Role: provider.RoleUser, Content: content}})
	meta := agent.SubagentMeta{Ref: ref, ParentSession: parentSession}
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ref+".meta.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
