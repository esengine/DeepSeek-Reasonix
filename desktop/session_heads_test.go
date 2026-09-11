package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncatalog"
	"reasonix/internal/store"
)

// schemaTwoTabFixture opens tab "test" on a five-message schema-2 session with
// a checkpoint at turn 1 whose boundary keeps the first three messages, and a
// workspace file the checkpoint can restore.
type schemaTwoTabFixture struct {
	app      *App
	ctrl     *control.Controller
	session  *agent.Session
	path     string
	filePath string
}

func newSchemaTwoTabFixture(t *testing.T) schemaTwoTabFixture {
	t.Helper()
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	root := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	path := agent.NewSessionPath(dir, "heads")
	ckptDir := strings.TrimSuffix(path, ".jsonl") + ".ckpt"
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(root, "a.txt")
	if err := os.WriteFile(filePath, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	mode := uint32(info.Mode().Perm())
	before, afterExists := "before", true
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{
		SchemaVersion: checkpoint.SchemaV2, Turn: 1, Time: time.Now(), Prompt: "edit", MsgIndex: 3,
		Coverage: checkpoint.CoverageComplete,
		Files: []checkpoint.FileSnap{{
			Path: "a.txt", Content: &before, SHA256: checkpoint.Digest([]byte(before)), Mode: mode,
			AfterExisted: &afterExists, AfterSHA256: checkpoint.Digest([]byte("after")), AfterMode: mode,
			CaptureSource: checkpoint.CaptureBeforeMutation,
		}},
	})
	session := agent.NewSession("")
	session.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "first"},
		{Role: provider.RoleAssistant, Content: "answer"},
		{Role: provider.RoleUser, Content: "edit"},
		{Role: provider.RoleAssistant, Content: "done"},
	})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	ag := agent.New(nil, nil, session, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: ag, Runner: ag, Sink: event.Discard, SessionDir: dir, SessionPath: path, WorkspaceRoot: root, Label: "test"})
	app := NewApp()
	app.setTestCtrl(ctrl, "deepseek/test")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = root
	app.tabs["test"].TopicID = "topic_heads"
	t.Cleanup(ctrl.Close)
	if _, ok := ctrl.SessionHead(); !ok {
		t.Fatal("fixture session must be schema 2")
	}
	return schemaTwoTabFixture{app: app, ctrl: ctrl, session: session, path: path, filePath: filePath}
}

func transcriptFilesIn(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, entry := range entries {
		if !entry.IsDir() && store.IsSessionTranscriptName(entry.Name()) {
			n++
		}
	}
	return n
}

func TestForkForTabSwitchesSchemaTwoTabInPlace(t *testing.T) {
	fx := newSchemaTwoTabFixture(t)
	meta, err := fx.app.ForkForTab("test", 1)
	if err != nil {
		t.Fatalf("ForkForTab: %v", err)
	}
	if meta.ID != "test" || !meta.Active || meta.SessionPath != fx.path || meta.SessionGeneration != 1 {
		t.Fatalf("fork meta = id %q active %v path %q generation %d, want the source tab rehydrated in place", meta.ID, meta.Active, meta.SessionPath, meta.SessionGeneration)
	}
	if len(fx.app.tabs) != 1 || fx.ctrl.SessionPath() != fx.path {
		t.Fatalf("tabs = %d path %q, want one tab on the same log", len(fx.app.tabs), fx.ctrl.SessionPath())
	}
	if got := len(fx.ctrl.History()); got != 3 {
		t.Fatalf("history after fork = %d, want the prefix before turn 1", got)
	}
	heads, err := agent.ListSessionHeads(fx.path)
	if err != nil || len(heads) != 2 || heads[1].Kind != agent.HeadKindFork || !heads[1].Selected || heads[0].MessageCount != 5 {
		t.Fatalf("heads = %+v err=%v", heads, err)
	}
	if got := transcriptFilesIn(t, filepath.Dir(fx.path)); got != 1 {
		t.Fatalf("transcript files = %d, want the fork inside the existing log", got)
	}
}

