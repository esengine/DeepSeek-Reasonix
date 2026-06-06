package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestTurnTreeDataRootsOnlyContainsRealRoots(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "second answer"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})
	ctrl.SetSessionPath(agent.NewSessionPath(dir, "test"))
	if err := ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}

	data, err := turnTreeData(ctrl)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2: %+v", len(data.Nodes), data.Nodes)
	}
	if len(data.Roots) != 1 {
		t.Fatalf("roots = %d, want only the first root node: %+v", len(data.Roots), data.Roots)
	}
	if data.Roots[0].Key != data.Nodes[0].Key {
		t.Fatalf("root = %+v, want first node %+v", data.Roots[0], data.Nodes[0])
	}
	if data.Nodes[0].ParentKey != "" {
		t.Fatalf("first node parent = %q, want empty", data.Nodes[0].ParentKey)
	}
	if data.Nodes[1].ParentKey != data.Nodes[0].Key {
		t.Fatalf("second node parent = %q, want %q", data.Nodes[1].ParentKey, data.Nodes[0].Key)
	}
	if data.Nodes[0].Response != "answer" {
		t.Fatalf("first node response = %q, want answer", data.Nodes[0].Response)
	}
}

func TestPersistTurnPreviewForTabRestoresNewTopicAfterRestart(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := t.TempDir()
	workspace := t.TempDir()
	source := agent.NewSession("sys")
	source.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	source.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	source.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	source.Add(provider.Message{Role: provider.RoleAssistant, Content: "second answer"})
	sourcePath := filepath.Join(dir, "source.jsonl")
	if err := source.Save(sourcePath); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(sourcePath, agent.BranchMeta{
		Scope:         "project",
		WorkspaceRoot: workspace,
		TopicID:       "topic-source",
		TopicTitle:    "Source",
	}); err != nil {
		t.Fatal(err)
	}

	targetPath := filepath.Join(dir, "target.jsonl")
	target := agent.NewSession("sys")
	exec := agent.New(nil, nil, target, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})
	defer ctrl.Close()
	ctrl.SetSessionPath(targetPath)
	if err := agent.SaveBranchMeta(targetPath, agent.BranchMeta{
		Scope:         "project",
		WorkspaceRoot: workspace,
		TopicID:       "topic-target",
		TopicTitle:    "Target",
	}); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"target": {
			ID:            "target",
			Scope:         "project",
			WorkspaceRoot: workspace,
			TopicID:       "topic-target",
			TopicTitle:    "Target",
			Ctrl:          ctrl,
			Ready:         true,
			disabledMCP:   map[string]ServerView{},
		},
	}
	app.activeTabID = "target"

	sourceID := agent.BranchID(sourcePath)
	if err := app.JumpToTurnForTab("target", sourceID, 0); err != nil {
		t.Fatal(err)
	}
	if ctrl.SessionPath() != "" {
		t.Fatalf("jump should be an unsaved preview before persistence, got %q", ctrl.SessionPath())
	}
	if err := app.PersistTurnPreviewForTab("target"); err != nil {
		t.Fatal(err)
	}
	persisted := findTopicSession(dir, "topic-target")
	if persisted == "" {
		t.Fatalf("persisted preview session for topic-target not found")
	}
	if persisted == targetPath {
		t.Fatalf("preview should materialize to a fresh branch path, got initial target path")
	}
	if _, err := os.Stat(persisted); err != nil {
		t.Fatalf("persisted preview missing: %v", err)
	}
	loaded, err := agent.LoadSession(persisted)
	if err != nil {
		t.Fatal(err)
	}
	messages := loaded.Snapshot()
	if len(messages) != 3 {
		t.Fatalf("loaded preview messages = %d, want system + selected turn", len(messages))
	}
	if messages[1].Role != provider.RoleUser || messages[1].Content != "first" {
		t.Fatalf("loaded user message = %+v, want first", messages[1])
	}
	if messages[2].Role != provider.RoleAssistant || messages[2].Content != "answer" {
		t.Fatalf("loaded assistant message = %+v, want answer", messages[2])
	}
	meta, ok, err := agent.LoadBranchMeta(persisted)
	if err != nil || !ok {
		t.Fatalf("load persisted meta ok=%v err=%v", ok, err)
	}
	if meta.TopicID != "topic-target" || meta.TopicTitle != "Target" {
		t.Fatalf("persisted meta should keep target topic, got %+v", meta)
	}
	if meta.ParentID != sourceID || meta.ForkTurn != 0 {
		t.Fatalf("persisted meta should remember selected source node, got %+v", meta)
	}
}

