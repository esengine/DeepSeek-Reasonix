package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/host"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

type fileGitWireController struct {
	*daemonFakeController
	workspaceRoot string
	sessionPath   string
	checkpoints   control.CheckpointSnapshot
}

func (c *fileGitWireController) WorkspaceRoot() string { return c.workspaceRoot }
func (c *fileGitWireController) SessionPath() string   { return c.sessionPath }
func (c *fileGitWireController) SessionDir() string    { return filepath.Dir(c.sessionPath) }

func (c *fileGitWireController) CheckpointSnapshot() control.CheckpointSnapshot {
	result := control.CheckpointSnapshot{
		Metas:                 make([]checkpoint.Meta, len(c.checkpoints.Metas)),
		TurnsByMessageIndex:   make(map[int]int, len(c.checkpoints.TurnsByMessageIndex)),
		ConversationAvailable: make(map[int]bool, len(c.checkpoints.ConversationAvailable)),
	}
	for index, meta := range c.checkpoints.Metas {
		result.Metas[index] = meta
		result.Metas[index].Paths = append([]string(nil), meta.Paths...)
	}
	for key, value := range c.checkpoints.TurnsByMessageIndex {
		result.TurnsByMessageIndex[key] = value
	}
	for key, value := range c.checkpoints.ConversationAvailable {
		result.ConversationAvailable[key] = value
	}
	return result
}

type fileGitWireFactory struct {
	catalog     *catalog.Catalog
	checkpoints control.CheckpointSnapshot
}

func (f *fileGitWireFactory) CreateController(ctx context.Context, target protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	resolved, err := f.catalog.ResolveRuntimeTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	return &fileGitWireController{
		daemonFakeController: newDaemonFakeController(ctx, sink),
		workspaceRoot:        resolved.WorkspaceRoot,
		sessionPath:          resolved.SessionPath,
		checkpoints:          f.checkpoints,
	}, nil
}

type fileGitWireFixture struct {
	root    string
	peer    *daemonPeer
	created protocol.SessionCreateResult
}

func newFileGitWireFixture(t *testing.T, root string, checkpoints control.CheckpointSnapshot) *fileGitWireFixture {
	t.Helper()
	stateRoot := t.TempDir()
	userHome := filepath.Join(stateRoot, "home")
	sessionDir := filepath.Join(stateRoot, "sessions")
	for _, directory := range []string{userHome, sessionDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	catalogValue, err := catalog.New("host-test", catalog.Options{
		StateDir: filepath.Join(stateRoot, "catalog"), UserHome: userHome,
		SessionDir: func(string) string { return sessionDir }, ProfileResolver: daemonProfileResolver{},
	})
	if err != nil {
		t.Fatalf("New file/Git wire Catalog: %v", err)
	}
	factory := &fileGitWireFactory{catalog: catalogValue, checkpoints: checkpoints}
	metadata := func(ctx context.Context, target protocol.RuntimeTarget) (protocol.SessionMetaSnapshot, error) {
		value, metadataErr := catalogValue.Metadata(ctx, target)
		if metadataErr != nil {
			return protocol.SessionMetaSnapshot{}, metadataErr
		}
		return protocol.SessionMetaSnapshot{
			TopicID: value.TopicID, Title: value.Title, ResolvedProfile: value.ResolvedProfile,
		}, nil
	}
	buildID := daemonTestBuildID(t, 'f')
	server, err := New(context.Background(), Options{
		BuildID: buildID, HostEpoch: "host-test",
		HostInfo:     protocol.HostInfo{OS: "linux", Arch: "amd64", ShellKind: "bash", SandboxBackend: "process"},
		Capabilities: protocol.FrozenCapabilities(false, false),
		Catalog:      catalogValue, ControllerFactory: factory, Metadata: metadata,
		RuntimeOptions: host.RuntimeManagerOptions{SubscriptionQueue: 16},
	})
	if err != nil {
		t.Fatalf("New file/Git wire Server: %v", err)
	}
	t.Cleanup(server.Close)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "file-git-client", "")
	browsed := browseWorkspacePeer(t, peer, root)
	opened := openWorkspacePeer(t, peer, "file-git-open", browsed.Directory.DirectoryRef)
	created := createSessionPeer(t, peer, "file-git-session", opened.Workspace.WorkspaceID)
	return &fileGitWireFixture{root: root, peer: peer, created: created}
}

func (f *fileGitWireFixture) query() protocol.RuntimeQuery {
	return protocol.RuntimeQuery{
		ExpectedHostEpoch: "host-test", Target: f.created.Target,
		ExpectedRuntimeEpoch: f.created.RuntimeEpoch,
	}
}

