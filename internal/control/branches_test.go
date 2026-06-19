package control

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestBranchAndSwitch(t *testing.T) {
	dir := t.TempDir()
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	exec.Session().Add(provider.Message{Role: provider.RoleUser, Content: "root prompt"})
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	c.SetSessionPath(agent.NewSessionPath(dir, "test"))
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	rootPath := c.SessionPath()
	rootID := agent.BranchID(rootPath)

	if _, err := c.Branch("try something"); err != nil {
		t.Fatal(err)
	}
	childPath := c.SessionPath()
	if childPath == rootPath {
		t.Fatal("branch should switch to a new session path")
	}
	meta, ok, err := agent.LoadBranchMeta(childPath)
	if err != nil || !ok {
		t.Fatalf("load child meta ok=%v err=%v", ok, err)
	}
	if meta.ParentID != rootID || meta.Name != "try something" {
		t.Fatalf("child meta = %+v, want parent %q and name", meta, rootID)
	}

	if _, err := c.SwitchBranch(rootID); err != nil {
		t.Fatal(err)
	}
	if c.SessionPath() != rootPath {
		t.Fatalf("session path = %q, want %q", c.SessionPath(), rootPath)
	}

	tree := c.BranchTreeText()
	if !strings.Contains(tree, shortBranchID(rootID)) || !strings.Contains(tree, "try something") {
		t.Fatalf("tree missing expected branches:\n%s", tree)
	}
}

func TestSwitchBranchRejectsCleanupPending(t *testing.T) {
	dir := t.TempDir()
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	exec.Session().Add(provider.Message{Role: provider.RoleUser, Content: "root prompt"})
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	c.SetSessionPath(filepath.Join(dir, "root.jsonl"))
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	rootPath := c.SessionPath()
	rootID := agent.BranchID(rootPath)

	if _, err := c.Branch("pending experiment"); err != nil {
		t.Fatal(err)
	}
	pendingPath := c.SessionPath()
	pendingID := agent.BranchID(pendingPath)
	if _, err := c.SwitchBranch(rootID); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkCleanupPending(pendingPath, "delete"); err != nil {
		t.Fatal(err)
	}

	tree := c.BranchTreeText()
	if strings.Contains(tree, "pending experiment") || strings.Contains(tree, shortBranchID(pendingID)) {
		t.Fatalf("tree leaked cleanup-pending branch:\n%s", tree)
	}
	if _, err := c.SwitchBranch(pendingID); err == nil {
		t.Fatal("SwitchBranch cleanup-pending id error = nil, want not found")
	}
	if c.SessionPath() != rootPath {
		t.Fatalf("session path changed to %q, want %q", c.SessionPath(), rootPath)
	}
	if _, err := c.SwitchBranch(pendingPath); err == nil {
		t.Fatal("SwitchBranch cleanup-pending path error = nil, want not found")
	}
	if c.SessionPath() != rootPath {
		t.Fatalf("session path changed to %q, want %q", c.SessionPath(), rootPath)
	}
}

func TestBranchResetsTwoModelPlannerContext(t *testing.T) {
	dir := t.TempDir()
	planner := &recordingProvider{name: "planner", streams: [][]provider.Chunk{
		textTurn("OLD PLAN: inspect alpha.go"),
		textTurn("BRANCH PLAN: inspect beta.go"),
	}}
	execProv := &recordingProvider{name: "executor", streams: [][]provider.Chunk{
		textTurn("old done"),
		textTurn("branch done"),
	}}
	exec := agent.New(execProv, tool.NewRegistry(), agent.NewSession("exec sys"), agent.Options{}, event.Discard)
	coord := agent.NewCoordinator(planner, agent.NewSession("planner sys"), nil, tool.NewRegistry(), agent.Options{}, exec, 0, event.Discard, nil)
	c := New(Options{Runner: coord, Executor: exec, SystemPrompt: "exec sys", SessionDir: dir, SessionPath: filepath.Join(dir, "root.jsonl"), Label: "test"})

	if err := c.Run(context.Background(), "old task alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Branch("child"); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background(), "branch task beta"); err != nil {
		t.Fatal(err)
	}

	if len(planner.requests) != 2 {
		t.Fatalf("planner requests = %d, want 2", len(planner.requests))
	}
	second := requestMessagesText(planner.requests[1].Messages)
	if strings.Contains(second, "old task alpha") || strings.Contains(second, "OLD PLAN") {
		t.Fatalf("branch planner request leaked previous session context:\n%s", second)
	}
	if !strings.Contains(second, "branch task beta") {
		t.Fatalf("branch planner request missing current task:\n%s", second)
	}
}

