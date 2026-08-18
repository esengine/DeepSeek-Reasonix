package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestDesktopRewindCommitAndUndoUseAuthoritativeControllerState(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	root := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	ckptDir := sessionPath[:len(sessionPath)-len(".jsonl")] + ".ckpt"
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(root, "a.txt")
	if err := os.WriteFile(filePath, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	diskMode := uint32(fileInfo.Mode().Perm())
	before := "before"
	afterExists := true
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{
		SchemaVersion: checkpoint.SchemaV2, Turn: 1, Time: time.Now(), Prompt: "edit", MsgIndex: 3,
		Coverage: checkpoint.CoverageComplete,
		Files: []checkpoint.FileSnap{{
			Path: "a.txt", Content: &before, SHA256: checkpoint.Digest([]byte(before)), Mode: diskMode,
			AfterExisted: &afterExists, AfterSHA256: checkpoint.Digest([]byte("after")), AfterMode: diskMode,
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
	if err := session.Save(sessionPath); err != nil {
		t.Fatal(err)
	}
	ag := agent.New(nil, nil, session, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: ag, Runner: ag, SessionDir: dir, SessionPath: sessionPath, WorkspaceRoot: root, Label: "test"})
	app := NewApp()
	app.setTestCtrl(ctrl, "test")
	app.tabs["test"].WorkspaceRoot = root
	defer func() {
		for _, tab := range app.tabs {
			if tab != nil && tab.Ctrl != nil {
				tab.Ctrl.Close()
			}
		}
	}()

	plan := app.PreviewRewindForTab("test", 1, "both")
	if !plan.OK || !plan.CanFiles || !plan.CanConversation {
		t.Fatalf("preview = %+v", plan)
	}
	result := app.CommitRewindForTab("test", plan.PlanID, 1, "both")
	if !result.OK || !result.UndoAvailable || result.TransactionID == "" {
		t.Fatalf("commit = %+v", result)
	}
	if !result.ConversationForked || result.Branch == "" || result.TabID == "" || result.Tab == nil {
		t.Fatalf("commit fork wiring = %+v, want branch and tab", result)
	}
	if got, err := os.ReadFile(filePath); err != nil || string(got) != before {
		t.Fatalf("file after commit = %q err=%v", got, err)
	}
	if got := ctrl.History(); len(got) != 5 {
		t.Fatalf("source controller history after commit = %d, want 5", len(got))
	}
	if got := app.HistoryForTab("test"); len(got) != 5 {
		t.Fatalf("source desktop history after commit = %d, want 5", len(got))
	}
	if got := ctrl.SessionPath(); got != sessionPath {
		t.Fatalf("source session path = %q, want %q", got, sessionPath)
	}
	forkTab := app.tabs[result.TabID]
	if forkTab == nil || forkTab.SessionPath != result.Branch {
		t.Fatalf("fork tab = %+v, want session %q", forkTab, result.Branch)
	}
	if app.activeTabID != result.TabID {
		t.Fatalf("active tab = %q, want fork %q", app.activeTabID, result.TabID)
	}
	forkSess, err := agent.LoadSession(result.Branch)
	if err != nil {
		t.Fatalf("LoadSession(fork): %v", err)
	}
	var forkContents []string
	for _, msg := range forkSess.Messages {
		forkContents = append(forkContents, msg.Content)
	}
	if !slices.Contains(forkContents, "first") || !slices.Contains(forkContents, "answer") {
		t.Fatalf("fork history missing prefix: %q", forkContents)
	}
	if slices.Contains(forkContents, "edit") || slices.Contains(forkContents, "done") {
		t.Fatalf("fork history still contains rewound turn: %q", forkContents)
	}

	parentMessages := session.Snapshot()
	parentMessages = append(parentMessages, provider.Message{Role: provider.RoleUser, Content: "parent continued"})
	session.Replace(parentMessages)
	undo := app.UndoRewindForTab("test", result.TransactionID)
	if !undo.OK {
		t.Fatalf("undo = %+v", undo)
	}
	if got, err := os.ReadFile(filePath); err != nil || string(got) != "after" {
		t.Fatalf("file after undo = %q err=%v", got, err)
	}
	if got := ctrl.History(); len(got) != 6 || got[5].Content != "parent continued" {
		t.Fatalf("controller history after undo = %+v, want continued parent", got)
	}
	if got := app.HistoryForTab("test"); len(got) != 6 || got[5].Content != "parent continued" {
		t.Fatalf("desktop history after undo = %+v, want continued parent", got)
	}
}

func TestAttachForkedRewindTabFailsClosedWhenSourceIsGone(t *testing.T) {
	app := NewApp()
	source := &WorkspaceTab{ID: "removed"}
	result := app.attachForkedRewindTab(source, RewindResultView{
		OK:                 true,
		ConversationForked: true,
		Branch:             filepath.Join(t.TempDir(), "fork.jsonl"),
	})
	if result.OK || !result.Partial {
		t.Fatalf("result = %+v, want failed partial result", result)
	}
	if result.Error != rewindForkAttachError {
		t.Fatalf("error = %q, want stable path-free error", result.Error)
	}
	if result.TabID != "" || result.Tab != nil {
		t.Fatalf("failed attach exposed target tab: %+v", result)
	}
}

func seedCheckpoint(t *testing.T, ckptDir string, c checkpoint.Checkpoint) {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ckptDir, "turn-"+strconv.Itoa(c.Turn)+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertCheckpointFilesEncodeAsArray(t *testing.T, metas []CheckpointMeta) {
	t.Helper()
	raw, err := json.Marshal(metas)
	if err != nil {
		t.Fatal(err)
	}
	var payload []struct {
		Files json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for i, item := range payload {
		if string(item.Files) == "null" {
			t.Fatalf("checkpoint %d files encoded as null; frontend expects []", i)
		}
		if len(item.Files) == 0 || item.Files[0] != '[' {
			t.Fatalf("checkpoint %d files encoded as %s, want JSON array", i, item.Files)
		}
	}
}

// TestCheckpointsCanCodePropagatesToEarlierTurns covers #3438: RestoreCode(turn)
// reverts files touched in that turn or any later one, so a turn with no file
// changes of its own can still rewind code when a later turn changed files. The
// desktop CanCode flag must reflect that suffix capability, not just the turn's
// own paths.
func TestCheckpointsCanCodePropagatesToEarlierTurns(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	ckptDir := sessionPath[:len(sessionPath)-len(".jsonl")] + ".ckpt"
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "old"
	afterExists := true
	now := time.Now()
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{SchemaVersion: checkpoint.SchemaV2, Turn: 0, Time: now, Prompt: "ask only", MsgIndex: 0})
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{SchemaVersion: checkpoint.SchemaV2, Turn: 1, Time: now, Prompt: "edit a file", MsgIndex: 2,
		Coverage: checkpoint.CoverageComplete,
		Files: []checkpoint.FileSnap{{
			Path: "a.txt", Content: &content, SHA256: checkpoint.Digest([]byte(content)),
			AfterExisted: &afterExists, AfterSHA256: checkpoint.Digest([]byte("new")), CaptureSource: checkpoint.CaptureBeforeMutation,
		}}})
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{SchemaVersion: checkpoint.SchemaV2, Turn: 2, Time: now, Prompt: "ask again", MsgIndex: 4})

	ag := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: ag, SessionDir: dir, Label: "test"})
	ctrl.SetSessionPath(sessionPath)

	app := &App{}
	app.setTestCtrl(ctrl, "test")

	metas := app.CheckpointsForTab("test")
	if len(metas) != 3 {
		t.Fatalf("checkpoints = %d, want 3", len(metas))
	}
	got := map[int]bool{}
	for _, m := range metas {
		got[m.Turn] = m.CanCode
	}
	if !got[0] {
		t.Error("turn 0 (no files of its own) should allow code rewind — turn 1 changed files")
	}
	if !got[1] {
		t.Error("turn 1 changed files, should allow code rewind")
	}
	if got[2] {
		t.Error("turn 2 is after the last file-bearing turn, should NOT allow code rewind")
	}
	if metas[0].TurnFileCount != 0 {
		t.Fatalf("turn 0 file count = %d, want 0 for this turn", metas[0].TurnFileCount)
	}
	if metas[1].TurnFileCount != 1 {
		t.Fatalf("turn 1 file count = %d, want 1 for this turn", metas[1].TurnFileCount)
	}
	if len(metas[0].Files) != 1 || metas[0].Files[0] != "a.txt" {
		t.Fatalf("turn 0 cumulative files = %#v, want [a.txt]", metas[0].Files)
	}
	if metas[0].FileCount != 1 || metas[0].FilesTruncated {
		t.Fatalf("turn 0 file summary = count %d truncated %v, want count 1 truncated false", metas[0].FileCount, metas[0].FilesTruncated)
	}
	if len(metas[2].Files) != 0 {
		t.Fatalf("turn 2 cumulative files = %#v, want empty", metas[2].Files)
	}
	assertCheckpointFilesEncodeAsArray(t, metas)
}

