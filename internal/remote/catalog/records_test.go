package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/store"
)

func TestListTopicsEmptyWorkspaceUsesJSONArray(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()

	topics, err := f.catalog.ListTopics(context.Background(), protocol.TopicListParams{
		ExpectedHostEpoch: testHostEpoch,
		WorkspaceID:       workspaceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if topics.Items == nil || len(topics.Items) != 0 || topics.HasMore || topics.NextCursor != "" {
		t.Fatalf("empty topic/list = %+v", topics)
	}
	payload, err := json.Marshal(topics)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(`"items":[]`)) {
		t.Fatalf("empty topic/list JSON must contain an array: %s", payload)
	}
}

func TestTopicAndSessionColdLifecycleMovesEveryArtifact(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	topicID := f.createTopic(workspaceID, "Original Topic")
	created := f.createSession(workspaceID, topicID)
	writeArtifactSet(t, created.SessionPath)

	renamed, err := f.catalog.RenameSession(protocol.SessionRenameParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "rename_session", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
		Title:                 "Named Session",
	})
	if err != nil || renamed.Title != "Named Session" {
		t.Fatalf("RenameSession = %+v, %v", renamed, err)
	}
	topicRenamed, err := f.catalog.RenameTopic(protocol.TopicRenameParams{
		HostMutation: protocol.HostMutation{RequestID: "rename_topic", ExpectedHostEpoch: testHostEpoch},
		WorkspaceID:  workspaceID,
		TopicID:      topicID,
		Title:        "Renamed Topic",
	})
	if err != nil || topicRenamed.Title != "Renamed Topic" {
		t.Fatalf("RenameTopic = %+v, %v", topicRenamed, err)
	}
	meta, ok, err := agent.LoadBranchMeta(created.SessionPath)
	if err != nil || !ok || meta.CustomTitle != "Named Session" || meta.TopicTitle != "Renamed Topic" {
		t.Fatalf("renamed sidecar = %+v, %v, ok=%v", meta, err, ok)
	}
	topics, err := f.catalog.ListTopics(context.Background(), protocol.TopicListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
	if err != nil || len(topics.Items) != 1 || topics.Items[0].SessionCount != 1 || topics.Items[0].Title != "Renamed Topic" {
		t.Fatalf("ListTopics = %+v, %v", topics, err)
	}
	_, err = f.catalog.DeleteTopic(protocol.TopicDeleteParams{
		HostMutation: protocol.HostMutation{RequestID: "delete_nonempty", ExpectedHostEpoch: testHostEpoch},
		WorkspaceID:  workspaceID,
		TopicID:      topicID,
	})
	requireCatalogCode(t, err, protocol.ErrTopicNotEmpty)

	trashed, err := f.catalog.TrashSession(protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "trash_session", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
		Guard:                 protocol.TrashNormal,
	})
	if err != nil || trashed.Disposition != protocol.DispositionTrashed {
		t.Fatalf("TrashSession = %+v, %v", trashed, err)
	}
	if _, err := os.Lstat(created.SessionPath); !os.IsNotExist(err) {
		t.Fatalf("live transcript still exists after trash: %v", err)
	}
	record := f.catalog.state.Sessions[created.Target.SessionID]
	if record.TrashPath == "" {
		t.Fatal("catalog did not persist trash path")
	}
	requireArtifactSet(t, record.TrashPath)
	_, err = f.catalog.ResolveRuntimeTarget(context.Background(), created.Target)
	requireCatalogCode(t, err, protocol.ErrSessionTrashed)
	trash, err := f.catalog.ListTrash(context.Background(), protocol.SessionTrashListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
	if err != nil || len(trash.Items) != 1 || trash.Items[0].Target != created.Target || trash.Items[0].Title != "Named Session" {
		t.Fatalf("ListTrash = %+v, %v", trash, err)
	}

	restored, err := f.catalog.RestoreSession(protocol.SessionRestoreParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "restore_session", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
	})
	if err != nil || restored.Target != created.Target || restored.TopicID != topicID || restored.Disposition != protocol.SessionRestored {
		t.Fatalf("RestoreSession = %+v, %v", restored, err)
	}
	requireArtifactSet(t, created.SessionPath)
	resolved, err := f.catalog.ResolveRuntimeTarget(context.Background(), created.Target)
	if err != nil || resolved.SessionPath != created.SessionPath {
		t.Fatalf("resolved restored Session = %+v, %v", resolved, err)
	}

	if _, err := f.catalog.TrashSession(protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "trash_again", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
		Guard:                 protocol.TrashNormal,
	}); err != nil {
		t.Fatal(err)
	}
	record = f.catalog.state.Sessions[created.Target.SessionID]
	trashContainerPath := filepath.Dir(record.TrashPath)
	purged, err := f.catalog.PurgeSession(protocol.SessionPurgeParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "purge", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
		Guard:                 protocol.TrashNormal,
	})
	if err != nil || !purged.Purged {
		t.Fatalf("PurgeSession = %+v, %v", purged, err)
	}
	if _, exists := f.catalog.state.Sessions[created.Target.SessionID]; exists || !f.catalog.state.RetiredSessionIDs[created.Target.SessionID] {
		t.Fatalf("purged identity was not retired: %+v", f.catalog.state)
	}
	if _, err := os.Lstat(trashContainerPath); !os.IsNotExist(err) {
		t.Fatalf("purged artifact container remains: %v", err)
	}
}

