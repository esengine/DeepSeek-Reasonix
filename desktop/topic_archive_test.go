package main

import (
	"bufio"
	"bytes"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

type snapshotErrorSessionController struct {
	control.SessionAPI
	err error
}

func (c *snapshotErrorSessionController) Snapshot() error { return c.err }

func TestTrashTopicSnapshotFailureKeepsRuntimeAndFiles(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	topicID := "topic_snapshot_failure"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Snapshot failure"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSessionWithPrompt(t, dir, "snapshot-failure.jsonl", topicID, "Snapshot failure", projectRoot, "preserve me", time.Now())
	base := controllerWithContent(t, sessionPath)
	defer base.Close()
	snapshotErr := errors.New("snapshot blocked")
	ctrl := &snapshotErrorSessionController{SessionAPI: base, err: snapshotErr}
	tab := &WorkspaceTab{ID: "snapshot-failure", Scope: "project", WorkspaceRoot: projectRoot, TopicID: topicID,
		TopicTitle: "Snapshot failure", SessionPath: sessionPath, Ctrl: ctrl, Ready: true, disabledMCP: map[string]ServerView{}}
	app := &App{tabs: map[string]*WorkspaceTab{tab.ID: tab}, tabOrder: []string{tab.ID}, activeTabID: tab.ID}
	if err := app.TrashTopic(topicID); !errors.Is(err, snapshotErr) {
		t.Fatalf("TrashTopic snapshot error = %v, want %v", err, snapshotErr)
	}
	if got := app.tabs[tab.ID]; got != tab || tab.removed {
		t.Fatalf("snapshot failure changed runtime binding: got=%p removed=%v", got, tab.removed)
	}
	if got := ctrl.SessionPath(); !sameDesktopPath(got, sessionPath) {
		t.Fatalf("snapshot failure session path = %q, want %q", got, sessionPath)
	}
	if agent.IsCleanupPending(sessionPath) {
		t.Fatal("snapshot failure must not publish a cleanup-pending marker")
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("snapshot failure removed the session file: %v", err)
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "Snapshot failure" {
		t.Fatalf("snapshot failure topic title = %q", got)
	}
}

func TestTrashTopicRejectsConcurrentRuntimeMutationWithoutWaiting(t *testing.T) {
	isolateDesktopUserDirs(t)
	topicID := "topic_runtime_mutation_busy"
	if err := setTopicTitle("", topicID, "Runtime mutation busy"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	app := &App{}
	app.runtimeRebuildMu.Lock()
	started := time.Now()
	err := app.TrashTopic(topicID)
	elapsed := time.Since(started)
	app.runtimeRebuildMu.Unlock()
	if !errors.Is(err, errTopicArchiveBusy) {
		t.Fatalf("TrashTopic error = %v, want %v", err, errTopicArchiveBusy)
	}
	if elapsed > time.Second {
		t.Fatalf("TrashTopic waited %s behind another runtime mutation", elapsed)
	}
	if got := loadTopicTitle("", topicID); got != "Runtime mutation busy" {
		t.Fatalf("busy archive changed topic title to %q", got)
	}
}

func TestTrashTopicForeignLeaseFailsBeforeCleanupCommit(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	topicID := "topic_foreign_lease"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Foreign lease"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSessionWithPrompt(t, dir, "foreign-lease.jsonl", topicID, "Foreign lease", projectRoot, "preserve me", time.Now())

	cmd := exec.Command(os.Args[0], "-test.run=^TestTrashTopicForeignLeaseHelper$")
	cmd.Env = append(os.Environ(),
		"REASONIX_TOPIC_ARCHIVE_LEASE_HELPER=1",
		"REASONIX_TOPIC_ARCHIVE_LEASE_PATH="+sessionPath,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("lease helper stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("lease helper stdout: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start lease helper: %v", err)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		_ = stdin.Close()
		if err := cmd.Wait(); err != nil {
			t.Errorf("lease helper exit: %v (%s)", err, stderr.String())
		}
	}
	defer release()
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(ready) != "ready" {
		t.Fatalf("lease helper readiness = %q, err=%v (%s)", ready, err, stderr.String())
	}

	app := NewApp()
	if err := app.TrashTopic(topicID); !errors.Is(err, errSessionBusyElsewhere) {
		t.Fatalf("TrashTopic foreign lease error = %v, want %v", err, errSessionBusyElsewhere)
	}
	if agent.IsCleanupPending(sessionPath) {
		t.Fatal("rejected archive published a cleanup-pending marker")
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "Foreign lease" {
		t.Fatalf("rejected archive topic title = %q", got)
	}
	if err := reconcileDesktopCleanupPending(dir); err != nil {
		t.Fatalf("reconcile without marker: %v", err)
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("rejected archive removed the live session: %v", err)
	}

	release()
	if err := reconcileDesktopCleanupPending(dir); err != nil {
		t.Fatalf("reconcile after foreign release: %v", err)
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("failed archive scheduled a later deletion: %v", err)
	}
}

func TestTrashTopicForeignLeaseHelper(t *testing.T) {
	if os.Getenv("REASONIX_TOPIC_ARCHIVE_LEASE_HELPER") != "1" {
		return
	}
	lease, err := agent.TryAcquireSessionLease(os.Getenv("REASONIX_TOPIC_ARCHIVE_LEASE_PATH"))
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	if _, err := os.Stdout.WriteString("ready\n"); err != nil {
		lease.Release()
		t.Fatalf("write readiness: %v", err)
	}
	var release [1]byte
	_, _ = os.Stdin.Read(release[:])
	lease.Release()
}

func TestSnapshotTopicRuntimeConflictLogOmitsSessionPath(t *testing.T) {
	privatePath := filepath.Join(t.TempDir(), "private-session.jsonl")
	base := control.New(control.Options{})
	defer base.Close()
	ctrl := &snapshotErrorSessionController{SessionAPI: base, err: &agent.SessionSnapshotConflictError{
		Path: privatePath,
		Kind: agent.SessionSnapshotConflictDiverged,
	}}
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	defer slog.SetDefault(previous)

	app := NewApp()
	if err := app.snapshotTopicRuntimeBindings([]removedSessionRuntime{{ctrl: ctrl}}); err != nil {
		t.Fatalf("snapshotTopicRuntimeBindings: %v", err)
	}
	logged := output.String()
	if strings.Contains(logged, privatePath) {
		t.Fatalf("snapshot conflict log exposed the session path: %s", logged)
	}
	if !strings.Contains(logged, string(agent.SessionSnapshotConflictDiverged)) {
		t.Fatalf("snapshot conflict log omitted the safe conflict kind: %s", logged)
	}
}

func TestTrashTopicConvertsLocalLeaseWithoutUnlockWindow(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	topicID := "topic_local_lease"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Local lease"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSessionWithPrompt(t, dir, "local-lease.jsonl", topicID, "Local lease", projectRoot, "preserve me", time.Now())
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: sessionPath, Label: "test", WorkspaceRoot: projectRoot})
	defer ctrl.Close()
	tab := &WorkspaceTab{ID: "local", Scope: "project", WorkspaceRoot: projectRoot, TopicID: topicID,
		TopicTitle: "Local lease", SessionPath: sessionPath, Ctrl: ctrl, Ready: true, disabledMCP: map[string]ServerView{}}
	if err := tab.ensureSessionLease(sessionPath); err != nil {
		t.Fatalf("ensureSessionLease: %v", err)
	}
	keep := &WorkspaceTab{ID: "keep", Scope: "project", WorkspaceRoot: projectRoot, TopicID: "keep", Ready: true}
	app := &App{tabs: map[string]*WorkspaceTab{tab.ID: tab, keep.ID: keep}, tabOrder: []string{tab.ID, keep.ID}, activeTabID: tab.ID}

	if err := app.TrashTopic("  " + topicID + "  "); err != nil {
		t.Fatalf("TrashTopic: %v", err)
	}
	if agent.IsCleanupPending(sessionPath) {
		t.Fatal("completed archive left cleanup pending")
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("archived session still exists, err=%v", err)
	}
	for _, path := range []string{store.SessionLeaseInfo(sessionPath), store.SessionLeaseLock(sessionPath), store.SessionLockFile(sessionPath)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("ownership sidecar survived archive: %s (err=%v)", path, err)
		}
	}
}

func TestTrashTopicCommittedCleanupFailureReconcilesWithoutFailureResponse(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	topicID := "topic_committed_cleanup"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Committed cleanup"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSessionWithPrompt(t, dir, "committed-cleanup.jsonl", topicID, "Committed cleanup", projectRoot, "preserve me", time.Now())
	topicArchiveCleanupHookForTest = func() error { return errors.New("injected cleanup failure") }
	defer func() { topicArchiveCleanupHookForTest = nil }()

	app := NewApp()
	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("committed TrashTopic returned a failure: %v", err)
	}
	if !agent.IsCleanupPending(sessionPath) {
		t.Fatal("committed cleanup failure did not retain its durable marker")
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "" {
		t.Fatalf("committed archive retained topic title %q", got)
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("deferred session disappeared before reconciliation: %v", err)
	}

	topicArchiveCleanupHookForTest = nil
	if err := reconcileDesktopCleanupPending(dir); err != nil {
		t.Fatalf("reconcile committed cleanup: %v", err)
	}
	if agent.IsCleanupPending(sessionPath) {
		t.Fatal("reconciliation retained the cleanup marker")
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("reconciled session still exists, err=%v", err)
	}
}

func TestTrashTopicMetadataFailureReconcilesFromDurableIntent(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	topicID := "topic_metadata_retry"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Metadata retry"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	writeTopicSessionWithPrompt(t, dir, "metadata-retry.jsonl", topicID, "Metadata retry", projectRoot, "preserve me", time.Now())
	blockedTempPath := topicTitleSourcesPath(projectRoot) + ".tmp"
	if err := os.MkdirAll(blockedTempPath, 0o755); err != nil {
		t.Fatalf("block metadata temp write: %v", err)
	}

	app := NewApp()
	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("committed TrashTopic returned a failure: %v", err)
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "Metadata retry" {
		t.Fatalf("failed metadata cleanup title = %q, want retained retry locator", got)
	}
	if _, err := os.Stat(topicArchiveMetadataPendingPath(topicID)); err != nil {
		t.Fatalf("metadata cleanup intent was not retained: %v", err)
	}

	if err := os.RemoveAll(blockedTempPath); err != nil {
		t.Fatalf("unblock metadata temp write: %v", err)
	}
	if err := reconcileTopicArchiveMetadataPending(app.deleteTopic); err != nil {
		t.Fatalf("reconcile topic metadata: %v", err)
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "" {
		t.Fatalf("reconciled archive retained topic title %q", got)
	}
	if _, err := os.Stat(topicArchiveMetadataPendingPath(topicID)); !os.IsNotExist(err) {
		t.Fatalf("reconciled metadata marker still exists, err=%v", err)
	}
	projects := loadProjectsFile()
	for _, project := range projects.Projects {
		if containsDesktopString(project.Topics, topicID) || containsDesktopString(project.PinnedTopics, topicID) {
			t.Fatalf("reconciled archive retained topic in project index: %+v", project)
		}
	}
}

func TestTopicArchiveIntentCompletesSessionsAndMetadataAfterRestart(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	topicID := "topic_restart_commit"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Restart commit"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSessionWithPrompt(t, dir, "restart-commit.jsonl", topicID, "Restart commit", projectRoot, "preserve me", time.Now())
	targets := []topicTrashTarget{{dir: dir, sessionPath: sessionPath, key: filepath.Base(sessionPath)}}
	if err := markTopicArchiveMetadataPending(topicID, targets); err != nil {
		t.Fatalf("mark archive intent: %v", err)
	}

	app := NewApp()
	if err := reconcileTopicArchiveMetadataPending(app.deleteTopic); err != nil {
		t.Fatalf("reconcile archive intent: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("reconciled live session still exists, err=%v", err)
	}
	trashPath := filepath.Join(sessionTrashPath(dir), filepath.Base(sessionPath), filepath.Base(sessionPath))
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("reconciled session was not preserved in trash: %v", err)
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "" {
		t.Fatalf("reconciled archive retained topic title %q", got)
	}
	if agent.IsCleanupPending(sessionPath) {
		t.Fatal("reconciled archive retained session cleanup marker")
	}
	if _, err := os.Stat(topicArchiveMetadataPendingPath(topicID)); !os.IsNotExist(err) {
		t.Fatalf("reconciled archive retained topic marker, err=%v", err)
	}
}

func TestTrashTopicPreservesDivergedExistingTrash(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	topicID := "topic_diverged_trash"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Diverged trash"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSessionWithPrompt(t, dir, "diverged-trash.jsonl", topicID, "Diverged trash", projectRoot, "new live history", time.Now())
	existingTrashPath := filepath.Join(sessionTrashPath(dir), filepath.Base(sessionPath), filepath.Base(sessionPath))
	existing := agent.NewSession("system")
	existing.Add(provider.Message{Role: provider.RoleUser, Content: "older trash history"})
	if err := existing.SaveSnapshot(existingTrashPath); err != nil {
		t.Fatalf("write existing trash: %v", err)
	}

	app := NewApp()
	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("TrashTopic: %v", err)
	}
	trashed, err := listTrashedSessionFiles(dir)
	if err != nil {
		t.Fatalf("listTrashedSessionFiles: %v", err)
	}
	if len(trashed) != 2 {
		t.Fatalf("trashed session count = %d, want both histories: %v", len(trashed), trashed)
	}
	seen := map[string]bool{}
	for _, path := range trashed {
		session, err := agent.LoadSession(path)
		if err != nil {
			t.Fatalf("LoadSession(%q): %v", path, err)
		}
		for _, message := range session.Snapshot() {
			seen[message.Content] = true
		}
	}
	for _, content := range []string{"older trash history", "new live history"} {
		if !seen[content] {
			t.Fatalf("archived histories = %#v, missing %q", seen, content)
		}
	}
}

func TestTopicArchiveOwnershipRollbackRestoresLocalLease(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := filepath.Join(dir, "rollback-lease.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	tab := &WorkspaceTab{ID: "rollback", SessionPath: sessionPath, Ready: true}
	if err := tab.ensureSessionLease(sessionPath); err != nil {
		t.Fatalf("ensureSessionLease: %v", err)
	}
	defer tab.releaseSessionLease()
	removed := []removedSessionRuntime{{tab: tab, sessionDir: dir, sessionPath: sessionPath}}
	targets := []topicTrashTarget{{dir: dir, sessionPath: sessionPath, key: filepath.Base(sessionPath)}}

	batch, err := acquireTopicArchiveOwnership(targets, removed)
	if err != nil {
		t.Fatalf("acquireTopicArchiveOwnership: %v", err)
	}
	if tab.sessionLeaseRuntimeKey() != "" {
		batch.rollback()
		t.Fatal("converted ownership remained attached to the runtime tab")
	}
	batch.rollback()
	if got := tab.sessionLeaseRuntimeKey(); got != sessionRuntimeKey(sessionPath) {
		t.Fatalf("rolled back lease key = %q, want %q", got, sessionRuntimeKey(sessionPath))
	}
	if next, err := agent.TryAcquireSessionLease(sessionPath); !errors.Is(err, agent.ErrSessionLeaseHeld) {
		if next != nil {
			next.Release()
		}
		t.Fatalf("competing lease after rollback err = %v, want ErrSessionLeaseHeld", err)
	}
}

func TestTrashTopicFallbackStaysOffCatalogSidebar(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	if err := addProject(projectRoot, "Archive Sidebar"); err != nil {
		t.Fatalf("add project: %v", err)
	}
	keepID := "topic_keep"
	archiveID := "topic_archive"
	if err := setTopicTitle(projectRoot, keepID, "Keep me"); err != nil {
		t.Fatalf("set keep title: %v", err)
	}
	if err := setTopicTitle(projectRoot, archiveID, "Archive me"); err != nil {
		t.Fatalf("set archive title: %v", err)
	}
	dir := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	keepPath := writeTopicSession(t, dir, "keep.jsonl", keepID, "Keep me", projectRoot)
	archivePath := writeTopicSession(t, dir, "archive.jsonl", archiveID, "Archive me", projectRoot)
	leftoverA := filepath.Join(dir, "leftover-a.jsonl")
	leftoverB := filepath.Join(dir, "leftover-b.jsonl")
	for _, path := range []string{leftoverA, leftoverB} {
		writeZeroByteSession(t, path)
		if err := pinNewEmptySessionBranchMeta(path, "project", projectRoot, "", defaultTopicTitle); err != nil {
			t.Fatalf("pin leftover %s: %v", path, err)
		}
	}
	ghostID := agent.BranchID(filepath.Join(dir, "ghost-blank.jsonl"))
	ghostPath := writeEmptyNamedSession(t, dir, "ghost-blank.jsonl", ghostID, defaultTopicTitle, projectRoot)
	nonzeroPath := filepath.Join(dir, "system-only.jsonl")
	if err := os.WriteFile(nonzeroPath, []byte(`{"role":"system","content":"identity"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write non-empty sibling: %v", err)
	}
	if err := pinNewEmptySessionBranchMeta(nonzeroPath, "project", projectRoot, "", defaultTopicTitle); err != nil {
		t.Fatalf("pin non-empty sibling: %v", err)
	}
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: archivePath, Label: "test", WorkspaceRoot: projectRoot})
	defer ctrl.Close()
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"archive": {
				ID: "archive", Scope: "project", WorkspaceRoot: projectRoot,
				TopicID: archiveID, TopicTitle: "Archive me", Ctrl: ctrl, Ready: true,
				disabledMCP: map[string]ServerView{},
			},
		},
		tabOrder:    []string{"archive"},
		activeTabID: "archive",
	}
	installSessionCatalogForTest(t, app, dir, "project", projectRoot)

	if err := app.TrashTopic(archiveID); err != nil {
		t.Fatalf("TrashTopic: %v", err)
	}
	reconcileSessionCatalogForTest(t, app, dir, "project", projectRoot)

	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("archived session should be gone, stat err = %v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("kept session missing: %v", err)
	}
	if _, err := os.Stat(nonzeroPath); err != nil {
		t.Fatalf("non-empty sibling must not be swept, stat err = %v", err)
	}
	for _, path := range []string{leftoverA, leftoverB, ghostPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unused transient blank %s should be discarded, stat err = %v", path, err)
		}
	}
	if len(app.tabs) != 1 {
		t.Fatalf("fallback should create exactly one visible tab, got %d", len(app.tabs))
	}
	var fallbackPath string
	for id, tab := range app.tabs {
		if strings.TrimSpace(tab.TopicID) != "" {
			t.Fatalf("fallback tab %q topic ID = %q, want transient unindexed blank", id, tab.TopicID)
		}
		fallbackPath = tab.SessionPath
		if strings.TrimSpace(fallbackPath) == "" {
			t.Fatalf("fallback tab %q has no precreated session path", id)
		}
	}
	page, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "project", WorkspaceRoot: projectRoot, Limit: 50})
	if err != nil {
		t.Fatalf("ListProjectTopics: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].TopicID != keepID {
		t.Fatalf("sidebar after archive = %#v, want only %q", page.Items, keepID)
	}
	if page.Items[0].Label == defaultTopicTitle || page.Items[0].Label == "New session" {
		t.Fatalf("sidebar listed a default blank title: %#v", page.Items[0])
	}
	if fallbackPath != "" {
		if _, err := os.Stat(fallbackPath); err != nil {
			t.Fatalf("current fallback blank should remain writable: %v", err)
		}
	}
}

func TestUnusedTransientBlankSessionOnlyMatchesZeroByteFiles(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	zero := filepath.Join(dir, "zero.jsonl")
	writeZeroByteSession(t, zero)
	if !unusedTransientBlankSession(dir, zero) {
		t.Fatal("zero-byte unindexed session should be an unused transient blank")
	}
	nonzero := filepath.Join(dir, "nonzero.jsonl")
	if err := os.WriteFile(nonzero, []byte(`{"role":"system","content":"identity"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write non-empty session: %v", err)
	}
	if unusedTransientBlankSession(dir, nonzero) {
		t.Fatal("non-empty session must not be classified as an unused transient blank")
	}
}

func writeZeroByteSession(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write zero-byte session %s: %v", path, err)
	}
}

func writeEmptyNamedSession(t *testing.T, dir, name, topicID, topicTitle, workspaceRoot string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	writeZeroByteSession(t, path)
	if err := pinNewEmptySessionBranchMeta(path, "project", workspaceRoot, topicID, topicTitle); err != nil {
		t.Fatalf("pin empty session: %v", err)
	}
	return path
}