func TestCheckpointsCanCodeDoesNotReenableLegacySuffix(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	ckptDir := sessionPath[:len(sessionPath)-len(".jsonl")] + ".ckpt"
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "old"
	now := time.Now()
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{SchemaVersion: checkpoint.SchemaV2, Turn: 0, Time: now, Prompt: "before", MsgIndex: 0})
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{Turn: 1, Time: now, Prompt: "legacy edit", MsgIndex: 2,
		Files: []checkpoint.FileSnap{{Path: "a.txt", Content: &content}}})

	ag := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: ag, SessionDir: dir, Label: "test"})
	ctrl.SetSessionPath(sessionPath)
	app := &App{}
	app.setTestCtrl(ctrl, "test")

	metas := app.CheckpointsForTab("test")
	if len(metas) != 2 {
		t.Fatalf("checkpoints = %d, want 2", len(metas))
	}
	for _, meta := range metas {
		if meta.CanCode {
			t.Fatalf("legacy suffix re-enabled code rewind at turn %d: %+v", meta.Turn, meta)
		}
	}
}

func TestCheckpointsForTabLimitsCumulativeFilePreview(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	ckptDir := sessionPath[:len(sessionPath)-len(".jsonl")] + ".ckpt"
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "old"
	files := make([]checkpoint.FileSnap, 0, checkpointFilePreviewLimit+5)
	for i := range checkpointFilePreviewLimit + 5 {
		files = append(files, checkpoint.FileSnap{Path: "file-" + strconv.Itoa(1000+i) + ".txt", Content: &content})
	}
	now := time.Now()
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{Turn: 0, Time: now, Prompt: "before edits", MsgIndex: 0})
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{Turn: 1, Time: now, Prompt: "edit many files", MsgIndex: 2, Files: files})

	ag := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: ag, SessionDir: dir, Label: "test"})
	ctrl.SetSessionPath(sessionPath)

	app := &App{}
	app.setTestCtrl(ctrl, "test")

	metas := app.CheckpointsForTab("test")
	if len(metas) != 2 {
		t.Fatalf("checkpoints = %d, want 2", len(metas))
	}
	if metas[0].FileCount != checkpointFilePreviewLimit+5 {
		t.Fatalf("turn 0 cumulative file count = %d, want %d", metas[0].FileCount, checkpointFilePreviewLimit+5)
	}
	if len(metas[0].Files) != checkpointFilePreviewLimit {
		t.Fatalf("turn 0 preview files = %d, want %d", len(metas[0].Files), checkpointFilePreviewLimit)
	}
	if !metas[0].FilesTruncated {
		t.Fatal("turn 0 should mark file preview as truncated")
	}
	if metas[0].TurnFileCount != 0 {
		t.Fatalf("turn 0 file count = %d, want 0 for this turn", metas[0].TurnFileCount)
	}
	if metas[1].TurnFileCount != checkpointFilePreviewLimit+5 {
		t.Fatalf("turn 1 file count = %d, want %d", metas[1].TurnFileCount, checkpointFilePreviewLimit+5)
	}
	assertCheckpointFilesEncodeAsArray(t, metas)
}