func TestCommitRewindForTabSwitchesSchemaTwoTabInPlace(t *testing.T) {
	fx := newSchemaTwoTabFixture(t)
	plan := fx.app.PreviewRewindForTab("test", 1, "both")
	if !plan.OK || !plan.CanFiles || !plan.CanConversation {
		t.Fatalf("preview = %+v", plan)
	}
	result := fx.app.CommitRewindForTab("test", plan.PlanID, 1, "both")
	if !result.OK || !result.ConversationForked || result.Branch == "" || strings.HasSuffix(result.Branch, ".jsonl") {
		t.Fatalf("commit = %+v, want a head id as the branch", result)
	}
	if result.TabID != "test" || result.Tab == nil || result.Tab.ID != "test" || result.Tab.SessionGeneration != 1 {
		t.Fatalf("commit tab wiring = tab %q meta %+v, want the source tab", result.TabID, result.Tab)
	}
	if got, err := os.ReadFile(fx.filePath); err != nil || string(got) != "before" {
		t.Fatalf("file after commit = %q err=%v", got, err)
	}
	if got := len(fx.ctrl.History()); got != 3 || fx.ctrl.SessionPath() != fx.path || len(fx.app.tabs) != 1 {
		t.Fatalf("after commit: history %d path %q tabs %d", got, fx.ctrl.SessionPath(), len(fx.app.tabs))
	}
	heads, err := agent.ListSessionHeads(fx.path)
	if err != nil || len(heads) != 2 || heads[1].ID != result.Branch || heads[1].Kind != agent.HeadKindRewind || !heads[1].Selected {
		t.Fatalf("heads = %+v err=%v", heads, err)
	}
	undo := fx.app.UndoRewindForTab("test", result.TransactionID)
	if !undo.OK {
		t.Fatalf("undo = %+v", undo)
	}
	if got, err := os.ReadFile(fx.filePath); err != nil || string(got) != "after" {
		t.Fatalf("file after undo = %q err=%v", got, err)
	}
	if got := fx.ctrl.History(); len(got) != 5 || got[4].Content != "done" {
		t.Fatalf("history after undo = %d messages, want the rewound turn back", len(got))
	}
	if undo.TabID != "test" || undo.Tab == nil || undo.Tab.SessionGeneration != 2 {
		t.Fatalf("undo tab wiring = tab %q meta %+v, want the source tab rehydrated again", undo.TabID, undo.Tab)
	}
	heads, err = agent.ListSessionHeads(fx.path)
	if err != nil || len(heads) != 2 || !heads[0].Selected || !heads[1].Retired {
		t.Fatalf("heads after undo = %+v err=%v, want main current and the empty rewind head retired", heads, err)
	}
	reloaded, err := agent.LoadSession(fx.path)
	if err != nil || len(reloaded.Messages) != 5 {
		t.Fatalf("reload after undo = %d messages err=%v, want the parent head persisted as current", len(reloaded.Messages), err)
	}
}

func TestChooseRecoveryBranchSwitchesOpenTabHeadInPlace(t *testing.T) {
	fx := newSchemaTwoTabFixture(t)
	if _, err := fx.app.ForkForTab("test", 1); err != nil {
		t.Fatal(err)
	}
	heads, _ := agent.ListSessionHeads(fx.path)
	fork := heads[1].ID
	req := RecoveryPreferenceRequest{Scope: "global", TopicID: "topic", Path: fx.path, HeadID: agent.SessionMainHead}
	if err := fx.app.ChooseRecoveryBranch(req); err != nil {
		t.Fatalf("ChooseRecoveryBranch(main): %v", err)
	}
	if got := len(fx.ctrl.History()); got != 5 || fx.ctrl.SessionPath() != fx.path {
		t.Fatalf("after choosing main: history %d path %q", got, fx.ctrl.SessionPath())
	}
	if gen := fx.app.tabs["test"].SessionGeneration; gen != 2 {
		t.Fatalf("session generation = %d, want a bump per head switch", gen)
	}
	if err := fx.app.RenameSessionHead(fx.path, fork, "alternative"); err != nil {
		t.Fatalf("RenameSessionHead: %v", err)
	}
	heads, err := agent.ListSessionHeads(fx.path)
	if err != nil || heads[1].Name != "alternative" || !heads[0].Selected {
		t.Fatalf("heads after rename = %+v err=%v", heads, err)
	}
	if err := fx.app.ChooseRecoveryBranch(RecoveryPreferenceRequest{Scope: "global", TopicID: "topic", Path: fx.path, HeadID: "missing"}); err == nil {
		t.Fatal("choosing an unknown head must fail")
	}
}