func writeFileGitWireFile(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %q: %v", relative, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %q: %v", relative, err)
	}
}

func intPointer(value int) *int { return &value }

func tamperProtocolCursor(cursor protocol.Cursor) protocol.Cursor {
	value := []byte(cursor)
	if value[len(value)-1] == 'A' {
		value[len(value)-1] = 'B'
	} else {
		value[len(value)-1] = 'A'
	}
	return protocol.Cursor(value)
}

func TestFileGitWireQueriesEnforceContainmentAndCursorSnapshots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFileGitWireFile(t, root, "dir-a/needle.txt", "needle body\n")
	writeFileGitWireFile(t, root, "dir-b/other.txt", "other\n")
	writeFileGitWireFile(t, root, "a.txt", "a\n")
	writeFileGitWireFile(t, root, "b.txt", "b\n")
	writeFileGitWireFile(t, root, "c.txt", "c\n")
	writeFileGitWireFile(t, root, "node_modules/needle-noise.js", "noise\n")
	writeFileGitWireFile(t, outside, "secret.txt", "outside secret\n")
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	fixture := newFileGitWireFixture(t, root, control.CheckpointSnapshot{})

	first := requestResult[protocol.FileListResult](t, fixture.peer, protocol.MethodFileList, protocol.FileListParams{
		RuntimeQuery: fixture.query(), Limit: intPointer(2),
	})
	if !first.HasMore || first.NextCursor == "" || len(first.Entries) != 2 ||
		first.Entries[0].Path != "dir-a" || first.Entries[1].Path != "dir-b" {
		t.Fatalf("first file/list page = %+v", first)
	}
	second := requestResult[protocol.FileListResult](t, fixture.peer, protocol.MethodFileList, protocol.FileListParams{
		RuntimeQuery: fixture.query(), Cursor: first.NextCursor, Limit: intPointer(100),
	})
	if second.HasMore || second.NextCursor != "" || len(second.Entries) != 3 {
		t.Fatalf("second file/list page = %+v", second)
	}
	for _, entry := range append(append([]protocol.FileEntry{}, first.Entries...), second.Entries...) {
		if entry.Path == "escape" || strings.Contains(entry.Path, "node_modules") {
			t.Fatalf("unsafe/noise entry crossed file/list: %+v", entry)
		}
	}

	tampered := requestError(t, fixture.peer, protocol.MethodFileList, protocol.FileListParams{
		RuntimeQuery: fixture.query(), Cursor: tamperProtocolCursor(first.NextCursor),
	})
	requireRemoteError(t, tampered, protocol.ErrStaleCursor)

	fresh := requestResult[protocol.FileListResult](t, fixture.peer, protocol.MethodFileList, protocol.FileListParams{
		RuntimeQuery: fixture.query(), Limit: intPointer(1),
	})
	writeFileGitWireFile(t, root, "new-after-cursor.txt", "new\n")
	stale := requestError(t, fixture.peer, protocol.MethodFileList, protocol.FileListParams{
		RuntimeQuery: fixture.query(), Cursor: fresh.NextCursor,
	})
	requireRemoteError(t, stale, protocol.ErrStaleCursor)

	search := requestResult[protocol.FileSearchResult](t, fixture.peer, protocol.MethodFileSearch, protocol.FileSearchParams{
		RuntimeQuery: fixture.query(), Query: "needle", Limit: intPointer(10),
	})
	if search.Truncated || len(search.Entries) != 1 || search.Entries[0].Path != "dir-a/needle.txt" {
		t.Fatalf("file/search result = %+v", search)
	}
	preview := requestResult[protocol.FilePreviewResult](t, fixture.peer, protocol.MethodFilePreview, protocol.FilePreviewParams{
		RuntimeQuery: fixture.query(), Path: "dir-a/needle.txt",
	})
	if preview.Kind != protocol.FileText || preview.Body == nil || *preview.Body != "needle body\n" || preview.Binary || preview.Truncated {
		t.Fatalf("file/preview result = %+v", preview)
	}

	traversal := requestError(t, fixture.peer, protocol.MethodFilePreview, protocol.FilePreviewParams{
		RuntimeQuery: fixture.query(), Path: "../secret.txt",
	})
	if traversal.Code != rpcwire.ErrInvalidParams {
		t.Fatalf("traversal JSON-RPC code = %d, want %d: %+v", traversal.Code, rpcwire.ErrInvalidParams, traversal)
	}
	escape := requestError(t, fixture.peer, protocol.MethodFilePreview, protocol.FilePreviewParams{
		RuntimeQuery: fixture.query(), Path: "escape/secret.txt",
	})
	requireRemoteError(t, escape, protocol.ErrPermissionDenied)
	serializedError := escape.Message + string(escape.Data)
	if strings.Contains(serializedError, root) || strings.Contains(serializedError, outside) || strings.Contains(serializedError, "secret.txt") {
		t.Fatalf("containment error leaked a Host path: %s", serializedError)
	}
}

