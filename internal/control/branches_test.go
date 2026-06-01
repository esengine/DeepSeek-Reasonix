package control

import (
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
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
	if !strings.Contains(tree, rootID) || !strings.Contains(tree, "try something") {
		t.Fatalf("tree missing expected branches:\n%s", tree)
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

func TestFormatBranchTreeMarksCurrent(t *testing.T) {
	branches := []agent.BranchInfo{
		{BranchMeta: agent.BranchMeta{ID: "root"}, Preview: "root", Turns: 1},
		{BranchMeta: agent.BranchMeta{ID: "child", ParentID: "root", Name: "child branch"}, Turns: 2},
	}
	got := FormatBranchTree(branches, "child")
	if !strings.Contains(got, "* child") {
		t.Fatalf("tree should mark current branch:\n%s", got)
	}
}
