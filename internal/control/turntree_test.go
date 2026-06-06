package control

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/hook"
	"reasonix/internal/provider"
)

func TestJumpToTurnUsesTemporaryPreviewUntilMaterialized(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "second answer"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	c.SetSessionPath(agent.NewSessionPath(dir, "test"))
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	rootID := agent.BranchID(c.SessionPath())
	if err := agent.SaveBranchMeta(c.SessionPath(), agent.BranchMeta{
		Scope:         "project",
		WorkspaceRoot: filepath.Join(dir, "workspace"),
		TopicID:       "topic-project",
		TopicTitle:    "Project Topic",
	}); err != nil {
		t.Fatal(err)
	}

	if err := c.JumpToTurn(rootID, 0); err != nil {
		t.Fatal(err)
	}
	if c.SessionPath() != "" {
		t.Fatalf("jump should enter an unsaved temporary preview, got path %q", c.SessionPath())
	}
	history := c.History()
	if len(history) != 3 {
		t.Fatalf("jumped history length = %d, want 3", len(history))
	}
	if history[1].Role != provider.RoleUser || history[1].Content != "first" {
		t.Fatalf("user message not preserved exactly: %+v", history[1])
	}
	if history[2].Role != provider.RoleAssistant || history[2].Content != "answer" {
		t.Fatalf("assistant reply from selected turn should be preserved: %+v", history[2])
	}

	tree, err := c.TurnTree()
	if err != nil {
		t.Fatal(err)
	}
	flat := tree.Flatten()
	if len(flat) == 0 || !flat[0].IsCurrent || flat[0].BranchID != rootID || flat[0].Turn != 0 {
		t.Fatalf("tree should show current position on selected parent node: %+v", flat)
	}

	if err := c.materializeJumpOrigin(""); err != nil {
		t.Fatal(err)
	}
	meta, ok, err := agent.LoadBranchMeta(c.SessionPath())
	if err != nil || !ok {
		t.Fatalf("load branch meta ok=%v err=%v", ok, err)
	}
	if meta.ParentID != rootID || meta.ForkTurn != 0 || meta.ForkMessageIndex != 3 {
		t.Fatalf("meta = %+v, want parent %q fork turn 0 message index 3", meta, rootID)
	}
	if meta.Scope != "project" || meta.TopicID != "topic-project" || meta.TopicTitle != "Project Topic" {
		t.Fatalf("branch should inherit project topic metadata, got %+v", meta)
	}
	if meta.WorkspaceRoot != filepath.Join(dir, "workspace") {
		t.Fatalf("branch should inherit workspace root, got %q", meta.WorkspaceRoot)
	}
}

func TestJumpPreviewMaterializesIntoTargetTopic(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "second answer"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	sourcePath := agent.NewSessionPath(dir, "source")
	c.SetSessionPath(sourcePath)
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	sourceID := agent.BranchID(sourcePath)
	if err := agent.SaveBranchMeta(sourcePath, agent.BranchMeta{
		Scope:         "project",
		WorkspaceRoot: filepath.Join(dir, "workspace"),
		TopicID:       "topic-source",
		TopicTitle:    "Source Topic",
	}); err != nil {
		t.Fatal(err)
	}

	targetPath := agent.NewSessionPath(dir, "target")
	c.SetSessionPath(targetPath)
	if err := agent.SaveBranchMeta(targetPath, agent.BranchMeta{
		Scope:         "project",
		WorkspaceRoot: filepath.Join(dir, "workspace"),
		TopicID:       "topic-target",
		TopicTitle:    "Target Topic",
	}); err != nil {
		t.Fatal(err)
	}

	if err := c.JumpToTurn(sourceID, 0); err != nil {
		t.Fatal(err)
	}
	if err := c.JumpToTurn(sourceID, 1); err != nil {
		t.Fatal(err)
	}
	if err := c.materializeJumpOrigin(""); err != nil {
		t.Fatal(err)
	}
	meta, ok, err := agent.LoadBranchMeta(c.SessionPath())
	if err != nil || !ok {
		t.Fatalf("load branch meta ok=%v err=%v", ok, err)
	}
	if meta.TopicID != "topic-target" || meta.TopicTitle != "Target Topic" {
		t.Fatalf("branch should materialize into target topic, got %+v", meta)
	}
	if meta.ParentID != sourceID || meta.ForkTurn != 1 {
		t.Fatalf("branch should still fork from source node, got %+v", meta)
	}
}

