package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/remote/protocol"
)

const testHostEpoch protocol.HostEpoch = "host_epoch_test"

type catalogFixture struct {
	t          *testing.T
	stateDir   string
	home       string
	workspace  string
	sessionDir string
	catalog    *Catalog
	now        time.Time
}

func newCatalogFixture(t *testing.T) *catalogFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workspace := filepath.Join(root, "workspace")
	sessionDir := filepath.Join(root, "state", "sessions")
	for _, dir := range []string{home, workspace, sessionDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f := &catalogFixture{
		t:          t,
		stateDir:   filepath.Join(root, "catalog"),
		home:       home,
		workspace:  workspace,
		sessionDir: sessionDir,
		now:        time.Unix(1_800_000_000, 0).UTC(),
	}
	f.catalog = f.reopen(testHostEpoch)
	return f
}

func (f *catalogFixture) reopen(epoch protocol.HostEpoch) *Catalog {
	f.t.Helper()
	sequence := 0
	c, err := New(epoch, Options{
		StateDir: f.stateDir,
		UserHome: f.home,
		SessionDir: func(root string) string {
			if pathKey(root) != pathKey(f.workspace) {
				f.t.Fatalf("SessionDir root = %q, want %q", root, f.workspace)
			}
			return f.sessionDir
		},
		NewOpaqueID: func(kind string) (string, error) {
			sequence++
			return fmt.Sprintf("%s_test_%04d", kind, sequence), nil
		},
		Now: func() time.Time {
			return f.now
		},
		ProfileResolver: ProfileResolverFunc(func(_ context.Context, root string, selection protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
			if pathKey(root) != pathKey(f.workspace) {
				return protocol.ResolvedProfile{}, fmt.Errorf("wrong workspace: %s", root)
			}
			profile := testProfile()
			if selection.Model != nil {
				profile.Model = strings.TrimSpace(*selection.Model)
			}
			if selection.Effort != nil {
				profile.Effort = strings.TrimSpace(*selection.Effort)
			}
			if selection.CollaborationMode != nil {
				profile.CollaborationMode = *selection.CollaborationMode
			}
			if selection.TokenMode != nil {
				profile.TokenMode = *selection.TokenMode
			}
			if selection.ToolApprovalMode != nil {
				profile.ToolApprovalMode = *selection.ToolApprovalMode
			}
			return profile, nil
		}),
	})
	if err != nil {
		f.t.Fatalf("New catalog: %v", err)
	}
	return c
}

func testProfile() protocol.ResolvedProfile {
	return protocol.ResolvedProfile{
		Model:             "test/provider-model",
		Effort:            "medium",
		CollaborationMode: protocol.CollaborationNormal,
		TokenMode:         protocol.TokenFull,
		ToolApprovalMode:  protocol.ToolApprovalAsk,
	}
}

func (f *catalogFixture) openWorkspace() protocol.WorkspaceID {
	f.t.Helper()
	browsed, err := f.catalog.Browse(protocol.WorkspaceBrowseParams{ExpectedHostEpoch: testHostEpoch, TypedPath: f.workspace})
	if err != nil {
		f.t.Fatalf("Browse workspace: %v", err)
	}
	opened, err := f.catalog.OpenWorkspace(protocol.WorkspaceOpenParams{
		HostMutation:        protocol.HostMutation{RequestID: "request_open", ExpectedHostEpoch: testHostEpoch},
		PrimaryDirectoryRef: browsed.Directory.DirectoryRef,
	})
	if err != nil {
		f.t.Fatalf("OpenWorkspace: %v", err)
	}
	return opened.Workspace.WorkspaceID
}

func (f *catalogFixture) createTopic(workspaceID protocol.WorkspaceID, title string) protocol.TopicID {
	f.t.Helper()
	result, err := f.catalog.CreateTopic(protocol.TopicCreateParams{
		HostMutation: protocol.HostMutation{RequestID: protocol.RequestID("request_topic_" + title), ExpectedHostEpoch: testHostEpoch},
		WorkspaceID:  workspaceID,
		Title:        title,
	})
	if err != nil {
		f.t.Fatalf("CreateTopic: %v", err)
	}
	return result.TopicID
}