func TestPurgedSessionIdentityIsNeverReimported(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	topicID := f.createTopic(workspaceID, "Retirement")
	created := f.createSession(workspaceID, topicID)
	if _, err := f.catalog.TrashSession(protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "trash", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
		Guard:                 protocol.TrashNormal,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.catalog.PurgeSession(protocol.SessionPurgeParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "purge", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
		Guard:                 protocol.TrashNormal,
	}); err != nil {
		t.Fatal(err)
	}

	legacy := filepath.Join(f.sessionDir, "copied-after-purge.jsonl")
	if err := os.WriteFile(legacy, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	meta := agent.BranchMeta{
		RemoteSessionID:      string(created.Target.SessionID),
		WorkspaceRoot:        f.workspace,
		TopicID:              string(topicID),
		TopicTitle:           "Retirement",
		Model:                testProfile().Model,
		Effort:               testProfile().Effort,
		Mode:                 string(testProfile().CollaborationMode),
		TokenMode:            string(testProfile().TokenMode),
		ToolApprovalMode:     string(testProfile().ToolApprovalMode),
		RemoteProfileVersion: 1,
	}
	if err := agent.SaveBranchMetaPreserveUpdated(legacy, meta); err != nil {
		t.Fatal(err)
	}
	listed, err := f.catalog.ListSessions(context.Background(), protocol.SessionListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
	if err != nil || len(listed.Items) != 1 {
		t.Fatalf("ListSessions = %+v, %v", listed, err)
	}
	if listed.Items[0].Target.SessionID == created.Target.SessionID {
		t.Fatalf("purged Session ID %s was reused", created.Target.SessionID)
	}
	stored, ok, err := agent.LoadBranchMeta(legacy)
	if err != nil || !ok || stored.RemoteSessionID == string(created.Target.SessionID) {
		t.Fatalf("reimported sidecar = %+v, %v, ok=%v", stored, err, ok)
	}
}

func TestTopicTrashIsAllOrNothingAndRestoreReopensTopic(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	topicID := f.createTopic(workspaceID, "Cascade")
	first := f.createSession(workspaceID, topicID)
	second := f.createSession(workspaceID, topicID)

	busyGuard, err := agent.TryAcquireSessionRemovalGuard(second.SessionPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.catalog.TrashTopic(protocol.TopicTrashParams{
		HostMutation: protocol.HostMutation{RequestID: "topic_trash_busy", ExpectedHostEpoch: testHostEpoch},
		WorkspaceID:  workspaceID,
		TopicID:      topicID,
	})
	busyGuard.Release()
	requireCatalogCode(t, err, protocol.ErrSessionBusy)
	for _, path := range []string{first.SessionPath, second.SessionPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("all-or-nothing preflight moved %s: %v", path, err)
		}
	}

	result, err := f.catalog.TrashTopic(protocol.TopicTrashParams{
		HostMutation: protocol.HostMutation{RequestID: "topic_trash", ExpectedHostEpoch: testHostEpoch},
		WorkspaceID:  workspaceID,
		TopicID:      topicID,
	})
	if err != nil || result.Disposition != protocol.DispositionTrashed || result.TrashedSessions != 2 {
		t.Fatalf("TrashTopic = %+v, %v", result, err)
	}
	topics, err := f.catalog.ListTopics(context.Background(), protocol.TopicListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
	if err != nil || len(topics.Items) != 0 {
		t.Fatalf("trashed Topic remained visible: %+v, %v", topics, err)
	}
	trash, err := f.catalog.ListTrash(context.Background(), protocol.SessionTrashListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
	if err != nil || len(trash.Items) != 2 {
		t.Fatalf("topic trash entries = %+v, %v", trash, err)
	}
	if _, err := f.catalog.RestoreSession(protocol.SessionRestoreParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "restore_one", ExpectedHostEpoch: testHostEpoch, Target: first.Target},
	}); err != nil {
		t.Fatal(err)
	}
	topics, err = f.catalog.ListTopics(context.Background(), protocol.TopicListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
	if err != nil || len(topics.Items) != 1 || topics.Items[0].TopicID != topicID || topics.Items[0].SessionCount != 1 {
		t.Fatalf("restored Topic = %+v, %v", topics, err)
	}
}

func TestTrashRestorePersistsAcrossDaemonRestart(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	topicID := f.createTopic(workspaceID, "Restart")
	created := f.createSession(workspaceID, topicID)
	if _, err := f.catalog.TrashSession(protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "trash_restart", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
		Guard:                 protocol.TrashNormal,
	}); err != nil {
		t.Fatal(err)
	}
	f.catalog = f.reopen(testHostEpoch)
	trash, err := f.catalog.ListTrash(context.Background(), protocol.SessionTrashListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
	if err != nil || len(trash.Items) != 1 || trash.Items[0].Target != created.Target {
		t.Fatalf("restarted trash = %+v, %v", trash, err)
	}
	if _, err := f.catalog.RestoreSession(protocol.SessionRestoreParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "restore_restart", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.catalog.ResolveRuntimeTarget(context.Background(), created.Target); err != nil {
		t.Fatalf("restored target after restart: %v", err)
	}
}

func TestRestoreCollisionPreservesForeignFileAndOpaqueIdentity(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	topicID := f.createTopic(workspaceID, "Collision")
	created := f.createSession(workspaceID, topicID)
	original := created.SessionPath
	if _, err := f.catalog.TrashSession(protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "trash_collision", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
		Guard:                 protocol.TrashNormal,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.catalog.RestoreSession(protocol.SessionRestoreParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "restore_collision", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(original)
	if err != nil || string(b) != "foreign" {
		t.Fatalf("foreign collision file changed: %q, %v", b, err)
	}
	resolved, err := f.catalog.ResolveRuntimeTarget(context.Background(), created.Target)
	if err != nil || resolved.SessionPath == original || resolved.Target != created.Target {
		t.Fatalf("collision restore = %+v, %v", resolved, err)
	}
	meta, ok, err := agent.LoadBranchMeta(resolved.SessionPath)
	if err != nil || !ok || meta.RemoteSessionID != string(created.Target.SessionID) || meta.ID != agent.BranchID(resolved.SessionPath) {
		t.Fatalf("collision sidecar = %+v, %v, ok=%v", meta, err, ok)
	}
}

func TestRedundantRecoveryGuardIsRecheckedAtMutationTime(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	topicID := f.createTopic(workspaceID, "Guard")
	created := f.createSession(workspaceID, topicID)
	_, err := f.catalog.TrashSession(protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "guarded_trash", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
		Guard:                 protocol.TrashRedundantRecoveryOnly,
	})
	requireCatalogCode(t, err, protocol.ErrRecoveryGuardFailed)
	if _, err := os.Stat(created.SessionPath); err != nil {
		t.Fatalf("failed recovery guard changed Session: %v", err)
	}
}

type activeRuntimeInspector struct {
	target protocol.RuntimeTarget
}

func (*activeRuntimeInspector) WorkspaceInUse(protocol.WorkspaceID) bool { return true }

func (i *activeRuntimeInspector) SessionSummary(target protocol.RuntimeTarget) (*protocol.SessionRuntimeSummary, bool) {
	if target != i.target {
		return nil, false
	}
	return &protocol.SessionRuntimeSummary{RuntimeEpoch: "runtime_active", Running: true}, true
}

func TestColdSessionMutationRequiresRuntimeRelease(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	topicID := f.createTopic(workspaceID, "Active")
	created := f.createSession(workspaceID, topicID)
	f.catalog.runtimeInspector = &activeRuntimeInspector{target: created.Target}
	_, err := f.catalog.TrashSession(protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "trash_active", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
		Guard:                 protocol.TrashNormal,
	})
	requireCatalogCode(t, err, protocol.ErrSessionBusy)
	if _, err := os.Stat(created.SessionPath); err != nil {
		t.Fatalf("busy cold mutation changed transcript: %v", err)
	}
}

func TestTrashRejectsSymlinkedInternalRoot(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	topicID := f.createTopic(workspaceID, "Symlink")
	created := f.createSession(workspaceID, topicID)
	outside := filepath.Join(filepath.Dir(f.sessionDir), "outside-trash")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(f.sessionDir, ".remote-trash")); err != nil {
		t.Fatal(err)
	}
	_, err := f.catalog.TrashSession(protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "trash_symlink", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
		Guard:                 protocol.TrashNormal,
	})
	requireCatalogCode(t, err, protocol.ErrSessionPersistFailed)
	if _, err := os.Stat(created.SessionPath); err != nil {
		t.Fatalf("symlink rejection moved transcript: %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink target was modified: %v, %v", entries, err)
	}
}

func TestTrashRegistryFailureRollsBackArtifactsAndLeavesNoPoisonedDestination(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	topicID := f.createTopic(workspaceID, "Rollback")
	created := f.createSession(workspaceID, topicID)
	writeArtifactSet(t, created.SessionPath)

	// Replacing the registry file with a directory makes the final atomic
	// ReplaceFile fail after artifacts have moved, exercising the rollback path
	// without an injectable mock filesystem.
	if err := os.Remove(f.catalog.statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.catalog.statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := f.catalog.TrashSession(protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "trash_registry_failure", ExpectedHostEpoch: testHostEpoch, Target: created.Target},
		Guard:                 protocol.TrashNormal,
	})
	requireCatalogCode(t, err, protocol.ErrSessionPersistFailed)
	requireArtifactSet(t, created.SessionPath)
	record := f.catalog.state.Sessions[created.Target.SessionID]
	if record.TrashPath != "" {
		t.Fatalf("failed mutation changed catalog record: %+v", record)
	}
	container := trashContainer(f.sessionDir, created.Target.SessionID)
	if _, err := os.Lstat(container); !os.IsNotExist(err) {
		t.Fatalf("failed mutation left poisoned trash destination: %v", err)
	}
}

func TestConcurrentTopicAndSessionCreationHasNoIdentityCollisions(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	const count = 40
	topicIDs := make(chan protocol.TopicID, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for index := 0; index < count; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			result, err := f.catalog.CreateTopic(protocol.TopicCreateParams{
				HostMutation: protocol.HostMutation{RequestID: protocol.RequestID(fmt.Sprintf("concurrent_topic_%d", index)), ExpectedHostEpoch: testHostEpoch},
				WorkspaceID:  workspaceID,
				Title:        fmt.Sprintf("Topic %d", index),
			})
			if err != nil {
				errs <- err
				return
			}
			topicIDs <- result.TopicID
		}(index)
	}
	wg.Wait()
	close(topicIDs)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	ids := make([]protocol.TopicID, 0, count)
	for id := range topicIDs {
		ids = append(ids, id)
	}
	if len(ids) != count {
		t.Fatalf("created %d Topics, want %d", len(ids), count)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for index := 1; index < len(ids); index++ {
		if ids[index] == ids[index-1] {
			t.Fatalf("duplicate Topic ID %s", ids[index])
		}
	}

	sessionIDs := make(chan protocol.SessionID, count)
	errs = make(chan error, count)
	wg = sync.WaitGroup{}
	for index := 0; index < count; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			created, err := f.catalog.CreateSession(context.Background(), protocol.SessionCreateParams{
				HostMutation: protocol.HostMutation{RequestID: protocol.RequestID(fmt.Sprintf("concurrent_session_%d", index)), ExpectedHostEpoch: testHostEpoch},
				WorkspaceID:  workspaceID,
				Topic:        protocol.TopicSelection{Kind: protocol.TopicExisting, TopicID: ids[index]},
				Profile:      protocol.ProfileSelection{},
			})
			if err != nil {
				errs <- err
				return
			}
			sessionIDs <- created.Target.SessionID
		}(index)
	}
	wg.Wait()
	close(sessionIDs)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := make(map[protocol.SessionID]bool)
	for id := range sessionIDs {
		if seen[id] {
			t.Fatalf("duplicate Session ID %s", id)
		}
		seen[id] = true
	}
	if len(seen) != count {
		t.Fatalf("created %d Sessions, want %d", len(seen), count)
	}
}

