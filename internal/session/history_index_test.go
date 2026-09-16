package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/projectiondb"
	"reasonix/internal/provider"
)

func searchHistoryReady(t *testing.T, query *Query, ref SessionRef, text, cursor string, limit int) SearchHistoryPage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		page, err := query.SearchHistory(t.Context(), ref, text, cursor, limit)
		if err != nil {
			t.Fatal(err)
		}
		if page.Status == "ready" {
			return page
		}
		if page.Status != "preparing" || time.Now().After(deadline) {
			t.Fatalf("search preparation = %+v", page)
		}
		time.Sleep(time.Millisecond)
	}
}

// A rebuild may scan appends made after its initial stat. Its continuation
// offset must describe the same completed commit as its sequence watermark.
func TestHistoryIndexRebuildPairsScannedSequenceAndOffset(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	persistence := NewFilesystemPersistence(root)
	service, err := NewService("local", persistence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "rebuild-cut"})
	if err != nil {
		t.Fatal(err)
	}
	appendMessage := func(id string) {
		t.Helper()
		payload, err := json.Marshal(map[string]any{"message": provider.Message{ID: id, Role: provider.RoleAssistant, Content: id}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = runtime.Session().AppendBatch(t.Context(), id, []Event{{Kind: "message/complete", Payload: payload}}); err != nil {
			t.Fatal(err)
		}
		if _, err = runtime.Session().Flush(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	appendMessage("before-stat")
	dir := filepath.Join(root, runtime.Ref().SessionID)
	staleRevision, err := revisionOfLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	appendMessage("after-stat")
	path := historyIndexPath(root, runtime.Ref().SessionID)
	if err := rebuildHistoryIndex(t.Context(), dir, path, runtime.Ref().SessionID, staleRevision); err != nil {
		t.Fatal(err)
	}
	appendMessage("after-scan")
	page := historyPageReady(t, service.Query(), runtime.Ref(), "", 32)
	if len(page.Messages) != 3 {
		t.Fatalf("messages = %+v", page.Messages)
	}
	for i, id := range []string{"before-stat", "after-stat", "after-scan"} {
		if page.Messages[i].MessageID != id {
			t.Fatalf("message %d = %+v", i, page.Messages[i])
		}
	}
}

func waitHistoryPage(t *testing.T, query *Query, ref SessionRef, cursor string, limit int) (MessageHistoryPage, error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		page, err := query.HistoryPage(t.Context(), ref, cursor, limit)
		if err != nil || page.Status != "preparing" {
			return page, err
		}
		if time.Now().After(deadline) {
			t.Fatalf("history locator remained preparing: %+v", page)
		}
		time.Sleep(time.Millisecond)
	}
}

func historyPageReady(t *testing.T, query *Query, ref SessionRef, cursor string, limit int) MessageHistoryPage {
	t.Helper()
	page, err := waitHistoryPage(t, query, ref, cursor, limit)
	if err != nil {
		t.Fatal(err)
	}
	if page.Status != "ready" {
		t.Fatalf("history page = %+v", page)
	}
	return page
}

func TestExternalHistoryColdOpenDefersBodiesBeforeModelReset(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "bounded-open"})
	if err != nil {
		t.Fatal(err)
	}
	oldPayload, err := json.Marshal(map[string]any{"message": provider.Message{ID: "old", Role: provider.RoleUser, Content: strings.Repeat("old", 40<<10)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().AppendBatch(t.Context(), "old", []Event{{Kind: "message/complete", Payload: oldPayload}}); err != nil {
		t.Fatal(err)
	}
	current := provider.Message{ID: "current", Role: provider.RoleUser, Content: "current workset"}
	currentPayload, err := json.Marshal(map[string]any{"messages": []provider.Message{current}, "reason": "bounded cold open"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().AppendBatch(t.Context(), "reset", []Event{{Kind: "model/context-replace", Payload: currentPayload}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	ref := runtime.Ref()
	if err := service.Close(t.Context(), ref); err != nil {
		t.Fatal(err)
	}

	log, err := os.Open(filepath.Join(root, ref.SessionID, "events.frames"))
	if err != nil {
		t.Fatal(err)
	}
	var historicalDigest string
	err = scanV4CommitFileRefs(t.Context(), log, 0, 1, contentStoreForSessionDir(filepath.Join(root, ref.SessionID)), nil, func(_ int64, commit Commit) bool {
		for _, event := range commit.Events {
			if event.Kind == "message/complete" && event.PayloadRef != nil {
				historicalDigest = event.PayloadRef.Digest
			}
		}
		return true
	})
	_ = log.Close()
	if err != nil || historicalDigest == "" {
		t.Fatalf("historical content reference = %q, %v", historicalDigest, err)
	}
	object := filepath.Join(root, ".content-v1", "objects", historicalDigest[:2], historicalDigest[2:4], historicalDigest)
	if err := os.Remove(object); err != nil {
		t.Fatal(err)
	}

	reopenedService, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedService.CloseAll(context.Background()) })
	binding, err := reopenedService.Open(t.Context(), ref)
	if err != nil {
		t.Fatalf("cold open resolved retired history body: %v", err)
	}
	defer binding.Release(context.Background())
	model := binding.Runtime().Session().DeriveMessages()
	if len(model) != 1 || model[0].ID != current.ID || model[0].Content != current.Content {
		t.Fatalf("cold model projection = %+v", model)
	}
	if _, err := waitHistoryPage(t, reopenedService.Query(), ref, "", 100); err == nil {
		t.Fatal("history query accepted a missing referenced body")
	}
}

func TestHistoryPageKeepsSnapshotAndAuthorizesReferencedContent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "paged"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(context.Background(), runtime.Ref()); err != nil {
			t.Error(err)
		}
	})
	appendMessage := func(id, content string) {
		payload, err := json.Marshal(map[string]any{"message": provider.Message{ID: id, Role: provider.RoleUser, Content: content}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Session().Append(t.Context(), Batch{OperationID: "message-" + id, Events: []Event{{Kind: "message/complete", Payload: payload}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Session().Flush(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	appendMessage("one", "first")
	appendMessage("two", strings.Repeat("large", 20000))
	appendMessage("three", "third")
	runtime.Session().mu.Lock()
	residentMessages := len(runtime.Session().projection.Messages)
	residentModel := len(runtime.Session().projection.ModelMessages)
	runtime.Session().mu.Unlock()
	if residentMessages != 0 || residentModel != 3 {
		t.Fatalf("service runtime retained durable UI bodies: messages=%d model=%d", residentMessages, residentModel)
	}
	ref := runtime.Ref()
	first := historyPageReady(t, service.Query(), ref, "", 1)
	if len(first.Messages) != 1 || first.Messages[0].MessageID != "three" || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	appendMessage("four", "must not enter the fixed snapshot")
	second, err := waitHistoryPage(t, service.Query(), ref, first.NextCursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Messages) != 2 || second.Messages[0].MessageID != "one" || second.Messages[1].MessageID != "two" {
		t.Fatalf("fixed snapshot second page = %+v", second)
	}
	large := second.Messages[1]
	if large.ContentRef == nil || len(large.Inline) != 0 {
		t.Fatalf("large message was not referenced: %+v", large)
	}
	if err := os.RemoveAll(historyIndexPath(root, "paged")); err != nil {
		t.Fatal(err)
	}
	chunk, err := service.Query().ReadContent(t.Context(), ref, *large.ContentRef, 0, min(64, large.ContentRef.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	var decoded provider.Message
	full, err := service.Query().ReadContent(t.Context(), ref, *large.ContentRef, 0, large.ContentRef.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunk) == 0 || json.Unmarshal(full, &decoded) != nil || decoded.ID != "two" {
		t.Fatalf("resolved content prefix=%q id=%q", chunk, decoded.ID)
	}
	foreign := *large.ContentRef
	foreign.Digest = strings.Repeat("0", len(foreign.Digest))
	if _, err := service.Query().ReadContent(t.Context(), ref, foreign, 0, 1); err == nil {
		t.Fatal("content hash without a session reference was authorized")
	}
	if _, err := service.Query().ReadContent(t.Context(), ref, *large.ContentRef, 0, (1<<20)+1); err == nil {
		t.Fatal("oversized content range was accepted")
	}
}

func TestLocateMessageReturnsFixedSnapshotCursorWithoutBody(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "locate"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two", "three"} {
		payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: id, Role: provider.RoleUser, Content: id}})
		if _, err := runtime.Session().Append(t.Context(), Batch{OperationID: "locate-" + id, Events: []Event{{Kind: "message/complete", Payload: payload}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = historyPageReady(t, service.Query(), runtime.Ref(), "", 1)
	location, err := service.Query().LocateMessage(t.Context(), runtime.Ref(), "two", 0)
	if err != nil || location.Status != "ready" || location.Position == 0 || location.Cursor == "" {
		t.Fatalf("location = %+v, %v", location, err)
	}
	page, err := service.Query().HistoryPage(t.Context(), runtime.Ref(), location.Cursor, 1)
	if err != nil || len(page.Messages) != 1 || page.Messages[0].MessageID != "two" {
		t.Fatalf("located page = %+v, %v", page, err)
	}
}

func TestHistoryLocatorGenerationSurvivesRebuildAndChangesOnReplacement(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "locator-generation"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: id, Role: provider.RoleUser, Content: id}})
		if _, err := runtime.Session().Append(t.Context(), Batch{OperationID: "generation-" + id, Events: []Event{{Kind: "message/complete", Payload: payload}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	first := historyPageReady(t, service.Query(), runtime.Ref(), "", 1)
	if first.NextCursor == "" || first.Generation == "" {
		t.Fatalf("first page = %+v", first)
	}
	if err := os.Remove(historyIndexPath(root, runtime.Ref().SessionID)); err != nil {
		t.Fatal(err)
	}
	service.Query().historyMu.Lock()
	delete(service.Query().historyBuilds, runtime.Ref().SessionID)
	service.Query().historyMu.Unlock()
	rebuilt := historyPageReady(t, service.Query(), runtime.Ref(), "", 1)
	if rebuilt.Generation != first.Generation {
		t.Fatalf("ordinary rebuild changed generation: %q -> %q", first.Generation, rebuilt.Generation)
	}
	if page, err := service.Query().HistoryPage(t.Context(), runtime.Ref(), first.NextCursor, 1); err != nil || page.Status != "ready" || len(page.Messages) != 1 || page.Messages[0].MessageID != "one" {
		t.Fatalf("cursor after rebuild = %+v, %v", page, err)
	}
	replacement := []provider.Message{{ID: "replacement", Role: provider.RoleUser, Content: "replacement"}}
	payload, _ := json.Marshal(map[string]any{"messages": replacement})
	if _, err := runtime.Session().Append(t.Context(), Batch{OperationID: "replace-history", Events: []Event{{Kind: "history/replace", Payload: payload}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Query().HistoryPage(t.Context(), runtime.Ref(), "", 1); err != nil {
		t.Fatal(err)
	}
	stale, err := waitHistoryPage(t, service.Query(), runtime.Ref(), first.NextCursor, 1)
	if err != nil || stale.Status != "stale_cursor" {
		t.Fatalf("cursor after replacement = %+v, %v", stale, err)
	}
}

func TestSearchHistoryUsesStableSnapshotAndOpaqueQueryCursor(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "search"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(context.Background(), runtime.Ref()); err != nil {
			t.Error(err)
		}
	})
	appendMessage := func(id, content string) {
		payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: id, Role: provider.RoleUser, Content: content}})
		if _, err := runtime.Session().Append(t.Context(), Batch{OperationID: id, Events: []Event{{Kind: "message/complete", Payload: payload}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Session().Flush(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	appendMessage("one", "first needle")
	appendMessage("two", "second needle")
	appendMessage("three", "unrelated")
	preparing, err := service.Query().SearchHistory(t.Context(), runtime.Ref(), "needle", "", 1)
	if err != nil || preparing.Status != "preparing" {
		t.Fatalf("first search did not return preparation state: %+v, %v", preparing, err)
	}
	first := searchHistoryReady(t, service.Query(), runtime.Ref(), "needle", "", 1)
	if len(first.Hits) != 1 || first.Hits[0].MessageID != "two" || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first search = %+v", first)
	}
	appendMessage("four", "new needle outside snapshot")
	second, err := service.Query().SearchHistory(t.Context(), runtime.Ref(), "needle", first.NextCursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Hits) != 1 || second.Hits[0].MessageID != "one" {
		t.Fatalf("second search = %+v", second)
	}
	stale, err := service.Query().SearchHistory(t.Context(), runtime.Ref(), "different", first.NextCursor, 10)
	if err != nil || stale.Status != "stale_cursor" {
		t.Fatalf("search cursor mismatch = %+v, %v", stale, err)
	}
}

func TestSearchHistoryCoversInlineFieldsAndReferencedBodies(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "search-storage"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(context.Background(), runtime.Ref()); err != nil {
			t.Error(err)
		}
	})
	messages := []provider.Message{
		{ID: "inline", Role: provider.RoleAssistant, RawContent: "raw-field-needle", ReasoningContent: "reasoning-field-needle"},
		{ID: "referenced", Role: provider.RoleUser, Content: strings.Repeat("large-body-", 7000) + "referenced-field-needle"},
	}
	for _, message := range messages {
		payload, marshalErr := json.Marshal(map[string]any{"message": message})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, err := runtime.Session().AppendBatch(t.Context(), "append-"+message.ID, []Event{{Kind: "message/complete", Payload: payload}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]string{
		"raw-field-needle":        "inline",
		"reasoning-field-needle":  "inline",
		"referenced-field-needle": "referenced",
	} {
		page := searchHistoryReady(t, service.Query(), runtime.Ref(), query, "", 10)
		if len(page.Hits) != 1 || page.Hits[0].MessageID != want {
			t.Fatalf("search %q = %+v, want %q", query, page.Hits, want)
		}
	}
}

func TestSearchHistoryKeepsLiteralUnicodeSubstringSemanticsIndependently(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "literal-search"})
	if err != nil {
		t.Fatal(err)
	}
	appendRecoveryTestMessage(t, runtime.Session(), "cn", `大会话恢复包含字面符号 "OR" % _`)
	appendRecoveryTestMessage(t, runtime.Session(), "other", "unrelated")
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"会", "会话", "话恢", `"OR"`, "%", "_"} {
		page := searchHistoryReady(t, service.Query(), runtime.Ref(), query, "", 10)
		if page.Status != "ready" || page.CoverageSequence != runtime.Session().EventSequence() || len(page.Hits) != 1 || page.Hits[0].MessageID != "cn" {
			t.Fatalf("search %q = %+v", query, page)
		}
	}
	searchPath := searchIndexPath(root, "literal-search")
	before, err := os.Stat(searchPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(historyIndexPath(root, "literal-search")); err != nil {
		t.Fatal(err)
	}
	appendRecoveryTestMessage(t, runtime.Session(), "new", "新的会话子串")
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	page := searchHistoryReady(t, service.Query(), runtime.Ref(), "会话", "", 10)
	after, err := os.Stat(searchPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("search index was rebuilt instead of incrementally advanced")
	}
	if len(page.Hits) != 2 || page.Hits[0].MessageID != "new" || page.Hits[1].MessageID != "cn" {
		t.Fatalf("independent incremental search = %+v", page)
	}
}

func TestHistoryIndexHasSnapshotPositionIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	handle, err := projectiondb.Open(t.Context(), projectiondb.OpenOptions{Path: path, Migrations: historyMigrations, RequireDisk: true, MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer handle.DB.Close()
	rows, err := handle.DB.QueryContext(context.Background(), `PRAGMA index_list(messages)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var sequence, unique, partial int
		var name, origin string
		if err := rows.Scan(&sequence, &name, &unique, &origin, &partial); err != nil {
			t.Fatal(err)
		}
		found = found || name == "messages_snapshot_position"
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("snapshot-position query index is missing")
	}
}

func TestHistoryIndexAdvancesInPlaceAfterAppend(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "incremental"})
	if err != nil {
		t.Fatal(err)
	}
	appendRecoveryTestMessage(t, runtime.Session(), "first", "first")
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = historyPageReady(t, service.Query(), runtime.Ref(), "", 100)
	path := historyIndexPath(root, "incremental")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := projectiondb.Open(t.Context(), projectiondb.OpenOptions{Path: path, Migrations: historyMigrations, RequireDisk: true, MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	var inlineBytes, searchBytes int64
	if err := handle.DB.QueryRowContext(t.Context(), `SELECT COALESCE(SUM(length(inline)),0),COALESCE(SUM(length(search_text)),0) FROM messages`).Scan(&inlineBytes, &searchBytes); err != nil {
		_ = handle.DB.Close()
		t.Fatal(err)
	}
	_ = handle.DB.Close()
	if inlineBytes != 0 || searchBytes != 0 {
		t.Fatalf("locator retained message bodies: inline=%d search=%d", inlineBytes, searchBytes)
	}
	appendRecoveryTestMessage(t, runtime.Session(), "second", "second")
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	page, err := waitHistoryPage(t, service.Query(), runtime.Ref(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("history locator was replaced instead of advanced in place")
	}
	if len(page.Messages) != 2 || page.Messages[0].MessageID != "first" || page.Messages[1].MessageID != "second" {
		t.Fatalf("incremental page = %+v", page)
	}
}