func TestSubmitDisplayFromTurnPreviewRecordsMaterializedSessionDisplay(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := agent.NewSession("sys")
	source.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	source.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	sourcePath := filepath.Join(dir, "source.jsonl")
	if err := source.Save(sourcePath); err != nil {
		t.Fatal(err)
	}

	target := agent.NewSession("sys")
	exec := agent.New(nil, nil, target, agent.Options{}, event.Discard)
	runner := &appendingDesktopRunner{session: target, started: make(chan string, 1)}
	ctrl := control.New(control.Options{Runner: runner, Executor: exec, SessionDir: dir, Label: "test"})
	defer ctrl.Close()
	ctrl.SetSessionPath(agent.NewSessionPath(dir, "test"))

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"target": {
			ID:          "target",
			Scope:       "global",
			TopicID:     "topic-target",
			TopicTitle:  "Target",
			Ctrl:        ctrl,
			Ready:       true,
			disabledMCP: map[string]ServerView{},
		},
	}
	app.activeTabID = "target"

	if err := app.JumpToTurnForTab("target", agent.BranchID(sourcePath), 0); err != nil {
		t.Fatal(err)
	}
	fullPrompt := "expanded pasted prompt"
	displayPrompt := "[Pasted text #1 · 3 lines]"
	app.SubmitDisplayToTab("target", displayPrompt, fullPrompt)
	<-runner.started
	waitNotRunning(t, ctrl)
	if ctrl.SessionPath() == "" {
		t.Fatalf("submit should materialize preview")
	}
	if got := resolveSessionDisplay(dir, ctrl.SessionPath(), fullPrompt); got != displayPrompt {
		t.Fatalf("display mapping = %q, want %q", got, displayPrompt)
	}
}

func TestTurnTreeDataOnlyReturnsCurrentRoot(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.jsonl")
	first := agent.NewSession("sys")
	first.Add(provider.Message{Role: provider.RoleUser, Content: "first root"})
	if err := first.Save(firstPath); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(firstPath, agent.BranchMeta{ID: "first"}); err != nil {
		t.Fatal(err)
	}

	second := agent.NewSession("sys")
	second.Add(provider.Message{Role: provider.RoleUser, Content: "second root"})
	second.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	secondPath := filepath.Join(dir, "second.jsonl")
	exec := agent.New(nil, nil, second, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})
	ctrl.SetSessionPath(secondPath)
	if err := ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(secondPath, agent.BranchMeta{ID: "second"}); err != nil {
		t.Fatal(err)
	}

	data, err := turnTreeData(ctrl)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Roots) != 1 || data.Roots[0].BranchID != "second" {
		t.Fatalf("roots = %+v, want only second root", data.Roots)
	}
	for _, node := range data.Nodes {
		if node.RootKey != data.Roots[0].Key {
			t.Fatalf("node from another root leaked: %+v roots=%+v", node, data.Roots)
		}
	}
}

func TestTurnTreeDataReturnsEmptyWhenCurrentRootUnknown(t *testing.T) {
	dir := t.TempDir()
	other := agent.NewSession("sys")
	other.Add(provider.Message{Role: provider.RoleUser, Content: "other root"})
	otherPath := filepath.Join(dir, "other.jsonl")
	if err := other.Save(otherPath); err != nil {
		t.Fatal(err)
	}

	empty := agent.NewSession("sys")
	exec := agent.New(nil, nil, empty, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})
	ctrl.SetSessionPath(filepath.Join(dir, "missing-current.jsonl"))

	data, err := turnTreeData(ctrl)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Roots) != 0 || len(data.Nodes) != 0 || data.CurrentKey != "" {
		t.Fatalf("unknown current root should return empty focused tree, got %+v", data)
	}
}