func TestFileGitWireWorkspaceChangesSurvivesUnavailableGit(t *testing.T) {
	root := t.TempDir()
	writeFileGitWireFile(t, root, "present.txt", "present\n")
	checkpointTime := time.UnixMilli(1_700_000_000_123)
	fixture := newFileGitWireFixture(t, root, control.CheckpointSnapshot{Metas: []checkpoint.Meta{{
		Turn: 4, Time: checkpointTime, Prompt: "checkpoint prompt",
		Paths: []string{filepath.Join(root, "present.txt"), "missing.txt"},
	}}})

	first := requestResult[protocol.WorkspaceChangesResult](t, fixture.peer, protocol.MethodWorkspaceChanges, protocol.WorkspaceChangesParams{
		RuntimeQuery: fixture.query(), Limit: intPointer(1),
	})
	if first.GitAvailable || first.GitBranch != "" || !first.HasMore || first.NextCursor == "" || len(first.Files) != 1 {
		t.Fatalf("first workspace/changes page = %+v", first)
	}
	second := requestResult[protocol.WorkspaceChangesResult](t, fixture.peer, protocol.MethodWorkspaceChanges, protocol.WorkspaceChangesParams{
		RuntimeQuery: fixture.query(), Cursor: first.NextCursor, Limit: intPointer(1),
	})
	if second.GitAvailable || second.HasMore || second.NextCursor != "" || len(second.Files) != 1 {
		t.Fatalf("second workspace/changes page = %+v", second)
	}
	files := append(append([]protocol.ChangedFile{}, first.Files...), second.Files...)
	if files[0].Path != "missing.txt" || files[1].Path != "present.txt" {
		t.Fatalf("checkpoint paths were not primary-relative and sorted: %+v", files)
	}
	for _, file := range files {
		if len(file.Sources) != 1 || file.Sources[0] != protocol.ChangeSession ||
			len(file.Turns) != 1 || file.Turns[0] != 4 || file.LatestPrompt != "checkpoint prompt" ||
			file.LatestTimeMs == nil || *file.LatestTimeMs != checkpointTime.UnixMilli() {
			t.Fatalf("checkpoint projection drifted: %+v", file)
		}
	}

	gitError := requestError(t, fixture.peer, protocol.MethodGitHistory, protocol.GitHistoryParams{RuntimeQuery: fixture.query()})
	data := requireRemoteError(t, gitError, protocol.ErrGitUnavailable)
	if data.Target == nil || *data.Target != fixture.created.Target {
		t.Fatalf("GIT_UNAVAILABLE target = %+v, want %+v", data.Target, fixture.created.Target)
	}
	serializedError := gitError.Message + string(gitError.Data)
	if strings.Contains(strings.ToLower(serializedError), "not a git") || strings.Contains(serializedError, root) {
		t.Fatalf("Git failure leaked Host diagnostics: %s", serializedError)
	}
}

func runFileGitWireGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Env = append(os.Environ(), "LC_ALL=C")
	raw, err := command.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			t.Skip("git is unavailable")
		}
		t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(string(raw)))
	}
	return strings.TrimSpace(string(raw))
}

func commitFileGitWire(t *testing.T, root, message string) string {
	t.Helper()
	runFileGitWireGit(t, root, "add", "--all")
	runFileGitWireGit(t, root, "commit", "-q", "-m", message)
	return runFileGitWireGit(t, root, "rev-parse", "HEAD")
}

