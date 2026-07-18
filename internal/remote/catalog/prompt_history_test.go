package catalog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/remote/protocol"
)

func TestPromptHistorySessionsMapsOnlyValidatedLiveWorkspaceSessions(t *testing.T) {
	fixture := newCatalogFixture(t)
	workspaceID := fixture.openWorkspace()
	topicID := fixture.createTopic(workspaceID, "History")
	live := fixture.createSession(workspaceID, topicID)
	trashed := fixture.createSession(workspaceID, topicID)
	if _, err := fixture.catalog.TrashSession(protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{
			RequestID: "trash-history", ExpectedHostEpoch: testHostEpoch, Target: trashed.Target,
		},
		Guard: protocol.TrashNormal,
	}); err != nil {
		t.Fatal(err)
	}

	mapped, err := fixture.catalog.PromptHistorySessions(context.Background(), testHostEpoch, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapped) != 1 || mapped[0].Target != live.Target || mapped[0].SessionPath != live.SessionPath ||
		mapped[0].SessionDir != filepath.Dir(live.SessionPath) {
		t.Fatalf("prompt-history mapping = %+v", mapped)
	}
	if string(mapped[0].Target.SessionID) == mapped[0].SessionPath || string(mapped[0].Target.WorkspaceID) == fixture.workspace {
		t.Fatalf("opaque target leaked a Host path: %+v", mapped[0])
	}
	wireProbe, err := json.Marshal(mapped)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wireProbe), live.SessionPath) || strings.Contains(string(wireProbe), fixture.sessionDir) {
		t.Fatalf("Host-only mapping serialized a path: %s", wireProbe)
	}

	_, err = fixture.catalog.PromptHistorySessions(context.Background(), "stale-host", workspaceID)
	requireCatalogCode(t, err, protocol.ErrStaleHostEpoch)
	_, err = fixture.catalog.PromptHistorySessions(context.Background(), testHostEpoch, "workspace-missing")
	requireCatalogCode(t, err, protocol.ErrWorkspaceNotFound)
}

func TestPromptHistorySessionsRejectsUnsafeCatalogArtifact(t *testing.T) {
	fixture := newCatalogFixture(t)
	workspaceID := fixture.openWorkspace()
	topicID := fixture.createTopic(workspaceID, "Unsafe")
	created := fixture.createSession(workspaceID, topicID)
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(created.SessionPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, created.SessionPath); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.catalog.PromptHistorySessions(context.Background(), testHostEpoch, workspaceID)
	requireCatalogCode(t, err, protocol.ErrSessionPersistFailed)
}
