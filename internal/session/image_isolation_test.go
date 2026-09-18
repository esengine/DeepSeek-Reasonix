package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/sessioncontent"
)

func TestImageIsolationUpgradesRevisionAndSurvivesRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	persistence := NewFilesystemPersistence(root)
	session, err := persistence.Create(CreateOptions{SessionID: "isolated"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close(t.Context()) })
	if got := session.Manifest().StorageRevision; got != StorageRevision {
		t.Fatalf("ordinary revision=%d want=%d", got, StorageRevision)
	}
	image := "data:image/png;base64,AAAA"
	message := provider.Message{ID: "user-message", Role: provider.RoleUser, Content: "inspect", Images: []string{image}}
	payload, _ := json.Marshal(map[string]any{"message": message})
	messageCommit, err := session.Append(t.Context(), Batch{OperationID: "message", Events: []Event{{Kind: "message/complete", Payload: payload}}})
	if err != nil {
		t.Fatal(err)
	}
	decision := provider.ImageIsolationDecision{
		Version:  1,
		Identity: provider.ImageIdentity{MessageID: message.ID, ImageOrdinal: 0, ContentDigest: provider.ImageContentDigest(image)},
		Reason:   "corrupt_image", Scope: provider.ImageIsolationScope{Kind: "content"},
	}
	if err := session.EnsureStorageRevision(t.Context(), MaxStorageRevision); err != nil {
		t.Fatal(err)
	}
	decisionPayload, _ := json.Marshal(decision)
	isolationCommit, err := session.Append(t.Context(), Batch{OperationID: "isolate", Events: []Event{{Kind: "model/image-isolation", Required: true, Payload: decisionPayload}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertIsolatedProjection(t, session.Snapshot().Projection, image, decision)
	if got := session.Manifest().StorageRevision; got != MaxStorageRevision {
		t.Fatalf("revision=%d want=%d", got, MaxStorageRevision)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(root, "isolated", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	reopened, err := persistence.Open("isolated", ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(t.Context())
	assertIsolatedProjection(t, reopened.Snapshot().Projection, image, decision)
	if got := reopened.Manifest().StorageRevision; got != MaxStorageRevision {
		t.Fatalf("reopened revision=%d", got)
	}
	if isolationCommit.LastSequence() <= messageCommit.LastSequence() {
		t.Fatal("isolation commit did not follow the original message")
	}
	afterOpen, err := os.ReadFile(filepath.Join(root, "isolated", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(manifestBytes, afterOpen) {
		// WriterGeneration must advance, but the storage revision must stay 4.
		t.Fatal("writer reopen did not publish a new generation")
	}
}

func assertIsolatedProjection(t *testing.T, projection Projection, original string, decision provider.ImageIsolationDecision) {
	t.Helper()
	if len(projection.Messages) != 1 || len(projection.Messages[0].Images) != 1 || projection.Messages[0].Images[0] != original {
		t.Fatalf("canonical history changed: %#v", projection.Messages)
	}
	if len(projection.ModelMessages) != 1 || len(projection.ModelMessages[0].ImageIsolations) != 1 {
		t.Fatalf("model isolation metadata missing: %#v", projection.ModelMessages)
	}
	projected, changed := provider.ApplyImageIsolation(projection.ModelMessages, []provider.ImageIsolationDecision{decision}, provider.ImageRequestIdentity{})
	if !changed || len(projected[0].Images) != 0 || projected[0].Content != "inspect\n\n"+provider.ImageUnavailablePlaceholder {
		t.Fatalf("isolated projection=%#v changed=%v", projected, changed)
	}
}

func TestForkRevisionTracksIsolationPrefix(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	parent, err := NewFilesystemPersistence(root).Create(CreateOptions{SessionID: "parent"})
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close(t.Context())
	image := "data:image/png;base64,AAAA"
	payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: "user", Role: provider.RoleUser, Content: "inspect", Images: []string{image}}})
	messageCommit, err := parent.Append(t.Context(), Batch{OperationID: "message", Events: []Event{{Kind: "message/complete", Payload: payload}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.EnsureStorageRevision(t.Context(), MaxStorageRevision); err != nil {
		t.Fatal(err)
	}
	decision, _ := json.Marshal(provider.ImageIsolationDecision{Version: 1, Identity: provider.ImageIdentity{MessageID: "user", ImageOrdinal: 0, ContentDigest: provider.ImageContentDigest(image)}, Reason: "corrupt_image", Scope: provider.ImageIsolationScope{Kind: "content"}})
	isolationCommit, err := parent.Append(t.Context(), Batch{OperationID: "isolation", Events: []Event{{Kind: "model/image-isolation", Required: true, Payload: decision}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parent.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := parent.Fork(t.Context(), filepath.Join(root, "before"), "before", messageCommit.LastSequence())
	if err != nil {
		t.Fatal(err)
	}
	after, err := parent.Fork(t.Context(), filepath.Join(root, "after"), "after", isolationCommit.LastSequence())
	if err != nil {
		t.Fatal(err)
	}
	if before.StorageRevision != StorageRevision || after.StorageRevision != MaxStorageRevision {
		t.Fatalf("fork revisions before=%d after=%d", before.StorageRevision, after.StorageRevision)
	}
}

func TestRevisionThreeReaderRejectsRevisionFourWithoutWriting(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	session, err := NewFilesystemPersistence(root).Create(CreateOptions{SessionID: "newer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.EnsureStorageRevision(t.Context(), MaxStorageRevision); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "newer", "manifest.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readStoredManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	previousAccepts := manifest.SchemaVersion == SchemaVersion && manifest.Codec == Codec && manifest.StorageRevision >= 1 && manifest.StorageRevision <= 3
	if previousAccepts {
		t.Fatal("revision-3 reader accepted revision 4")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("previous-reader compatibility check changed the manifest")
	}
}

func TestRevisionFourQueryIndexesAndCursorsUseManifestRevision(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(t.Context()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "revision-four-query"})
	if err != nil {
		t.Fatal(err)
	}
	appendWindowMessages(t, runtime, "one", "two", "three")
	ref := runtime.Ref()

	oldHistory := historyPageReady(t, service.Query(), ref, "", 1)
	oldWindow := windowReady(t, service.Query(), ref, HistoryWindowRequest{Anchor: "newest", Limit: 1})
	oldSearch := searchHistoryReady(t, service.Query(), ref, "body", "", 1)
	if oldHistory.NextCursor == "" || oldWindow.OlderCursor == "" || oldSearch.NextCursor == "" {
		t.Fatalf("revision 3 cursors missing: history=%q window=%q search=%q", oldHistory.NextCursor, oldWindow.OlderCursor, oldSearch.NextCursor)
	}
	if err := runtime.Session().EnsureStorageRevision(t.Context(), MaxStorageRevision); err != nil {
		t.Fatal(err)
	}

	newHistory := historyPageReady(t, service.Query(), ref, "", 1)
	newWindow := windowReady(t, service.Query(), ref, HistoryWindowRequest{Anchor: "newest", Limit: 1})
	newSearch := searchHistoryReady(t, service.Query(), ref, "body", "", 1)
	assertHistoryCursorRevision := func(name, cursor string, decode func(string) (int, error)) {
		t.Helper()
		revision, err := decode(cursor)
		if err != nil {
			t.Fatalf("decode %s cursor: %v", name, err)
		}
		if revision != MaxStorageRevision {
			t.Fatalf("%s cursor revision=%d want=%d", name, revision, MaxStorageRevision)
		}
	}
	assertHistoryCursorRevision("history", newHistory.NextCursor, func(cursor string) (int, error) {
		parsed, err := decodeHistoryCursor(cursor)
		return parsed.StorageRevision, err
	})
	assertHistoryCursorRevision("window", newWindow.OlderCursor, func(cursor string) (int, error) {
		parsed, err := decodeHistoryWindowCursor(cursor)
		return parsed.StorageRevision, err
	})
	assertHistoryCursorRevision("search", newSearch.NextCursor, func(cursor string) (int, error) {
		parsed, err := decodeSearchHistoryCursor(cursor)
		return parsed.StorageRevision, err
	})

	if stale, err := service.Query().HistoryPage(t.Context(), ref, oldHistory.NextCursor, 1); err != nil || stale.Status != "stale_cursor" {
		t.Fatalf("revision 3 history cursor after upgrade=%+v err=%v", stale, err)
	}
	if stale, err := service.Query().ReadHistoryWindow(t.Context(), ref, HistoryWindowRequest{Anchor: "cursor", Cursor: oldWindow.OlderCursor, Limit: 1}); err != nil || stale.Status != "stale_cursor" {
		t.Fatalf("revision 3 window cursor after upgrade=%+v err=%v", stale, err)
	}
	if stale, err := service.Query().SearchHistory(t.Context(), ref, "body", oldSearch.NextCursor, 1); err != nil || stale.Status != "stale_cursor" {
		t.Fatalf("revision 3 search cursor after upgrade=%+v err=%v", stale, err)
	}
}

func TestRevisionFourExportImportPreservesOriginalImageAndIsolation(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "source")
	source, err := NewService("source", NewFilesystemPersistence(sourceRoot))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.CloseAll(t.Context()) })
	runtime, err := source.Create(t.Context(), CreateOptions{SessionID: "revision-four-portable"})
	if err != nil {
		t.Fatal(err)
	}
	image := "data:image/png;base64,AAAA"
	message := provider.Message{ID: "portable-image", Role: provider.RoleUser, Content: "inspect", Images: []string{image}}
	payload, _ := json.Marshal(map[string]any{"message": message})
	if _, err := runtime.Session().AppendBatch(t.Context(), "portable-message", []Event{{Kind: "message/complete", Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	decision := provider.ImageIsolationDecision{
		Version:  1,
		Identity: provider.ImageIdentity{MessageID: message.ID, ImageOrdinal: 0, ContentDigest: provider.ImageContentDigest(image)},
		Reason:   "corrupt_image", Scope: provider.ImageIsolationScope{Kind: "content"},
	}
	if err := runtime.Session().EnsureStorageRevision(t.Context(), MaxStorageRevision); err != nil {
		t.Fatal(err)
	}
	decisionPayload, _ := json.Marshal(decision)
	if _, err := runtime.Session().AppendBatch(t.Context(), "portable-isolation", []Event{{Kind: "model/image-isolation", Required: true, Payload: decisionPayload}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	sourceDir := filepath.Join(sourceRoot, runtime.Ref().SessionID)
	orphanStore := contentStoreForSessionDir(sourceDir)
	orphan, err := orphanStore.Put(t.Context(), bytes.NewBufferString("unreferenced isolation bytes"), sessioncontent.Metadata{MediaType: "application/octet-stream"})
	if err != nil {
		t.Fatal(err)
	}
	exported := filepath.Join(t.TempDir(), "exported")
	if err := source.Export(t.Context(), runtime.Ref(), exported); err != nil {
		t.Fatal(err)
	}
	exportedManifest, err := readManifest(filepath.Join(exported, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if exportedManifest.StorageRevision != MaxStorageRevision {
		t.Fatalf("exported revision=%d", exportedManifest.StorageRevision)
	}
	if _, err := sessioncontent.New(filepath.Join(exported, ".content-v1")).ReadRange(t.Context(), orphan, 0, orphan.Bytes); err == nil {
		t.Fatal("export content closure included an unreferenced object")
	}
	commits, err := Replay(exported, nil)
	if err != nil {
		t.Fatal(err)
	}
	exportedProjection, err := Project(commits)
	if err != nil {
		t.Fatal(err)
	}
	assertIsolatedProjection(t, exportedProjection, image, decision)

	targetRoot := filepath.Join(t.TempDir(), "target")
	target, err := NewService("target", NewFilesystemPersistence(targetRoot))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.CloseAll(t.Context()) })
	ref, err := target.Import(t.Context(), exported)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := target.openRuntime(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Session().Manifest().StorageRevision != MaxStorageRevision {
		t.Fatalf("imported revision=%d", imported.Session().Manifest().StorageRevision)
	}
	assertIsolatedProjection(t, imported.Session().Snapshot().Projection, image, decision)
}