func TestFileGitWireHistoryCommitPatchAndEpochGuards(t *testing.T) {
	root := t.TempDir()
	runFileGitWireGit(t, root, "init", "-q")
	runFileGitWireGit(t, root, "config", "user.email", "wire@example.test")
	runFileGitWireGit(t, root, "config", "user.name", "Wire Test")
	runFileGitWireGit(t, root, "config", "gc.auto", "0")
	writeFileGitWireFile(t, root, "a.txt", "a\n")
	writeFileGitWireFile(t, root, "b.txt", "b\n")
	writeFileGitWireFile(t, root, "large.txt", "")
	baseHash := commitFileGitWire(t, root, "base files")
	large := strings.Repeat("界", protocol.GitPatchBytes/3+100_000) + "\n"
	writeFileGitWireFile(t, root, "large.txt", large)
	largeHash := commitFileGitWire(t, root, "large patch")
	fixture := newFileGitWireFixture(t, root, control.CheckpointSnapshot{})

	history := requestResult[protocol.GitHistoryResult](t, fixture.peer, protocol.MethodGitHistory, protocol.GitHistoryParams{
		RuntimeQuery: fixture.query(),
	})
	if history.Truncated || history.ReturnedItems != 2 || len(history.Commits) != 2 {
		t.Fatalf("git/history result = %+v", history)
	}
	fullHash := regexp.MustCompile(`^[0-9a-f]{40}$`)
	for _, commit := range history.Commits {
		if !fullHash.MatchString(commit.Hash) {
			t.Fatalf("git/history returned non-full hash %q", commit.Hash)
		}
		if _, err := time.Parse(time.RFC3339, commit.Date); err != nil {
			t.Fatalf("git/history returned non-RFC3339 date %q: %v", commit.Date, err)
		}
	}

	var commitFiles []protocol.GitCommitFile
	var cursor protocol.Cursor
	for {
		page := requestResult[protocol.GitCommitDetailResult](t, fixture.peer, protocol.MethodGitCommitDetail, protocol.GitCommitDetailParams{
			RuntimeQuery: fixture.query(), Hash: baseHash, Cursor: cursor, Limit: intPointer(1),
		})
		if page.Kind != protocol.GitDetailFiles || page.Files == nil || page.HasMore == nil || len(*page.Files) != 1 {
			t.Fatalf("git/commitDetail files page = %+v", page)
		}
		commitFiles = append(commitFiles, (*page.Files)...)
		if !*page.HasMore {
			if page.NextCursor != "" {
				t.Fatalf("terminal commit page retained cursor: %+v", page)
			}
			break
		}
		if page.NextCursor == "" {
			t.Fatalf("non-terminal commit page omitted cursor: %+v", page)
		}
		cursor = page.NextCursor
	}
	if len(commitFiles) != 3 || commitFiles[0].Path != "a.txt" || commitFiles[1].Path != "b.txt" || commitFiles[2].Path != "large.txt" {
		t.Fatalf("paginated commit files = %+v", commitFiles)
	}

	patch := requestResult[protocol.GitCommitDetailResult](t, fixture.peer, protocol.MethodGitCommitDetail, protocol.GitCommitDetailParams{
		RuntimeQuery: fixture.query(), Hash: largeHash, Path: "large.txt",
	})
	if patch.Kind != protocol.GitDetailPatch || patch.Body == nil || patch.SizeBytes == nil ||
		patch.ReturnedBytes == nil || patch.Truncated == nil {
		t.Fatalf("git/commitDetail patch shape = %+v", patch)
	}
	if !*patch.Truncated || patch.TruncationReason != protocol.ByteLimit ||
		*patch.SizeBytes <= *patch.ReturnedBytes || *patch.ReturnedBytes > protocol.GitPatchBytes ||
		!utf8.ValidString(*patch.Body) {
		t.Fatalf("patch bounds = size:%d returned:%d truncated:%v reason:%q utf8:%v",
			*patch.SizeBytes, *patch.ReturnedBytes, *patch.Truncated, patch.TruncationReason, utf8.ValidString(*patch.Body))
	}

	missingObject := requestError(t, fixture.peer, protocol.MethodGitCommitDetail, protocol.GitCommitDetailParams{
		RuntimeQuery: fixture.query(), Hash: strings.Repeat("f", 40),
	})
	requireRemoteError(t, missingObject, protocol.ErrGitObjectNotFound)

	staleRuntimeQuery := fixture.query()
	staleRuntimeQuery.ExpectedRuntimeEpoch = "runtime-stale"
	staleRuntime := requestError(t, fixture.peer, protocol.MethodFileList, protocol.FileListParams{RuntimeQuery: staleRuntimeQuery})
	staleRuntimeData := requireRemoteError(t, staleRuntime, protocol.ErrStaleRuntimeEpoch)
	if staleRuntimeData.Expected != string(fixture.created.RuntimeEpoch) || staleRuntimeData.Actual != "runtime-stale" ||
		staleRuntimeData.Target == nil || *staleRuntimeData.Target != fixture.created.Target {
		t.Fatalf("STALE_RUNTIME_EPOCH data = %+v", staleRuntimeData)
	}

	staleHostQuery := fixture.query()
	staleHostQuery.ExpectedHostEpoch = "host-stale"
	staleHost := requestError(t, fixture.peer, protocol.MethodFileList, protocol.FileListParams{RuntimeQuery: staleHostQuery})
	staleHostData := requireRemoteError(t, staleHost, protocol.ErrStaleHostEpoch)
	if staleHostData.Expected != "host-test" || staleHostData.Actual != "host-stale" {
		t.Fatalf("STALE_HOST_EPOCH data = %+v", staleHostData)
	}
}