func TestGetRecoveryLineageListsHeadsAndCleansCoveredOnes(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctx := context.Background()
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog, err := sessioncatalog.Open(ctx, sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(ctx) })
	path := filepath.Join(dir, "log.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "shared question"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "shared answer"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	fork, err := session.ForkHead(path, session.Snapshot()[2].ID, agent.HeadKindFork, "alt")
	if err != nil {
		t.Fatal(err)
	}
	session.Add(provider.Message{Role: provider.RoleUser, Content: "alt question"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.UpdateBranchMeta(path, false, func(meta *agent.BranchMeta) error {
		meta.Scope, meta.TopicID, meta.TopicTitle, meta.CustomTitle = "global", "topic", "Topic", "log note"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReconcileDirectory(ctx, sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.sessionCatalog.Store(catalog)
	key := ProjectTopicKey{Scope: "global", TopicID: "topic"}

	view := app.GetRecoveryLineage(key)
	if view.State != sessionHeadLineageState || len(view.Members) != 2 || view.CleanupEligible != 1 {
		t.Fatalf("heads view = %+v", view)
	}
	main, alt := view.Members[0], view.Members[1]
	if main.HeadID != agent.SessionMainHead || main.Canonical || main.VersionNote != "log note" || main.Path != path {
		t.Fatalf("main member = %+v", main)
	}
	if alt.HeadID != fork || !alt.Canonical || !alt.Selected || alt.HeadName != "alt" || alt.VersionNote != "alt" || alt.Path != path {
		t.Fatalf("fork member = %+v", alt)
	}
	if state := app.GetSessionVersionState(key); state.ActiveVersionID != fork || state.ActivePath != path {
		t.Fatalf("version state = %+v", state)
	}

	if err := app.ChooseRecoveryBranch(RecoveryPreferenceRequest{Scope: "global", TopicID: "topic", Path: path, HeadID: agent.SessionMainHead}); err != nil {
		t.Fatalf("ChooseRecoveryBranch: %v", err)
	}
	view = app.GetRecoveryLineage(key)
	if !view.Members[0].Canonical || view.Members[1].Canonical || view.CleanupEligible != 0 {
		t.Fatalf("after selecting main = %+v", view)
	}
	if dry := app.CleanRecoveryLineage(RecoveryCleanupRequest{Scope: "global", TopicID: "topic"}); dry.Eligible != 0 {
		t.Fatalf("diverged fork must not be cleanup-eligible: %+v", dry)
	}

	if err := app.ChooseRecoveryBranch(RecoveryPreferenceRequest{Scope: "global", TopicID: "topic", Path: path, HeadID: fork}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sessionHeadQuietPeriod = time.Minute })
	sessionHeadQuietPeriod = time.Hour
	busy := app.CleanRecoveryLineage(RecoveryCleanupRequest{Scope: "global", TopicID: "topic", Apply: true})
	if busy.Eligible != 1 || busy.Busy != 1 || busy.Moved != 0 || busy.Items[0].HeadID != agent.SessionMainHead || busy.Items[0].Status != "busy" {
		t.Fatalf("cleanup inside the quiet period = %+v", busy)
	}
	sessionHeadQuietPeriod = 0
	applied := app.CleanRecoveryLineage(RecoveryCleanupRequest{Scope: "global", TopicID: "topic", Apply: true})
	if applied.Eligible != 1 || applied.Moved != 1 || applied.Items[0].Status != "retired" {
		t.Fatalf("cleanup = %+v", applied)
	}
	heads, err := agent.ListSessionHeads(path)
	if err != nil || len(heads) != 2 || !heads[0].Retired || heads[1].Retired {
		t.Fatalf("heads after cleanup = %+v err=%v", heads, err)
	}
	if view = app.GetRecoveryLineage(key); len(view.Members) != 1 || view.Members[0].HeadID != fork {
		t.Fatalf("view after cleanup = %+v", view)
	}
	if got := transcriptFilesIn(t, dir); got != 1 {
		t.Fatalf("transcript files = %d, want cleanup to leave the log alone", got)
	}
}