func (f *catalogFixture) createSession(workspaceID protocol.WorkspaceID, topicID protocol.TopicID, additional ...protocol.DirectoryRef) CreatedSession {
	f.t.Helper()
	result, err := f.catalog.CreateSession(context.Background(), protocol.SessionCreateParams{
		HostMutation:            protocol.HostMutation{RequestID: protocol.RequestID(fmt.Sprintf("request_session_%d", len(f.catalog.state.Sessions))), ExpectedHostEpoch: testHostEpoch},
		WorkspaceID:             workspaceID,
		AdditionalDirectoryRefs: append([]protocol.DirectoryRef{}, additional...),
		Topic:                   protocol.TopicSelection{Kind: protocol.TopicExisting, TopicID: topicID},
		Profile:                 protocol.ProfileSelection{},
	})
	if err != nil {
		f.t.Fatalf("CreateSession: %v", err)
	}
	return result
}

func requireCatalogCode(t *testing.T, err error, want protocol.ReasonixErrorCode) {
	t.Helper()
	got, ok := ErrorCode(err)
	if !ok || got != want {
		t.Fatalf("error = %v (code %q, ok %v), want %s", err, got, ok, want)
	}
}

func TestWorkspaceIdentityCanonicalPersistentAndEpochScoped(t *testing.T) {
	f := newCatalogFixture(t)
	alias := filepath.Join(filepath.Dir(f.workspace), "workspace-alias")
	if err := os.Symlink(f.workspace, alias); err != nil {
		t.Fatal(err)
	}
	browse, err := f.catalog.Browse(protocol.WorkspaceBrowseParams{ExpectedHostEpoch: testHostEpoch, TypedPath: alias})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := f.catalog.OpenWorkspace(protocol.WorkspaceOpenParams{
		HostMutation:        protocol.HostMutation{RequestID: "open_1", ExpectedHostEpoch: testHostEpoch},
		PrimaryDirectoryRef: browse.Directory.DirectoryRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := opened.Workspace.WorkspaceID
	if workspaceID == "" || string(workspaceID) == f.workspace || opened.Workspace.DisplayPath != f.workspace {
		t.Fatalf("opened workspace = %+v", opened)
	}
	closed, err := f.catalog.CloseWorkspace(protocol.WorkspaceCloseParams{
		HostMutation: protocol.HostMutation{RequestID: "close_1", ExpectedHostEpoch: testHostEpoch},
		WorkspaceID:  workspaceID,
	})
	if err != nil || closed.Disposition != protocol.WorkspaceClosed {
		t.Fatalf("CloseWorkspace = %+v, %v", closed, err)
	}
	reopened, err := f.catalog.OpenWorkspace(protocol.WorkspaceOpenParams{
		HostMutation:        protocol.HostMutation{RequestID: "open_2", ExpectedHostEpoch: testHostEpoch},
		PrimaryDirectoryRef: browse.Directory.DirectoryRef,
	})
	if err != nil || reopened.Workspace.WorkspaceID != workspaceID {
		t.Fatalf("reopen = %+v, %v; want stable %s", reopened, err, workspaceID)
	}

	f.catalog = f.reopen("host_epoch_next")
	listed, err := f.catalog.ListWorkspaces(protocol.WorkspaceListParams{ExpectedHostEpoch: "host_epoch_next"})
	if err != nil || len(listed.Items) != 1 || listed.Items[0].WorkspaceID != workspaceID {
		t.Fatalf("persistent list = %+v, %v", listed, err)
	}
	_, err = f.catalog.OpenWorkspace(protocol.WorkspaceOpenParams{
		HostMutation:        protocol.HostMutation{RequestID: "open_stale", ExpectedHostEpoch: "host_epoch_next"},
		PrimaryDirectoryRef: browse.Directory.DirectoryRef,
	})
	requireCatalogCode(t, err, protocol.ErrStaleDirectoryRef)
}

func TestBrowseArbitraryExistingDirectoryAndCursorDoesNotCrossEpoch(t *testing.T) {
	f := newCatalogFixture(t)
	outside := filepath.Join(filepath.Dir(f.home), "not-under-home")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if err := os.Mkdir(filepath.Join(outside, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	limit := 1
	first, err := f.catalog.Browse(protocol.WorkspaceBrowseParams{ExpectedHostEpoch: testHostEpoch, TypedPath: outside, Limit: &limit})
	if err != nil || !first.HasMore || first.NextCursor == "" || first.Directory.DisplayPath != outside {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	second, err := f.catalog.Browse(protocol.WorkspaceBrowseParams{ExpectedHostEpoch: testHostEpoch, Cursor: first.NextCursor, Limit: &limit})
	if err != nil || len(second.Entries) != 1 || second.Entries[0].Name == first.Entries[0].Name {
		t.Fatalf("second page = %+v, %v", second, err)
	}
	f.catalog = f.reopen("epoch_cursor_reset")
	_, err = f.catalog.Browse(protocol.WorkspaceBrowseParams{ExpectedHostEpoch: "epoch_cursor_reset", Cursor: first.NextCursor})
	requireCatalogCode(t, err, protocol.ErrStaleCursor)
}

func TestBrowseClassifiesUnreadableDirectory(t *testing.T) {
	f := newCatalogFixture(t)
	blocked := filepath.Join(filepath.Dir(f.home), "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })
	_, err := f.catalog.Browse(protocol.WorkspaceBrowseParams{ExpectedHostEpoch: testHostEpoch, TypedPath: blocked})
	if err == nil {
		// Privileged CI users can bypass mode bits; the classification is covered
		// on normal unprivileged Linux runners.
		t.Skip("current test user can read mode-000 directories")
	}
	requireCatalogCode(t, err, protocol.ErrPermissionDenied)
}

func TestConcurrentWorkspaceOpenAllocatesOneStableIdentity(t *testing.T) {
	f := newCatalogFixture(t)
	browse, err := f.catalog.Browse(protocol.WorkspaceBrowseParams{ExpectedHostEpoch: testHostEpoch, TypedPath: f.workspace})
	if err != nil {
		t.Fatal(err)
	}
	const workers = 64
	ids := make(chan protocol.WorkspaceID, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			result, err := f.catalog.OpenWorkspace(protocol.WorkspaceOpenParams{
				HostMutation:        protocol.HostMutation{RequestID: protocol.RequestID(fmt.Sprintf("open_%d", index)), ExpectedHostEpoch: testHostEpoch},
				PrimaryDirectoryRef: browse.Directory.DirectoryRef,
			})
			if err != nil {
				errs <- err
				return
			}
			ids <- result.Workspace.WorkspaceID
		}(i)
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var want protocol.WorkspaceID
	for id := range ids {
		if want == "" {
			want = id
		}
		if id != want {
			t.Fatalf("workspace IDs diverged: %s != %s", id, want)
		}
	}
}

func TestLegacySessionMigrationSurvivesRenameAndDuplicateCopy(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	legacyPath := filepath.Join(f.sessionDir, "human-readable-basename.jsonl")
	if err := os.WriteFile(legacyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyMeta := agent.BranchMeta{
		TopicID:       "legacy_topic",
		TopicTitle:    "Legacy topic",
		WorkspaceRoot: f.workspace,
		Model:         "test/provider-model",
	}
	if err := agent.SaveBranchMetaPreserveUpdated(legacyPath, legacyMeta); err != nil {
		t.Fatal(err)
	}
	listed, err := f.catalog.ListSessions(context.Background(), protocol.SessionListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
	if err != nil || len(listed.Items) != 1 {
		t.Fatalf("migrated list = %+v, %v", listed, err)
	}
	sessionID := listed.Items[0].Target.SessionID
	if sessionID == "" || string(sessionID) == agent.BranchID(legacyPath) {
		t.Fatalf("session ID %q was derived from basename %q", sessionID, agent.BranchID(legacyPath))
	}
	meta, ok, err := agent.LoadBranchMeta(legacyPath)
	if err != nil || !ok || meta.RemoteSessionID != string(sessionID) || meta.RemoteProfileVersion != 1 || meta.Effort != "medium" {
		t.Fatalf("migrated meta = %+v, %v, ok=%v", meta, err, ok)
	}

	renamedPath := filepath.Join(f.sessionDir, "renamed-on-host.jsonl")
	if err := os.Rename(legacyPath, renamedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(agent.BranchMetaPath(legacyPath), agent.BranchMetaPath(renamedPath)); err != nil {
		t.Fatal(err)
	}
	listed, err = f.catalog.ListSessions(context.Background(), protocol.SessionListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
	if err != nil || len(listed.Items) != 1 || listed.Items[0].Target.SessionID != sessionID {
		t.Fatalf("renamed list = %+v, %v; want %s", listed, err, sessionID)
	}

	duplicatePath := filepath.Join(f.sessionDir, "copied-session.jsonl")
	copyFile(t, renamedPath, duplicatePath)
	copyFile(t, agent.BranchMetaPath(renamedPath), agent.BranchMetaPath(duplicatePath))
	listed, err = f.catalog.ListSessions(context.Background(), protocol.SessionListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
	if err != nil || len(listed.Items) != 2 {
		t.Fatalf("duplicate list = %+v, %v", listed, err)
	}
	ids := []string{string(listed.Items[0].Target.SessionID), string(listed.Items[1].Target.SessionID)}
	sort.Strings(ids)
	if ids[0] == ids[1] {
		t.Fatalf("duplicate sidecars aliased to %q", ids[0])
	}
	duplicateMeta, ok, err := agent.LoadBranchMeta(duplicatePath)
	if err != nil || !ok || duplicateMeta.RemoteSessionID == string(sessionID) {
		t.Fatalf("duplicate meta = %+v, %v, ok=%v", duplicateMeta, err, ok)
	}

	f.catalog = f.reopen(testHostEpoch)
	reloaded, err := f.catalog.ListSessions(context.Background(), protocol.SessionListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
	if err != nil || len(reloaded.Items) != 2 {
		t.Fatalf("reloaded list = %+v, %v", reloaded, err)
	}
}

func TestLegacyMigrationFreezesApprovalWithoutChangingNewSessionDefaults(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	type observedSelection struct {
		mode     string
		approval *protocol.ToolApprovalMode
	}
	var observed []observedSelection
	f.catalog.profileResolver = ProfileResolverFunc(func(_ context.Context, _ string, selection protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
		entry := observedSelection{}
		if selection.CollaborationMode != nil {
			entry.mode = string(*selection.CollaborationMode)
		}
		if selection.ToolApprovalMode != nil {
			copyValue := *selection.ToolApprovalMode
			entry.approval = &copyValue
		}
		observed = append(observed, entry)
		profile := testProfile()
		switch strings.ToLower(entry.mode) {
		case "plan", "plan-yolo", "yolo-plan":
			profile.CollaborationMode = protocol.CollaborationPlan
		case "goal":
			profile.CollaborationMode = protocol.CollaborationGoal
		default:
			profile.CollaborationMode = protocol.CollaborationNormal
		}
		if entry.approval != nil {
			profile.ToolApprovalMode = *entry.approval
		} else {
			// Deliberately unsafe current user default: migration must override
			// this for old normal/plan Sessions, while new Session creation keeps it.
			profile.ToolApprovalMode = protocol.ToolApprovalYOLO
		}
		if entry.mode == "yolo" || entry.mode == "plan-yolo" || entry.mode == "yolo-plan" {
			profile.ToolApprovalMode = protocol.ToolApprovalYOLO
		}
		return profile, nil
	})

	for _, test := range []struct {
		name string
		mode string
	}{
		{name: "legacy-normal", mode: "normal"},
		{name: "legacy-plan", mode: "plan"},
		{name: "legacy-yolo", mode: "yolo"},
		{name: "legacy-plan-yolo", mode: "plan-yolo"},
	} {
		path := filepath.Join(f.sessionDir, test.name+".jsonl")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
			WorkspaceRoot: f.workspace,
			TopicID:       "topic_" + test.name,
			TopicTitle:    test.name,
			Mode:          test.mode,
		}); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := f.catalog.ListSessions(context.Background(), protocol.SessionListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
	if err != nil || len(listed.Items) != 4 {
		t.Fatalf("legacy ListSessions = %+v, %v", listed, err)
	}
	for _, name := range []string{"legacy-normal", "legacy-plan"} {
		meta, ok, err := agent.LoadBranchMeta(filepath.Join(f.sessionDir, name+".jsonl"))
		if err != nil || !ok || meta.ToolApprovalMode != string(protocol.ToolApprovalAsk) {
			t.Fatalf("%s approval = %+v, %v, ok=%v; want ask", name, meta, err, ok)
		}
	}
	for _, name := range []string{"legacy-yolo", "legacy-plan-yolo"} {
		meta, ok, err := agent.LoadBranchMeta(filepath.Join(f.sessionDir, name+".jsonl"))
		if err != nil || !ok || meta.ToolApprovalMode != string(protocol.ToolApprovalYOLO) {
			t.Fatalf("%s approval = %+v, %v, ok=%v; want yolo", name, meta, err, ok)
		}
	}
	for _, selection := range observed[:4] {
		switch selection.mode {
		case "yolo", "plan-yolo", "yolo-plan":
			if selection.approval != nil {
				t.Fatalf("legacy %s unexpectedly overrode YOLO with %s", selection.mode, *selection.approval)
			}
		default:
			if selection.approval == nil || *selection.approval != protocol.ToolApprovalAsk {
				t.Fatalf("legacy %s approval selection = %v, want explicit ask", selection.mode, selection.approval)
			}
		}
	}

	newTopic := f.createTopic(workspaceID, "new-default")
	created, err := f.catalog.CreateSession(context.Background(), protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "new_default", ExpectedHostEpoch: testHostEpoch},
		WorkspaceID:  workspaceID,
		Topic:        protocol.TopicSelection{Kind: protocol.TopicExisting, TopicID: newTopic},
		Profile:      protocol.ProfileSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ResolvedProfile.ToolApprovalMode != protocol.ToolApprovalYOLO {
		t.Fatalf("new Session approval = %s, want current Host default yolo", created.ResolvedProfile.ToolApprovalMode)
	}
	if last := observed[len(observed)-1]; last.approval != nil {
		t.Fatalf("new Session selection unexpectedly froze approval to %s", *last.approval)
	}
}

func TestCreateSessionPersistsCanonicalAdditionalDirsAndResolvedProfile(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	topicID := f.createTopic(workspaceID, "Additional dirs")
	additionalReal := filepath.Join(filepath.Dir(f.workspace), "additional-real")
	additionalAlias := filepath.Join(filepath.Dir(f.workspace), "additional-alias")
	if err := os.Mkdir(additionalReal, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(additionalReal, additionalAlias); err != nil {
		t.Fatal(err)
	}
	browse, err := f.catalog.Browse(protocol.WorkspaceBrowseParams{ExpectedHostEpoch: testHostEpoch, TypedPath: additionalAlias})
	if err != nil {
		t.Fatal(err)
	}
	created := f.createSession(workspaceID, topicID, browse.Directory.DirectoryRef, browse.Directory.DirectoryRef)
	if len(created.AdditionalDirs) != 1 || created.AdditionalDirs[0] != additionalReal || created.ResolvedProfile != testProfile() {
		t.Fatalf("created Session = %+v", created)
	}
	meta, ok, err := agent.LoadBranchMeta(created.SessionPath)
	if err != nil || !ok || len(meta.AdditionalDirs) != 1 || meta.AdditionalDirs[0] != additionalReal || meta.RemoteProfileVersion != 1 {
		t.Fatalf("persisted meta = %+v, %v, ok=%v", meta, err, ok)
	}
	f.catalog = f.reopen(testHostEpoch)
	resolved, err := f.catalog.ResolveRuntimeTarget(context.Background(), created.Target)
	if err != nil || len(resolved.AdditionalDirs) != 1 || resolved.AdditionalDirs[0] != additionalReal || resolved.ResolvedProfile != testProfile() {
		t.Fatalf("resolved after restart = %+v, %v", resolved, err)
	}
	if err := os.RemoveAll(additionalReal); err != nil {
		t.Fatal(err)
	}
	_, err = f.catalog.ResolveRuntimeTarget(context.Background(), created.Target)
	requireCatalogCode(t, err, protocol.ErrDirectoryNotFound)
}

func TestTargetResolverRejectsPathEscapeAndNeverUsesTargetAsPath(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	topicID := f.createTopic(workspaceID, "Path security")
	created := f.createSession(workspaceID, topicID)
	outside := filepath.Join(filepath.Dir(f.sessionDir), "outside.jsonl")
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f.catalog.mu.Lock()
	record := f.catalog.state.Sessions[created.Target.SessionID]
	record.Path = outside
	f.catalog.state.Sessions[created.Target.SessionID] = record
	f.catalog.mu.Unlock()
	_, err := f.catalog.ResolveRuntimeTarget(context.Background(), created.Target)
	requireCatalogCode(t, err, protocol.ErrSessionPersistFailed)
	if strings.Contains(string(created.Target.SessionID), f.sessionDir) {
		t.Fatalf("opaque target leaked store path: %s", created.Target.SessionID)
	}
}

func TestRegistryAndMetadataSymlinksAreRejected(t *testing.T) {
	t.Run("state directory", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target-state")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		stateLink := filepath.Join(root, "state-link")
		if err := os.Symlink(target, stateLink); err != nil {
			t.Fatal(err)
		}
		if _, err := New(testHostEpoch, Options{StateDir: stateLink, UserHome: root}); err == nil || !strings.Contains(err.Error(), "protect state dir") {
			t.Fatalf("New with state directory symlink error = %v", err)
		}
	})

	t.Run("registry", func(t *testing.T) {
		root := t.TempDir()
		stateDir := filepath.Join(root, "state")
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "target.json")
		if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(stateDir, "catalog-v1.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := New(testHostEpoch, Options{StateDir: stateDir, UserHome: root}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("New with registry symlink error = %v", err)
		}
	})

	t.Run("sidecar", func(t *testing.T) {
		f := newCatalogFixture(t)
		workspaceID := f.openWorkspace()
		path := filepath.Join(f.sessionDir, "legacy.jsonl")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		foreign := filepath.Join(filepath.Dir(f.sessionDir), "foreign.meta")
		if err := os.WriteFile(foreign, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, agent.BranchMetaPath(path)); err != nil {
			t.Fatal(err)
		}
		_, err := f.catalog.ListSessions(context.Background(), protocol.SessionListParams{ExpectedHostEpoch: testHostEpoch, WorkspaceID: workspaceID})
		requireCatalogCode(t, err, protocol.ErrSessionPersistFailed)
	})
}

func TestWorkspaceListCursorInvalidatesOnCatalogMutation(t *testing.T) {
	f := newCatalogFixture(t)
	_ = f.openWorkspace()
	secondWorkspace := filepath.Join(filepath.Dir(f.workspace), "workspace-two")
	thirdWorkspace := filepath.Join(filepath.Dir(f.workspace), "workspace-three")
	for _, path := range []string{secondWorkspace, thirdWorkspace} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		browse, err := f.catalog.Browse(protocol.WorkspaceBrowseParams{ExpectedHostEpoch: testHostEpoch, TypedPath: path})
		if err != nil {
			t.Fatal(err)
		}
		// This fixture's SessionDir callback is only consulted for Session
		// operations, not workspace identity/listing.
		if _, err := f.catalog.OpenWorkspace(protocol.WorkspaceOpenParams{HostMutation: protocol.HostMutation{RequestID: protocol.RequestID("open_" + filepath.Base(path)), ExpectedHostEpoch: testHostEpoch}, PrimaryDirectoryRef: browse.Directory.DirectoryRef}); err != nil {
			t.Fatal(err)
		}
	}
	limit := 1
	first, err := f.catalog.ListWorkspaces(protocol.WorkspaceListParams{ExpectedHostEpoch: testHostEpoch, Limit: &limit})
	if err != nil || !first.HasMore {
		t.Fatalf("first list = %+v, %v", first, err)
	}
	if _, err := f.catalog.CreateTopic(protocol.TopicCreateParams{HostMutation: protocol.HostMutation{RequestID: "topic_mutation", ExpectedHostEpoch: testHostEpoch}, WorkspaceID: first.Items[0].WorkspaceID, Title: "mutation"}); err != nil {
		t.Fatal(err)
	}
	_, err = f.catalog.ListWorkspaces(protocol.WorkspaceListParams{ExpectedHostEpoch: testHostEpoch, Cursor: first.NextCursor, Limit: &limit})
	requireCatalogCode(t, err, protocol.ErrStaleCursor)
}

type reentrantWorkspaceInspector struct{ catalog *Catalog }

func (*reentrantWorkspaceInspector) WorkspaceInUse(protocol.WorkspaceID) bool { return false }

func (*reentrantWorkspaceInspector) SessionSummary(protocol.RuntimeTarget) (*protocol.SessionRuntimeSummary, bool) {
	return nil, false
}

func TestCloseWorkspaceRequiresAtomicReservationWhenRuntimeInspectorIsInstalled(t *testing.T) {
	f := newCatalogFixture(t)
	workspaceID := f.openWorkspace()
	inspector := &reentrantWorkspaceInspector{catalog: f.catalog}
	f.catalog.runtimeInspector = inspector
	params := protocol.WorkspaceCloseParams{
		HostMutation: protocol.HostMutation{RequestID: "close_reentrant", ExpectedHostEpoch: testHostEpoch},
		WorkspaceID:  workspaceID,
	}
	_, err := f.catalog.CloseWorkspace(params)
	requireCatalogCode(t, err, protocol.ErrWorkspaceInUse)
	listed, err := f.catalog.ListWorkspaces(protocol.WorkspaceListParams{ExpectedHostEpoch: testHostEpoch})
	if err != nil || len(listed.Items) != 1 {
		t.Fatalf("unsafe close changed catalog: %+v, %v", listed, err)
	}
	if _, err := f.catalog.CloseWorkspaceReserved(params, nil); err == nil {
		t.Fatal("reserved close accepted a missing capability")
	}
	capability := f.catalog.IssueWorkspaceCloseCapability(workspaceID)
	result, err := f.catalog.CloseWorkspaceReserved(params, capability)
	if err != nil || result.Disposition != protocol.WorkspaceClosed {
		t.Fatalf("CloseWorkspaceReserved = %+v, %v", result, err)
	}
	if _, err := f.catalog.CloseWorkspaceReserved(params, capability); err == nil {
		t.Fatal("reserved close reused a consumed capability")
	}
}

func copyFile(t *testing.T, source, destination string) {
	t.Helper()
	b, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