func writeArtifactSet(t *testing.T, sessionPath string) {
	t.Helper()
	for _, path := range []string{
		store.SessionGoalState(sessionPath),
		store.SessionEventLog(sessionPath),
		store.SessionEventIndex(sessionPath),
		store.SessionConflictLog(sessionPath),
	} {
		if err := os.WriteFile(path, []byte("artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{store.SessionCheckpointDir(sessionPath), store.SessionJobsDir(sessionPath)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "artifact"), []byte("artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func requireArtifactSet(t *testing.T, sessionPath string) {
	t.Helper()
	paths := append([]string{sessionPath}, store.SessionSidecarFiles(sessionPath)...)
	paths = append(paths, store.SessionCheckpointDir(sessionPath), store.SessionJobsDir(sessionPath))
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("artifact %s is missing: %v", path, err)
		}
	}
}

func TestColdGuardErrorClassification(t *testing.T) {
	if got, ok := ErrorCode(coldGuardError(agent.ErrSessionLeaseHeld)); !ok || got != protocol.ErrSessionBusy {
		t.Fatalf("lease error code = %q, %v", got, ok)
	}
	plain := errors.New("disk failure")
	if got, ok := ErrorCode(coldGuardError(plain)); !ok || got != protocol.ErrSessionPersistFailed {
		t.Fatalf("plain error code = %q, %v", got, ok)
	}
}
