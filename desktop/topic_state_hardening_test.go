package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/topicstate"
)

func TestTopicIndexRepairDoesNotOverwriteConcurrentManualRename(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	seedLegacyTopicBridge(t, workspaceRoot)
	if err := setTopicTitleWithSource(workspaceRoot, "topic-1", "Before repair", topicTitleSourceAuto); err != nil {
		t.Fatal(err)
	}
	staleTitles := loadTopicTitles(workspaceRoot)
	staleSources := loadTopicTitleSources(workspaceRoot)
	staleTitles["topic-2"] = "Recovered topic"
	staleSources["topic-2"] = topicTitleSourceManual

	startSave := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-startSave
		done <- saveTopicTitleIndex(workspaceRoot, staleTitles, staleSources)
	}()
	if err := setTopicTitle(workspaceRoot, "topic-1", "Manual rename wins"); err != nil {
		t.Fatal(err)
	}
	close(startSave)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := loadTopicTitle(workspaceRoot, "topic-1"); got != "Manual rename wins" {
		t.Fatalf("manual rename was overwritten: %q", got)
	}
	if got := loadTopicTitle(workspaceRoot, "topic-2"); got != "Recovered topic" {
		t.Fatalf("missing title was not repaired: %q", got)
	}
	legacy, err := loadLegacyStringMap(topicTitlesPath(workspaceRoot))
	if err != nil {
		t.Fatal(err)
	}
	if legacy["topic-1"] != "Manual rename wins" {
		t.Fatalf("legacy mirror reverted manual rename: %#v", legacy)
	}
}

func TestRestoreTombstonedTopicInLegacyBridgeScope(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	if err := addProject(workspaceRoot, ""); err != nil {
		t.Fatal(err)
	}
	seedLegacyTopicBridge(t, workspaceRoot)
	topicID := "topic-restored"
	if err := setTopicTitle(workspaceRoot, topicID, "Before delete"); err != nil {
		t.Fatal(err)
	}
	if err := deleteTopicState(workspaceRoot, topicID); err != nil {
		t.Fatal(err)
	}
	if err := removeTopicFromProjectsFile(topicID); err != nil {
		t.Fatal(err)
	}
	dir := desktopSessionDir(workspaceRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "restored.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"restore"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		CreatedAt: time.Now().Add(-time.Minute), Scope: "project", WorkspaceRoot: workspaceRoot,
		TopicID: topicID, TopicTitle: "Restored title",
	}); err != nil {
		t.Fatal(err)
	}

	if err := restoreSessionTopicIndex(dir, path); err != nil {
		t.Fatal(err)
	}
	projects := loadProjectsFile()
	if containsDesktopString(projects.DeletedTopics, topicID) {
		t.Fatalf("restore left tombstone behind: %#v", projects.DeletedTopics)
	}
	projectIndex := projectIndexByRoot(projects.Projects, workspaceRoot)
	if projectIndex < 0 || !containsDesktopString(projects.Projects[projectIndex].Topics, topicID) {
		t.Fatalf("restored topic is not indexed: %#v", projects.Projects)
	}
	if got := loadTopicTitle(workspaceRoot, topicID); got != "Restored title" {
		t.Fatalf("authoritative restored title = %q", got)
	}
	legacy, err := loadLegacyStringMap(topicTitlesPath(workspaceRoot))
	if err != nil {
		t.Fatal(err)
	}
	if legacy[topicID] != "Restored title" {
		t.Fatalf("legacy restored title = %q", legacy[topicID])
	}
}