func TestSwitchBranchResetsTwoModelPlannerContext(t *testing.T) {
	dir := t.TempDir()
	planner := &recordingProvider{name: "planner", streams: [][]provider.Chunk{
		textTurn("ROOT PLAN: inspect alpha.go"),
		textTurn("CHILD PLAN: inspect beta.go"),
		textTurn("ROOT AGAIN PLAN: inspect gamma.go"),
	}}
	execProv := &recordingProvider{name: "executor", streams: [][]provider.Chunk{
		textTurn("root done"),
		textTurn("child done"),
		textTurn("root again done"),
	}}
	exec := agent.New(execProv, tool.NewRegistry(), agent.NewSession("exec sys"), agent.Options{}, event.Discard)
	coord := agent.NewCoordinator(planner, agent.NewSession("planner sys"), nil, tool.NewRegistry(), agent.Options{}, exec, 0, event.Discard, nil)
	rootPath := filepath.Join(dir, "root.jsonl")
	c := New(Options{Runner: coord, Executor: exec, SystemPrompt: "exec sys", SessionDir: dir, SessionPath: rootPath, Label: "test"})

	if err := c.Run(context.Background(), "root task alpha"); err != nil {
		t.Fatal(err)
	}
	rootID := agent.BranchID(c.SessionPath())
	if _, err := c.Branch("child"); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background(), "child task beta"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SwitchBranch(rootID); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background(), "root task gamma"); err != nil {
		t.Fatal(err)
	}

	if len(planner.requests) != 3 {
		t.Fatalf("planner requests = %d, want 3", len(planner.requests))
	}
	third := requestMessagesText(planner.requests[2].Messages)
	if strings.Contains(third, "child task beta") || strings.Contains(third, "CHILD PLAN") {
		t.Fatalf("switched planner request leaked previous branch context:\n%s", third)
	}
	if !strings.Contains(third, "root task gamma") {
		t.Fatalf("switched planner request missing current task:\n%s", third)
	}
}

func TestSubmitBranchHonorsNumericTurnTarget(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first prompt"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "first answer"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "second prompt"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	c.SetSessionPath(agent.NewSessionPath(dir, "test"))
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	rootPath := c.SessionPath()

	c.mu.Lock()
	c.cpBound[1] = 3 // displayed turn 2 starts before "second prompt"
	c.mu.Unlock()

	c.Submit("/branch 2 experiment")
	if c.SessionPath() == rootPath {
		t.Fatal("Submit /branch <turn> should switch to a forked session")
	}
	meta, ok, err := agent.LoadBranchMeta(c.SessionPath())
	if err != nil || !ok {
		t.Fatalf("load branch meta ok=%v err=%v", ok, err)
	}
	if meta.ForkTurn != 1 || meta.ForkMessageIndex != 3 || meta.Name != "experiment" {
		t.Fatalf("meta = %+v, want turn 1, msg index 3, name experiment", meta)
	}
	if got := len(c.History()); got != 3 {
		t.Fatalf("forked history length = %d, want 3", got)
	}
}

