package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/remote/protocol"
)

func TestCreateSiblingFreezesSourceProfileDirectoriesTopicAndDropsGoal(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	topicID := f.createTopic(workspaceID, "Lifecycle")
	additionalPath := filepath.Join(filepath.Dir(f.workspace), "additional")
	if err := os.MkdirAll(additionalPath, 0o700); err != nil {
		t.Fatal(err)
	}
	browsed, err := f.catalog.Browse(protocol.WorkspaceBrowseParams{ExpectedHostEpoch: testHostEpoch, TypedPath: additionalPath})
	if err != nil {
		t.Fatal(err)
	}
	source := f.createSession(workspaceID, topicID, browsed.Directory.DirectoryRef)
	if _, err := agent.UpdateBranchMetaPreserveUpdated(source.SessionPath, func(meta *agent.BranchMeta) error {
		meta.Goal = "must not copy"
		meta.CustomTitle = "source title"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	created, err := f.catalog.CreateSiblingSession(context.Background(), testHostEpoch, source.Target)
	if err != nil {
		t.Fatal(err)
	}
	if created.Target == source.Target || created.TopicID != topicID || created.ResolvedProfile != source.ResolvedProfile {
		t.Fatalf("sibling = %+v, source = %+v", created, source)
	}
	if len(created.AdditionalDirs) != 1 || pathKey(created.AdditionalDirs[0]) != pathKey(additionalPath) {
		t.Fatalf("sibling additional dirs = %q", created.AdditionalDirs)
	}
	meta, ok, err := agent.LoadBranchMeta(created.SessionPath)
	if err != nil || !ok {
		t.Fatalf("load sibling meta: ok=%v err=%v", ok, err)
	}
	if meta.Goal != "" || meta.CustomTitle != "" || meta.RemoteSessionID != string(created.Target.SessionID) {
		t.Fatalf("sibling metadata leaked source state: %+v", meta)
	}
	if _, err := f.catalog.ResolveRuntimeTarget(context.Background(), source.Target); err != nil {
		t.Fatalf("session/new retired its source: %v", err)
	}
	if _, err := f.catalog.ResolveRuntimeTarget(context.Background(), created.Target); err != nil {
		t.Fatalf("resolve sibling: %v", err)
	}
}

func TestClearTransitionRollbackAndCleanupPendingAreDurable(t *testing.T) {
	t.Run("rollback restores only old identity", func(t *testing.T) {
		f := newCatalogFixture(t)
		workspaceID := f.openWorkspace()
		source := f.createSession(workspaceID, f.createTopic(workspaceID, "Clear"))
		transition, err := f.catalog.BeginClear(context.Background(), testHostEpoch, source.Target)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.catalog.ResolveRuntimeTarget(context.Background(), source.Target); err == nil {
			t.Fatal("retired source still resolved during clear transition")
		}
		if err := transition.Rollback(); err != nil {
			t.Fatal(err)
		}
		if meta, ok, metaErr := agent.LoadBranchMeta(source.SessionPath); metaErr != nil || !ok || meta.RemoteSessionID != string(source.Target.SessionID) {
			t.Fatalf("source meta after rollback = %+v ok=%v err=%v", meta, ok, metaErr)
		}
		if _, err := f.catalog.ResolveRuntimeTarget(context.Background(), source.Target); err != nil {
			t.Fatalf("source not restored: %v", err)
		}
		if _, err := f.catalog.ResolveRuntimeTarget(context.Background(), transition.Replacement.Target); err == nil {
			t.Fatal("rolled-back replacement still resolved")
		}
		if _, err := os.Stat(transition.Replacement.SessionPath); !os.IsNotExist(err) {
			t.Fatalf("rolled-back replacement artifact remains: %v", err)
		}
	})

	t.Run("cleanup failure keeps identities final and marks pending", func(t *testing.T) {
		f := newCatalogFixture(t)
		workspaceID := f.openWorkspace()
		source := f.createSession(workspaceID, f.createTopic(workspaceID, "Clear pending"))
		transition, err := f.catalog.BeginClear(context.Background(), testHostEpoch, source.Target)
		if err != nil {
			t.Fatal(err)
		}
		f.catalog.removeAll = func(path string) error {
			if pathKey(path) == pathKey(source.SessionPath) {
				return errors.New("injected cleanup failure")
			}
			return os.RemoveAll(path)
		}
		disposition, err := transition.CleanupPrevious()
		if disposition != protocol.SessionCleanupPending || err == nil {
			t.Fatalf("cleanup = %q, %v", disposition, err)
		}
		if !agent.IsCleanupPending(source.SessionPath) {
			t.Fatal("cleanup_pending did not leave durable marker")
		}
		if _, err := f.catalog.ResolveRuntimeTarget(context.Background(), source.Target); err == nil {
			t.Fatal("cleanup_pending resurrected retired source")
		}
		if _, err := f.catalog.ResolveRuntimeTarget(context.Background(), transition.Replacement.Target); err != nil {
			t.Fatalf("replacement not durable: %v", err)
		}
	})
}

func TestAdoptForkPersistsOpaqueRemoteAncestryAndRollbackCleans(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	source := f.createSession(workspaceID, f.createTopic(workspaceID, "Fork"))
	forkPath := filepath.Join(f.sessionDir, "fork.jsonl")
	if err := agent.NewSession("system").Save(forkPath); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(forkPath, agent.BranchMeta{Name: "child", ParentID: "local-parent"}); err != nil {
		t.Fatal(err)
	}
	created, err := f.catalog.AdoptFork(context.Background(), testHostEpoch, source.Target, "checkpoint-opaque", forkPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.catalog.state.Sessions) != 2 {
		t.Fatalf("fork adoption allocated duplicate identities: %+v", f.catalog.state.Sessions)
	}
	meta, ok, err := agent.LoadBranchMeta(forkPath)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if meta.RemoteParentSessionID != string(source.Target.SessionID) || meta.RemoteParentCheckpointID != "checkpoint-opaque" || meta.ParentID != "local-parent" {
		t.Fatalf("fork ancestry = %+v", meta)
	}
	if err := f.catalog.RollbackCreatedSession(created.Target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(forkPath); !os.IsNotExist(err) {
		t.Fatalf("failed fork cleanup left transcript: %v", err)
	}
	if !f.catalog.state.RetiredSessionIDs[created.Target.SessionID] {
		t.Fatal("rolled-back child identity was not permanently retired")
	}
	if len(f.catalog.state.Sessions) != 1 || f.catalog.state.Sessions[source.Target.SessionID].Path != source.SessionPath {
		t.Fatalf("fork rollback left catalog records: %+v", f.catalog.state.Sessions)
	}
}

func TestAdoptTipBranchRetainsLocalAncestryWithoutInventingCheckpoint(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	source := f.createSession(workspaceID, f.createTopic(workspaceID, "Branch"))
	branchPath := filepath.Join(f.sessionDir, "branch.jsonl")
	if err := agent.NewSession("system").Save(branchPath); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(branchPath, agent.BranchMeta{
		Name: "tip", ParentID: "local-parent", ForkTurn: -1, ForkMessageIndex: 7,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := f.catalog.AdoptBranch(context.Background(), testHostEpoch, source.Target, branchPath)
	if err != nil {
		t.Fatal(err)
	}
	meta, ok, err := agent.LoadBranchMeta(branchPath)
	if err != nil || !ok || meta.RemoteParentSessionID != string(source.Target.SessionID) ||
		meta.RemoteParentCheckpointID != "" || meta.ParentID != "local-parent" || meta.ForkMessageIndex != 7 {
		t.Fatalf("tip branch ancestry = %+v ok=%v err=%v", meta, ok, err)
	}
	if len(f.catalog.state.Sessions) != 2 || f.catalog.state.Sessions[created.Target.SessionID].Path != branchPath {
		t.Fatalf("tip branch catalog = %+v", f.catalog.state.Sessions)
	}
}

func TestProfileTransitionMergesWholeProfileAndRollsBack(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	source := f.createSession(workspaceID, f.createTopic(workspaceID, "Profile"))
	plan := protocol.CollaborationPlan
	yolo := protocol.ToolApprovalYOLO
	transition, err := f.catalog.BeginProfilePatch(context.Background(), testHostEpoch, source.Target, protocol.ProfilePatch{
		CollaborationMode: &plan, ToolApprovalMode: &yolo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.Current.Model != source.ResolvedProfile.Model || transition.Current.Effort != source.ResolvedProfile.Effort ||
		transition.Current.CollaborationMode != plan || transition.Current.ToolApprovalMode != yolo {
		t.Fatalf("merged profile = %+v", transition.Current)
	}
	resolved, err := f.catalog.ResolveRuntimeTarget(context.Background(), source.Target)
	if err != nil || resolved.ResolvedProfile != transition.Current {
		t.Fatalf("persisted profile = %+v, %v", resolved.ResolvedProfile, err)
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
	resolved, err = f.catalog.ResolveRuntimeTarget(context.Background(), source.Target)
	if err != nil || resolved.ResolvedProfile != transition.Previous {
		t.Fatalf("rolled back profile = %+v, %v", resolved.ResolvedProfile, err)
	}

	if _, err := f.catalog.BeginProfilePatch(context.Background(), testHostEpoch, source.Target, protocol.ProfilePatch{}); err == nil {
		t.Fatal("empty profile patch was accepted")
	}
}
