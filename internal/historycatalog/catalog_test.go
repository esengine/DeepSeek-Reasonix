package historycatalog

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func saveMessages(t *testing.T, path string, messages ...provider.Message) {
	t.Helper()
	session := agent.NewSession("")
	for _, message := range messages {
		session.Add(message)
	}
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileAndSearchFTSWithoutStoredBody(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "decision.jsonl")
	saveMessages(t, path,
		provider.Message{Role: provider.RoleUser, Content: "Should we use vector embeddings?"},
		provider.Message{Role: provider.RoleAssistant, Content: "Keep lightweight BM25 retrieval for history."})
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "history.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	registered := Root{Path: root, Source: "project", Scope: "project", WorkspaceRoot: "/workspace"}
	if err := catalog.ReconcileRoot(ctx, registered); err != nil {
		t.Fatal(err)
	}
	result, err := catalog.Search(ctx, SearchRequest{Query: "lightweight BM25", Scope: "project", WorkspaceRoot: "/workspace", Kinds: []string{"assistant_text"}, Roots: []string{root}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].SessionPath != path || result.Items[0].MessageIndex != 1 {
		t.Fatalf("result=%#v", result)
	}
	var storedTerms string
	if err := catalog.db.QueryRow(`SELECT terms FROM history_fts WHERE rowid=?`, result.Items[0].RowID).Scan(&storedTerms); err == nil {
		t.Fatalf("contentless FTS unexpectedly returned stored terms %q", storedTerms)
	}
}

func TestRewriteRemovesOldTerms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "rewrite.jsonl")
	saveMessages(t, path, provider.Message{Role: provider.RoleUser, Content: "obsolete unicorn marker"})
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "history.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	target := Root{Path: root, Source: "project", Scope: "project", WorkspaceRoot: root}
	if err := catalog.ReconcileRoot(ctx, target); err != nil {
		t.Fatal(err)
	}
	session, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	session.Rewrite([]provider.Message{{Role: provider.RoleUser, Content: "replacement phoenix marker"}}, "test")
	if err := session.SaveRewrite(path); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReconcileRoot(ctx, target); err != nil {
		t.Fatal(err)
	}
	oldResult, err := catalog.Search(ctx, SearchRequest{Query: "unicorn", Kinds: []string{"user_text"}, Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(oldResult.Items) != 0 {
		t.Fatalf("stale terms survived rewrite: %#v", oldResult.Items)
	}
	newResult, err := catalog.Search(ctx, SearchRequest{Query: "phoenix", Kinds: []string{"user_text"}, Roots: []string{root}})
	if err != nil || len(newResult.Items) != 1 {
		t.Fatalf("newResult=%#v err=%v", newResult, err)
	}
}

func TestAppendIndexesOnlyDisplayTail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "append.jsonl")
	saveMessages(t, path, provider.Message{Role: provider.RoleUser, Content: "stable prefix marker"})
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "history.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	target := Root{Path: root, Source: "project", Scope: "project", WorkspaceRoot: root}
	if err := catalog.ReconcileRoot(ctx, target); err != nil {
		t.Fatal(err)
	}
	var prefixRowID int64
	if err := catalog.db.QueryRow(`SELECT id FROM history_documents WHERE source_path=? AND message_index=0`, path).Scan(&prefixRowID); err != nil {
		t.Fatal(err)
	}
	session, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "new append-only phoenix"})
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := catalog.indexPath(ctx, target, path, 0, 1); err != nil {
		t.Fatal(err)
	}
	var unchangedRowID int64
	if err := catalog.db.QueryRow(`SELECT id FROM history_documents WHERE source_path=? AND message_index=0`, path).Scan(&unchangedRowID); err != nil {
		t.Fatal(err)
	}
	if unchangedRowID != prefixRowID {
		t.Fatalf("prefix row was rebuilt: before=%d after=%d", prefixRowID, unchangedRowID)
	}
	result, err := catalog.Search(ctx, SearchRequest{Query: "phoenix", Roots: []string{root}})
	if err != nil || len(result.Items) != 1 || result.Items[0].MessageIndex != 1 {
		t.Fatalf("appended result=%#v err=%v", result, err)
	}
}

func TestSearchKeysetContinuesPastCatalogLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "many.jsonl")
	messages := make([]provider.Message, 0, 5)
	for range 5 {
		messages = append(messages, provider.Message{Role: provider.RoleUser, Content: "shared pagination marker"})
	}
	saveMessages(t, path, messages...)
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "history.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileRoot(ctx, Root{Path: root, Source: "project", Scope: "project", WorkspaceRoot: root}); err != nil {
		t.Fatal(err)
	}
	request := SearchRequest{Query: "pagination", Kinds: []string{"user_text"}, Roots: []string{root}, Limit: 2}
	seen := map[int]bool{}
	for {
		result, err := catalog.Search(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Items) == 0 {
			break
		}
		for _, item := range result.Items {
			if seen[item.MessageIndex] {
				t.Fatalf("message %d repeated across keyset pages", item.MessageIndex)
			}
			seen[item.MessageIndex] = true
		}
		last := result.Items[len(result.Items)-1]
		request.After = &SearchCursor{Rank: last.Rank, SessionPath: last.SessionPath, MessageIndex: last.MessageIndex,
			PartIndex: last.PartIndex, RowID: last.RowID}
	}
	if len(seen) != len(messages) {
		t.Fatalf("indexed messages=%d, want %d", len(seen), len(messages))
	}
}

func TestToolOutputIsIndexedAndSearchableByExplicitKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "tools.jsonl")
	saveMessages(t, path,
		provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "bash", Arguments: `{"cmd":"echo hello"}`}}},
		provider.Message{Role: provider.RoleTool, ToolCallID: "1", Name: "bash", Content: "zephyroutputtokenxyz hello"},
		provider.Message{Role: provider.RoleTool, ToolCallID: "2", Name: "bash", Content: "error: permission denied on quasarerrortokenabc"},
	)
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "history.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileRoot(ctx, Root{Path: root, Source: "project", Scope: "project", WorkspaceRoot: root}); err != nil {
		t.Fatal(err)
	}

	defaultKinds := []string{"user_text", "assistant_text", "tool_input", "tool_error"}
	defaultResult, err := catalog.Search(ctx, SearchRequest{Query: "zephyroutputtokenxyz", Kinds: defaultKinds, Roots: []string{root}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultResult.Items) != 0 {
		t.Fatalf("default kinds unexpectedly returned tool_output hits: %#v", defaultResult.Items)
	}

	outputResult, err := catalog.Search(ctx, SearchRequest{Query: "zephyroutputtokenxyz", Kinds: []string{"tool_output"}, Roots: []string{root}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(outputResult.Items) != 1 || outputResult.Items[0].Kind != "tool_output" {
		t.Fatalf("tool_output result=%#v", outputResult.Items)
	}

	errorResult, err := catalog.Search(ctx, SearchRequest{Query: "quasarerrortokenabc", Kinds: []string{"tool_error"}, Roots: []string{root}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(errorResult.Items) != 1 || errorResult.Items[0].Kind != "tool_error" {
		t.Fatalf("tool_error result=%#v", errorResult.Items)
	}
}

func TestTokenizerVersionMismatchClearsMixedProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "old-tokenizer.jsonl")
	databasePath := filepath.Join(t.TempDir(), "history.sqlite")
	saveMessages(t, path, provider.Message{Role: provider.RoleUser, Content: "tokenizer migration marker"})
	catalog, err := Open(ctx, Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReconcileRoot(ctx, Root{Path: root, Source: "project", Scope: "project", WorkspaceRoot: root}); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.db.Exec(`UPDATE history_state SET tokenizer_version=?`, TokenizerVersion+1); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })
	result, err := reopened.Search(ctx, SearchRequest{Query: "migration", Roots: []string{root}})
	if err != nil || len(result.Items) != 0 {
		t.Fatalf("mixed tokenizer rows survived: result=%#v err=%v", result, err)
	}
}