func TestParseBranchTarget(t *testing.T) {
	turn, name, fromTurn, err := ParseBranchTarget("3 experiment")
	if err != nil || !fromTurn || turn != 3 || name != "experiment" {
		t.Fatalf("ParseBranchTarget numeric = (%d,%q,%v,%v)", turn, name, fromTurn, err)
	}
	turn, name, fromTurn, err = ParseBranchTarget("experiment")
	if err != nil || fromTurn || turn != 0 || name != "experiment" {
		t.Fatalf("ParseBranchTarget name = (%d,%q,%v,%v)", turn, name, fromTurn, err)
	}
	if _, _, _, err = ParseBranchTarget("0 bad"); err == nil {
		t.Fatal("ParseBranchTarget should reject non-positive turns")
	}
}

func TestSubmitSwitchEmitsErrorNotice(t *testing.T) {
	var notices []string
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{
		Executor: exec,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})

	c.Submit("/switch")
	if len(notices) == 0 {
		t.Fatal("/switch with empty ref should emit an error notice")
	}
	if !strings.Contains(notices[len(notices)-1], "usage") {
		t.Fatalf("notice = %q, want usage hint", notices[len(notices)-1])
	}

	notices = notices[:0]
	c.Submit("/switch nonexistent")
	if len(notices) == 0 {
		t.Fatal("/switch with unknown ref should emit an error notice")
	}
}

func TestSubmitBranchEmitsErrorNoticeWhileRunning(t *testing.T) {
	var notices []string
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{
		Executor:   exec,
		SessionDir: t.TempDir(),
		Label:      "test",
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})
	c.SetSessionPath(agent.NewSessionPath(c.sessionDir, "test"))

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	c.Submit("/branch experiment")
	if len(notices) == 0 {
		t.Fatal("/branch while running should emit an error notice")
	}
	if !strings.Contains(notices[len(notices)-1], "cannot branch") {
		t.Fatalf("notice = %q, want 'cannot branch' error", notices[len(notices)-1])
	}
}

func TestFormatBranchTreeMarksCurrent(t *testing.T) {
	branches := []agent.BranchInfo{
		{BranchMeta: agent.BranchMeta{ID: "root"}, Preview: "root", Turns: 1},
		{BranchMeta: agent.BranchMeta{ID: "child", ParentID: "root", Name: "child branch"}, Turns: 2},
	}
	got := FormatBranchTree(branches, "child")
	if !strings.Contains(got, "child branch  2 turns  current") {
		t.Fatalf("tree should mark current branch:\n%s", got)
	}
	if strings.Contains(got, "*") {
		t.Fatalf("tree should not use duplicate current markers:\n%s", got)
	}
}

func TestFormatBranchTreeUsesCompactVisualRows(t *testing.T) {
	branches := []agent.BranchInfo{
		{
			BranchMeta: agent.BranchMeta{ID: "20260601-033830.928433000-deepseek-v4-flash"},
			Preview:    "你是谁",
			Turns:      3,
		},
		{
			BranchMeta: agent.BranchMeta{
				ID:       "20260601-033937.165828000-deepseek-v4-flash",
				ParentID: "20260601-033830.928433000-deepseek-v4-flash",
			},
			Preview: `{ "code": 0, "msg": "success", "data": { "rows": [] } }`,
			Turns:   1,
		},
	}
	got := FormatBranchTree(branches, "20260601-033937.165828000-deepseek-v4-flash")
	checks := []string{
		"└─",
		"0601-033937.165",
		"JSON response: success",
		"1 turn",
		"current",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Fatalf("tree missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "20260601-033937.165828000-deepseek-v4-flash") {
		t.Fatalf("tree should use compact branch IDs:\n%s", got)
	}
	if strings.Contains(got, `"data"`) {
		t.Fatalf("tree should summarize JSON-like previews:\n%s", got)
	}
}

func TestResolveBranchAcceptsDisplayedShortID(t *testing.T) {
	branches := []agent.BranchInfo{
		{BranchMeta: agent.BranchMeta{ID: "20260601-033937.165828000-deepseek-v4-flash"}},
	}
	got, err := resolveBranch(branches, "0601-033937.165")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != branches[0].ID {
		t.Fatalf("branch = %q, want %q", got.ID, branches[0].ID)
	}
}