func TestLegacyAutoMetaMergePreservesDatabaseOnlyUnknownFields(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	seedLegacyTopicBridge(t, workspaceRoot)
	path := topicAutoTitleMetaPath(workspaceRoot)
	if err := os.WriteFile(path, []byte(`{"topic-1":{"stage":1,"future":{"kept":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadTopicAutoTitleMeta(workspaceRoot)["topic-1"].Stage; got != 1 {
		t.Fatalf("initial stage = %d", got)
	}
	if err := os.WriteFile(path, []byte(`{"topic-1":{"stage":3,"basisHash":"old-writer"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setTopicCreatedAt(workspaceRoot, "topic-1", 1234); err != nil {
		t.Fatal(err)
	}
	snapshot, err := desktopTopicState.snapshot(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	raw := snapshot.Records["topic-1"].AutoMeta
	if !containsJSONKey(raw, "future") {
		t.Fatalf("database-only unknown field was lost: %s", raw)
	}
	var meta topicAutoTitleMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Stage != 3 || meta.BasisHash != "old-writer" {
		t.Fatalf("legacy known fields were not merged: %+v", meta)
	}
}

func TestRestartRepairsPendingMirrorBeforeLegacyReconciliation(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	seedLegacyTopicBridge(t, workspaceRoot)
	if err := createTopicState(workspaceRoot, "topic-1", defaultTopicTitle, topicTitleSourceAuto, 123); err != nil {
		t.Fatal(err)
	}
	initial := autoTopicTitleProposal{Title: "Initial title", Stage: 1, UserTurns: 1, BasisHash: "initial"}
	if applied, err := applyAutoTopicTitle(workspaceRoot, "topic-1", initial.Title, initial); err != nil || !applied {
		t.Fatalf("apply initial title: applied=%v err=%v", applied, err)
	}

	topicLegacyWriteHookForTest = func(path string) error {
		if path == topicAutoTitleMetaPath(workspaceRoot) {
			return errors.New("injected partial legacy mirror")
		}
		return nil
	}
	t.Cleanup(func() { topicLegacyWriteHookForTest = nil })
	updated := autoTopicTitleProposal{Title: "Updated title", Stage: 3, UserTurns: 4, BasisHash: "updated"}
	if applied, err := applyAutoTopicTitle(workspaceRoot, "topic-1", updated.Title, updated); err != nil || !applied {
		t.Fatalf("apply updated title: applied=%v err=%v", applied, err)
	}
	// A second write while the mirror is still failing must advance SQLite,
	// not silently succeed through the legacy-only fallback.
	if err := setTopicCreatedAt(workspaceRoot, "topic-1", 999); err != nil {
		t.Fatal(err)
	}
	desktopTopicState.close()
	topicLegacyWriteHookForTest = nil

	snapshot, err := desktopTopicState.snapshot(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	record := snapshot.Records["topic-1"]
	var meta topicAutoTitleMeta
	if err := json.Unmarshal(record.AutoMeta, &meta); err != nil {
		t.Fatal(err)
	}
	if record.Title != updated.Title || record.CreatedAtMS != 999 || meta.Stage != updated.Stage || meta.BasisHash != updated.BasisHash {
		t.Fatalf("pending mirror rolled SQLite back: record=%+v meta=%+v", record, meta)
	}
	legacy, err := loadLegacyAutoMetaMap(topicAutoTitleMetaPath(workspaceRoot))
	if err != nil {
		t.Fatal(err)
	}
	if got := legacy["topic-1"]; got.Stage != updated.Stage || got.BasisHash != updated.BasisHash {
		t.Fatalf("pending mirror was not repaired: %+v", got)
	}
}

func TestCompoundTopicMutationsCommitOneRevision(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	if err := createTopicState(workspaceRoot, "topic-1", defaultTopicTitle, topicTitleSourceAuto, 123); err != nil {
		t.Fatal(err)
	}
	created, err := desktopTopicState.snapshot(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if created.State.Revision != 1 {
		t.Fatalf("create revision = %d, want 1", created.State.Revision)
	}
	record := created.Records["topic-1"]
	if record.Title != defaultTopicTitle || record.TitleSource != topicTitleSourceAuto || record.CreatedAtMS != 123 {
		t.Fatalf("created record is partial: %+v", record)
	}
	proposal := autoTopicTitleProposal{Title: "Generated title", Stage: 2, UserTurns: 3, BasisHash: "basis"}
	if applied, err := applyAutoTopicTitle(workspaceRoot, "topic-1", proposal.Title, proposal); err != nil {
		t.Fatal(err)
	} else if !applied {
		t.Fatal("automatic title was not applied")
	}
	updated, err := desktopTopicState.snapshot(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State.Revision != 2 {
		t.Fatalf("auto-title revision = %d, want 2", updated.State.Revision)
	}
	record = updated.Records["topic-1"]
	var meta topicAutoTitleMeta
	if err := json.Unmarshal(record.AutoMeta, &meta); err != nil {
		t.Fatal(err)
	}
	if record.Title != proposal.Title || record.TitleSource != topicTitleSourceAuto || meta.Stage != proposal.Stage || meta.BasisHash != proposal.BasisHash {
		t.Fatalf("auto-title record is partial: record=%+v meta=%+v", record, meta)
	}
}

func TestAutomaticTitleDoesNotOverwriteConcurrentManualRename(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	if err := createTopicState(workspaceRoot, "topic-1", defaultTopicTitle, topicTitleSourceAuto, 123); err != nil {
		t.Fatal(err)
	}
	proposal := autoTopicTitleProposal{Title: "Stale automatic title", Stage: 2, UserTurns: 3, BasisHash: "basis"}
	if err := setTopicTitle(workspaceRoot, "topic-1", "Manual rename wins"); err != nil {
		t.Fatal(err)
	}
	if applied, err := applyAutoTopicTitle(workspaceRoot, "topic-1", proposal.Title, proposal); err != nil {
		t.Fatal(err)
	} else if applied {
		t.Fatal("stale automatic title overwrote a manual rename")
	}
	snapshot, err := desktopTopicState.snapshot(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	record := snapshot.Records["topic-1"]
	if record.Title != "Manual rename wins" || record.TitleSource != topicTitleSourceManual || len(record.AutoMeta) != 0 {
		t.Fatalf("manual record changed: %+v", record)
	}
}

func TestManualRenamePublishesAfterInFlightAutomaticTitle(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	app := NewApp()
	topic, err := app.CreateTopic("project", workspaceRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	dir := desktopSessionDir(workspaceRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeTopicSessionWithPrompt(t, dir, "rename-race.jsonl", topic.ID, defaultTopicTitle, workspaceRoot, "automatic candidate", time.Now())
	ctrl := controllerWithContent(t, path)
	defer ctrl.Close()
	tab := &WorkspaceTab{
		ID: "rename-race", Scope: "project", WorkspaceRoot: workspaceRoot,
		TopicID: topic.ID, TopicTitle: defaultTopicTitle, topicTitleSource: topicTitleSourceAuto,
		SessionPath: path, Ctrl: ctrl,
	}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}

	autoCommitted := make(chan struct{})
	releaseAuto := make(chan struct{})
	topicAutoTitleCommittedHookForTest = func() {
		close(autoCommitted)
		<-releaseAuto
	}
	t.Cleanup(func() { topicAutoTitleCommittedHookForTest = nil })
	autoDone := make(chan bool, 1)
	go func() { autoDone <- app.maybeAutoTitleTopic(tab) }()
	<-autoCommitted

	renameDone := make(chan error, 1)
	go func() { renameDone <- app.RenameTopic(topic.ID, "Manual title wins") }()
	select {
	case err := <-renameDone:
		t.Fatalf("manual rename bypassed in-flight auto publication: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseAuto)
	if updated := <-autoDone; !updated {
		t.Fatal("automatic title was not applied before the manual rename")
	}
	if err := <-renameDone; err != nil {
		t.Fatal(err)
	}

	if got := loadTopicTitle(workspaceRoot, topic.ID); got != "Manual title wins" {
		t.Fatalf("authoritative title = %q", got)
	}
	if tab.TopicTitle != "Manual title wins" || tab.topicTitleSource != topicTitleSourceManual {
		t.Fatalf("tab title publication = %q source=%q", tab.TopicTitle, tab.topicTitleSource)
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("load branch metadata: ok=%v err=%v", ok, err)
	}
	if meta.TopicTitle != "Manual title wins" {
		t.Fatalf("session metadata title = %q", meta.TopicTitle)
	}
}

func TestFutureTopicSchemaWithoutLegacyReturnsVisibleReadError(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	path := config.DesktopTopicStatePath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(2, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTopicTitlesForUpdate(workspaceRoot); err == nil {
		t.Fatal("future schema without legacy unexpectedly became an empty title map")
	} else {
		var future *topicstate.FutureSchemaError
		if !errors.As(err, &future) {
			t.Fatalf("update read error = %v, want FutureSchemaError", err)
		}
	}
	_, err = NewApp().ListProjectTopics(ProjectTopicPageRequest{Scope: "project", WorkspaceRoot: workspaceRoot})
	if err == nil || !strings.Contains(err.Error(), "newer Reasonix version") {
		t.Fatalf("Wails read error = %v", err)
	}
	if strings.Contains(err.Error(), workspaceRoot) {
		t.Fatalf("Wails read error leaked workspace path: %v", err)
	}
}

func TestCorruptTopicDatabaseRecoversFromSessionMetadataWithoutLegacy(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	dir := desktopSessionDir(workspaceRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	liveID := "topic-live"
	deletedID := "topic-deleted"
	for _, entry := range []struct{ id, name, title string }{
		{liveID, "live.jsonl", "Recovered title"},
		{deletedID, "deleted.jsonl", "Must stay deleted"},
	} {
		path := filepath.Join(dir, entry.name)
		if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := agent.SaveBranchMeta(path, agent.BranchMeta{
			CreatedAt: time.Now().Add(-time.Hour), Scope: "project", WorkspaceRoot: workspaceRoot,
			TopicID: entry.id, TopicTitle: entry.title,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := updateProjectsFile(func(file *desktopProjectFile) (bool, error) {
		file.DeletedTopics = append(file.DeletedTopics, deletedID)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	dbPath := config.DesktopTopicStatePath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := setTopicCreatedAt(workspaceRoot, liveID, 5678); err != nil {
		t.Fatal(err)
	}
	snapshot, err := desktopTopicState.snapshot(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Records[liveID]; got.Title != "Recovered title" || got.CreatedAtMS != 5678 {
		t.Fatalf("recovered record = %+v", got)
	}
	if _, ok := snapshot.Records[deletedID]; ok {
		t.Fatalf("tombstoned topic was recovered: %+v", snapshot.Records[deletedID])
	}
	if matches, _ := filepath.Glob(dbPath + ".corrupt-*"); len(matches) != 1 {
		t.Fatalf("corrupt backups = %#v", matches)
	}
	for _, legacyPath := range legacyTopicPaths(workspaceRoot) {
		if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
			t.Fatalf("session recovery created legacy file %s: %v", filepath.Base(legacyPath), err)
		}
	}
	state, err := topicstate.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = state.Close()
}