func TestBranchFromJumpPreviewPreservesName(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "second answer"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	c.SetSessionPath(agent.NewSessionPath(dir, "test"))
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	rootID := agent.BranchID(c.SessionPath())

	if err := c.JumpToTurn(rootID, 0); err != nil {
		t.Fatal(err)
	}
	path, err := c.Branch(" experiment ")
	if err != nil {
		t.Fatal(err)
	}
	if path == "" || path != c.SessionPath() {
		t.Fatalf("Branch returned path %q, session path %q", path, c.SessionPath())
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("load branch meta ok=%v err=%v", ok, err)
	}
	if meta.Name != "experiment" {
		t.Fatalf("branch name = %q, want experiment; meta=%+v", meta.Name, meta)
	}
	if meta.ParentID != rootID || meta.ForkTurn != 0 || meta.ForkMessageIndex != 3 {
		t.Fatalf("meta = %+v, want parent %q fork turn 0 message index 3", meta, rootID)
	}
}

func TestRepeatedJumpToTurnReusesTemporaryPreview(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "second answer"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	c.SetSessionPath(agent.NewSessionPath(dir, "test"))
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	rootID := agent.BranchID(c.SessionPath())

	if err := c.JumpToTurn(rootID, 0); err != nil {
		t.Fatal(err)
	}
	if err := c.JumpToTurn(rootID, 1); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("jump previews should not create branch files, got %d files: %v", len(files), files)
	}
	if _, err := os.Stat(c.SessionPath()); c.SessionPath() != "" || err == nil {
		t.Fatalf("preview should remain unsaved, path=%q err=%v", c.SessionPath(), err)
	}
}

func TestBlockedPromptDoesNotMaterializeJumpPreview(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "second answer"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	hooks := hook.NewRunner(
		[]hook.ResolvedHook{{HookConfig: hook.HookConfig{Command: "deny"}, Event: hook.UserPromptSubmit}},
		"",
		func(context.Context, hook.SpawnInput) hook.SpawnResult {
			return hook.SpawnResult{ExitCode: 2, Stderr: "blocked"}
		},
		nil,
	)
	c := New(Options{Runner: appendingRunner{session: sess}, Executor: exec, SessionDir: dir, Label: "test", Hooks: hooks})
	c.SetSessionPath(agent.NewSessionPath(dir, "test"))
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	rootID := agent.BranchID(c.SessionPath())

	if err := c.JumpToTurn(rootID, 0); err != nil {
		t.Fatal(err)
	}
	if err := c.runTurnWithRaw(context.Background(), "blocked prompt", "blocked prompt"); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("blocked prompt should not materialize preview, got files: %v", files)
	}
	if c.SessionPath() != "" {
		t.Fatalf("blocked prompt should leave preview unsaved, got path %q", c.SessionPath())
	}
	c.mu.Lock()
	pending := len(c.pendingSessionDisplays)
	c.mu.Unlock()
	if pending != 0 {
		t.Fatalf("blocked prompt should clear pending display records, got %d", pending)
	}
}

func TestPreviewStateClearsPendingSessionDisplays(t *testing.T) {
	c := New(Options{})
	record := func(string, string, string) error { return nil }

	c.queueSessionDisplay("same raw", "short", record)
	c.ClearJumpOrigin()
	c.mu.Lock()
	pending := len(c.pendingSessionDisplays)
	c.mu.Unlock()
	if pending != 0 {
		t.Fatalf("ClearJumpOrigin should clear pending display records, got %d", pending)
	}

	c.queueSessionDisplay("same raw", "short", record)
	c.SetSessionPath("next.jsonl")
	c.mu.Lock()
	pending = len(c.pendingSessionDisplays)
	c.mu.Unlock()
	if pending != 0 {
		t.Fatalf("SetSessionPath should clear pending display records, got %d", pending)
	}
}

func TestSubmitDisplayClearsPendingForNonTurnInput(t *testing.T) {
	c := New(Options{})
	c.SubmitDisplay("/unknown", "short", func(string, string, string) error { return nil })

	c.mu.Lock()
	pending := len(c.pendingSessionDisplays)
	c.mu.Unlock()
	if pending != 0 {
		t.Fatalf("non-turn submit should clear pending display records, got %d", pending)
	}
}

func TestSubmitDisplayWhileRunningDoesNotQueue(t *testing.T) {
	c := New(Options{})
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	c.SubmitDisplay("same raw", "short", func(string, string, string) error { return nil })

	c.mu.Lock()
	pending := len(c.pendingSessionDisplays)
	c.mu.Unlock()
	if pending != 0 {
		t.Fatalf("running submit should not queue pending display records, got %d", pending)
	}
}

func TestMaterializeJumpPreviewRefusesWhileRunning(t *testing.T) {
	c := New(Options{SessionDir: t.TempDir()})
	c.mu.Lock()
	c.running = true
	c.jumpOrigin = jumpOrigin{active: true}
	c.mu.Unlock()

	if err := c.MaterializeJumpPreview(); err == nil {
		t.Fatalf("MaterializeJumpPreview should fail while running")
	}
}
