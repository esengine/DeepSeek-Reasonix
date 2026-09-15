package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
)

func writePrototypeStore(t *testing.T, dir string, events []Event, torn string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: 3, Codec: PrototypeCodec, SessionID: "prototype", CreatedAt: time.Now().UTC(), WriterGeneration: 1}
	manifestBytes, _ := json.Marshal(manifest)
	if err := fileutil.AtomicWriteFileStrict(filepath.Join(dir, "manifest.json"), append(manifestBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	var log []byte
	if len(events) > 0 {
		for i := range events {
			events[i].ID = "event-" + string(rune('a'+i))
			events[i].Sequence = uint64(i + 1)
		}
		hash, err := hashOperation("prototype", "", events)
		if err != nil {
			t.Fatal(err)
		}
		commit := Commit{SchemaVersion: 3, Codec: PrototypeCodec, RecordType: "commit", ID: "prototype-commit", OperationID: "prototype-operation", OperationHash: hash, FirstSequence: 1, EventCount: len(events), WriterGeneration: 1, CreatedAt: time.Now().UTC(), Events: events}
		line, _ := json.Marshal(commit)
		log = append(line, '\n')
	}
	log = append(log, torn...)
	if err := fileutil.AtomicWriteFileStrict(filepath.Join(dir, "events.jsonl"), log, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestContinueImportedResolvesPairedHistoryStructurally(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, "sessions")
	legacyPath := filepath.Join(legacyDir, "paired.jsonl")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := agent.NewSession("system")
	legacy.Add(provider.Message{ID: "user-1", Role: provider.RoleUser, Content: "hello"})
	if err := legacy.Save(legacyPath); err != nil {
		t.Fatal(err)
	}
	messages := legacy.Snapshot()
	payload, err := json.Marshal(map[string]any{"messages": messages})
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(root, "sessions-v4")
	previewDir := filepath.Join(targetRoot, agent.BranchID(legacyPath))
	writePrototypeStore(t, previewDir, []Event{{Kind: "context/replace", Payload: payload}}, "")

	result, err := importSourceForLegacyWithHeader(t.Context(), legacyPath, targetRoot, "", CreateOptions{
		CWD: "/workspace", Origin: SessionOriginLegacyImport,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != "events" || result.TargetID == agent.BranchID(legacyPath) {
		t.Fatalf("resolved import = %+v", result)
	}
	info, err := NewFilesystemPersistence(targetRoot).Stat(t.Context(), result.TargetID)
	// Headers record CWD in OS-native form; clean the expectation the same way.
	if err != nil || info.CWD != filepath.Clean("/workspace") || info.Origin != SessionOriginLegacyImport {
		t.Fatalf("imported header = %+v, %v", info, err)
	}
}

func TestContinueImportedReusesFinalCanonicalStore(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, "legacy.jsonl")
	legacy := agent.NewSession("system")
	legacy.Add(provider.Message{Role: provider.RoleUser, Content: "old"})
	if err := legacy.Save(legacyPath); err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(root, "sessions-v4")
	id := agent.BranchID(legacyPath)
	canonical, err := Open(filepath.Join(targetRoot, id), id)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(struct {
		Message provider.Message `json:"message"`
	}{Message: provider.Message{ID: "new", Role: provider.RoleAssistant, Content: "newer canonical work"}})
	if _, err := canonical.Append(t.Context(), Batch{OperationID: "canonical", Events: []Event{{Kind: "message/complete", Payload: payload}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := canonical.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := canonical.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	result, err := importSourceForLegacy(t.Context(), legacyPath, targetRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetID != id || !result.Reused || result.Kind != "final" {
		t.Fatalf("result = %+v", result)
	}
}

func TestContinueImportedRefusesDivergentPairedHistory(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, "sessions")
	legacyPath := filepath.Join(legacyDir, "conflict.jsonl")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := agent.NewSession("system")
	legacy.Add(provider.Message{ID: "legacy-user", Role: provider.RoleUser, Content: "legacy"})
	if err := legacy.Save(legacyPath); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"messages": []provider.Message{{ID: "event-user", Role: provider.RoleUser, Content: "events"}}})
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(root, "sessions-v4")
	writePrototypeStore(t, filepath.Join(targetRoot, agent.BranchID(legacyPath)), []Event{{Kind: "context/replace", Payload: payload}}, "")
	if _, err := importSourceForLegacy(t.Context(), legacyPath, targetRoot, ""); !errors.Is(err, ErrImportConflict) {
		t.Fatalf("conflicting import = %v", err)
	}
	// Classification happens before publication: a refused import must not have
	// adopted the transcript as an executable target. The event sidecar source
	// directory is the only pre-existing entry under the target root.
	assertNoMigrationTarget(t, targetRoot, legacyPath, legacyDir)
}

// assertNoMigrationTarget proves the frozen legacy head was never published.
// The paired sidecar source is an input, not a target, so it is expected.
func assertNoMigrationTarget(t *testing.T, targetRoot, legacyPath, legacyDir string) {
	t.Helper()
	sourceSHA, err := sourceDigestForTest(legacyPath, legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	targetID := migrationTargetID(legacyPath, sourceSHA, "")
	if _, statErr := os.Stat(filepath.Join(targetRoot, targetID)); statErr == nil {
		t.Fatalf("legacy target %q was published before classification", targetID)
	}
}

// sourceDigestForTest recomputes the frozen source digest the same way the
// migration path does.
func sourceDigestForTest(sourcePath, sourceDir string) (string, error) {
	artifacts, source, freezeDir, err := freezeLegacyArtifacts(context.Background(), sourcePath)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(freezeDir)
	_ = artifacts
	_ = sourceDir
	return source.SHA256, nil
}

func TestContinueImportedPublishesOnlyTheSelectedSource(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, "sessions")
	legacyPath := filepath.Join(legacyDir, "paired.jsonl")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := agent.NewSession("system")
	legacy.Add(provider.Message{ID: "shared", Role: provider.RoleUser, Content: "first"})
	if err := legacy.Save(legacyPath); err != nil {
		t.Fatal(err)
	}
	// The sidecar holds the transcript's own prefix plus one newer message, so
	// it is the only source that can be resumed without losing work.
	previewMessages := append(legacy.Snapshot(), provider.Message{ID: "newer", Role: provider.RoleAssistant, Content: "second"})
	payload, err := json.Marshal(map[string]any{"messages": previewMessages})
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(root, "sessions-v4")
	writePrototypeStore(t, filepath.Join(targetRoot, agent.BranchID(legacyPath)), []Event{{Kind: "context/replace", Payload: payload}}, "")
	result, err := importSourceForLegacy(t.Context(), legacyPath, targetRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != "events" {
		t.Fatalf("selected kind = %q, want events", result.Kind)
	}
	// The transcript target must not exist: only the selected source is built.
	assertNoMigrationTarget(t, targetRoot, legacyPath, legacyDir)
	messages, err := projectedMessagesForTest(t, targetRoot, result.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != len(previewMessages) || messages[len(messages)-1].ID != "newer" {
		t.Fatalf("selected projection has %d messages, want the newer sidecar history", len(messages))
	}
}

func projectedMessagesForTest(t *testing.T, targetRoot, sessionID string) ([]provider.Message, error) {
	t.Helper()
	handle, err := NewFilesystemPersistence(targetRoot).Open(sessionID, ReadOnly)
	if err != nil {
		return nil, err
	}
	defer handle.Close(context.Background())
	projection := Projection{}
	var cursor uint64
	for {
		page, readErr := handle.Read(t.Context(), cursor, 1000)
		if readErr != nil {
			return nil, readErr
		}
		for _, commit := range page.Commits {
			if applyErr := applyProjectionCommit(&projection, commit); applyErr != nil {
				return nil, applyErr
			}
		}
		if !page.Truncated {
			return projection.Messages, nil
		}
		cursor = page.Next
	}
}

func TestPreviewFreezeRefusesExactWriterOwnership(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "preview")
	writePrototypeStore(t, dir, nil, "")
	release, err := filelock.Acquire(t.Context(), filepath.Join(dir, "writer.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := freezePreview(t.Context(), dir); !errors.Is(err, ErrWriterOwned) {
		t.Fatalf("freeze error = %v, want ErrWriterOwned", err)
	}
}

func TestContinueStoredPreviewUpgradesLinearV3ToFinalCodec(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	oldDir := filepath.Join(root, "old-linear")
	store, err := CreateStore(oldDir, "old-linear")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: "user", Role: provider.RoleUser, Content: "hello"}})
	if _, err := store.Append(t.Context(), Batch{OperationID: "message", Events: []Event{{Kind: "message/complete", Payload: payload}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	commits, err := Replay(oldDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: 3, Codec: LegacyLinearCodec, SessionID: "old-linear", CreatedAt: time.Now().UTC(), WriterGeneration: 1}
	manifestBytes, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(oldDir, "manifest.json"), append(manifestBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	for _, commit := range commits {
		commit.SchemaVersion = 3
		commit.Codec = LegacyLinearCodec
		line, _ := json.Marshal(commit)
		log.Write(line)
		log.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(oldDir, "events.jsonl"), log.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, result, err := service.ContinueStoredPreview(t.Context(), "old-linear")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close(context.Background(), runtime.Ref()) })
	if result.Source.Version != LegacyLinearCodec || runtime.Ref().SessionID == "old-linear" {
		t.Fatalf("upgrade = %+v, ref = %+v", result, runtime.Ref())
	}
	if got := runtime.Session().Snapshot().Projection.ModelMessages; len(got) != 1 || got[0].ID != "user" {
		t.Fatalf("upgraded messages = %+v", got)
	}
}

func TestContinueStoredPreviewUpgradesUnpublishedV4Draft(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	draftDir := filepath.Join(root, "draft-v4")
	store, err := CreateStore(draftDir, "draft-v4")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: "user", Role: provider.RoleUser, Content: "hello"}})
	if _, err := store.Append(t.Context(), Batch{OperationID: "message", Events: []Event{{Kind: "message/complete", Payload: payload}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	manifest, err := readManifest(filepath.Join(draftDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest.StorageRevision = 0
	if err := writeManifestFile(filepath.Join(draftDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(draftDir, "draft-v4"); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("direct draft Open error = %v", err)
	}

	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, result, err := service.ContinueStoredPreview(t.Context(), "draft-v4")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close(context.Background(), runtime.Ref()) })
	if result.Source.Version != Codec || runtime.Ref().SessionID == "draft-v4" {
		t.Fatalf("upgrade = %+v, ref = %+v", result, runtime.Ref())
	}
	upgraded, err := readManifest(filepath.Join(root, runtime.Ref().SessionID, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.StorageRevision != StorageRevision {
		t.Fatalf("storage revision = %d, want %d", upgraded.StorageRevision, StorageRevision)
	}
	if got := runtime.Session().Snapshot().Projection.ModelMessages; len(got) != 1 || got[0].ID != "user" {
		t.Fatalf("upgraded messages = %+v", got)
	}
	var original Manifest
	originalBytes, err := os.ReadFile(filepath.Join(draftDir, "manifest.json"))
	if err == nil {
		err = json.Unmarshal(originalBytes, &original)
	}
	if err != nil || original.StorageRevision != 0 {
		t.Fatalf("draft source changed: %+v, %v", original, err)
	}
}

func TestPrototypeRequiresExplicitImportAndPreservesTornTail(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "prototype")
	payload, _ := json.Marshal(map[string]any{"messages": []provider.Message{{ID: "message-1", Role: provider.RoleUser, Content: "hello"}}, "reason": "prototype"})
	writePrototypeStore(t, source, []Event{{Kind: "context/replace", Payload: payload}}, `{"torn":`)
	if _, err := Open(source, "prototype"); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("direct prototype Open error = %v", err)
	}

	result, err := ImportPrototype(t.Context(), source, filepath.Join(root, "final"))
	if err != nil {
		t.Fatal(err)
	}
	if result.ImportedEvents != 1 {
		t.Fatalf("imported events = %d", result.ImportedEvents)
	}
	store, err := Open(result.TargetDir, result.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	projection := store.Snapshot().Projection
	if len(projection.Messages) != 1 || projection.Messages[0].ID != "message-1" || len(projection.ModelMessages) != 1 {
		t.Fatalf("projection = %+v", projection)
	}
	if err := store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	frozen, err := os.ReadFile(filepath.Join(result.TargetDir, "legacy", "prototype", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(frozen[len(frozen)-len(`{"torn":`):]) != `{"torn":` {
		t.Fatalf("prototype tail was not preserved: %q", frozen)
	}

	reused, err := ImportPrototype(t.Context(), source, filepath.Join(root, "final"))
	if err != nil || !reused.Reused || reused.TargetID != result.TargetID {
		t.Fatalf("reused import = %+v, %v", reused, err)
	}
}

func TestPrototypeImportAcceptsLegacyRecordLargerThan64MiB(t *testing.T) {
	if testing.Short() {
		t.Skip("capacity regression")
	}
	root := t.TempDir()
	source := filepath.Join(root, "prototype-large-record")
	large := strings.Repeat("x", (64<<20)+(1<<20))
	payload, err := json.Marshal(map[string]any{"message": provider.Message{ID: "large-message", Role: provider.RoleAssistant, Content: large}})
	if err != nil {
		t.Fatal(err)
	}
	writePrototypeStore(t, source, []Event{{Kind: "message/complete", Payload: payload}}, "")
	result, err := ImportPrototype(t.Context(), source, filepath.Join(root, "final"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(result.TargetDir, result.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(context.Background())
	messages := store.Snapshot().Projection.Messages
	if len(messages) != 1 || messages[0].Content != large {
		t.Fatalf("large legacy record round trip = %d messages / %d bytes", len(messages), func() int {
			if len(messages) == 0 {
				return 0
			}
			return len(messages[0].Content)
		}())
	}
}

func TestPrototypeImportRejectsUnknownRequiredEvent(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "prototype")
	writePrototypeStore(t, source, []Event{{Kind: "future/required"}}, "")
	if _, err := ImportPrototype(t.Context(), source, filepath.Join(root, "final")); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("unknown required import error = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "final"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name()[0] != '.' {
			t.Fatalf("failed import published %q", entry.Name())
		}
	}
}

func TestImportMessageComparisonIgnoresNonAuthoritativeToolRecovery(t *testing.T) {
	base := []provider.Message{{
		Role: provider.RoleAssistant,
		ID:   "assistant-tool",
		ToolCalls: []provider.ToolCall{{
			ID: "call-1", Name: "read_file", Arguments: `{"path":"input.txt"}`,
		}},
	}}
	legacy := append([]provider.Message(nil), base...)
	legacy[0].ToolCalls = append([]provider.ToolCall(nil), base[0].ToolCalls...)
	legacy[0].ToolCalls[0].Recovery = &provider.ToolCallRecord{}
	legacy[0].WorkDurationMs = 41

	if !messagesEqual(legacy, base) || !messagesPrefix(legacy, base) {
		t.Fatal("process-local recovery metadata created a false import conflict")
	}
	if !messagesPrefix([]provider.Message{}, base) {
		t.Fatal("an empty legacy transcript was not recognized as an event-history prefix")
	}
}
