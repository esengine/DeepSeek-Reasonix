package agentteam

import (
	"testing"
)

func TestNewTeam(t *testing.T) {
	team := NewTeam("test-team", "/workspace")
	if team.Name() != "test-team" {
		t.Errorf("expected name 'test-team', got %q", team.Name())
	}
	if team.Status() != TeamActive {
		t.Errorf("expected status active, got %s", team.Status())
	}
	if team.Workspace() != "/workspace" {
		t.Errorf("expected workspace /workspace, got %q", team.Workspace())
	}
}

func TestTeamMembers(t *testing.T) {
	team := NewTeam("test-team", "/workspace")

	member := TeamMember{
		ID:   "member-1",
		Name: "Alice",
		Role: "developer",
	}

	team.AddMember(member)
	members := team.Members()
	if len(members) != 1 {
		t.Errorf("expected 1 member, got %d", len(members))
	}
	if members[0].Name != "Alice" {
		t.Errorf("expected member name Alice, got %s", members[0].Name)
	}

	got, ok := team.GetMember("member-1")
	if !ok {
		t.Error("should find member-1")
	}
	if got.Role != "developer" {
		t.Errorf("expected role developer, got %s", got.Role)
	}

	team.UpdateMemberStatus("member-1", "working")
	got, _ = team.GetMember("member-1")
	if got.Status != "working" {
		t.Errorf("expected status working, got %s", got.Status)
	}

	team.RemoveMember("member-1")
	if len(team.Members()) != 0 {
		t.Errorf("expected 0 members after removal, got %d", len(team.Members()))
	}
}

func TestTeamDescription(t *testing.T) {
	team := NewTeam("test-team", "/workspace")
	team.SetDescription("A test team")
	if team.Description() != "A test team" {
		t.Errorf("expected description 'A test team', got %q", team.Description())
	}
}

func TestTeamLead(t *testing.T) {
	team := NewTeam("test-team", "/workspace")
	team.SetLead("lead-1")
	if team.LeadID() != "lead-1" {
		t.Errorf("expected lead ID 'lead-1', got %q", team.LeadID())
	}
}

func TestTaskList(t *testing.T) {
	tl := NewTaskList("")

	id, err := tl.Create(Task{
		Title:       "Test task",
		Description: "A test task",
		Priority:    5,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty task ID")
	}

	if tl.Len() != 1 {
		t.Errorf("expected 1 task, got %d", tl.Len())
	}

	task, ok := tl.Get(id)
	if !ok {
		t.Fatal("should find task by ID")
	}
	if task.Title != "Test task" {
		t.Errorf("expected title 'Test task', got %q", task.Title)
	}
	if task.Status != TaskPending {
		t.Errorf("expected status pending, got %s", task.Status)
	}

	err = tl.Update(id, Task{Status: TaskInProgress, Assignee: "alice"})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	task, _ = tl.Get(id)
	if task.Status != TaskInProgress {
		t.Errorf("expected status in_progress, got %s", task.Status)
	}
	if task.Assignee != "alice" {
		t.Errorf("expected assignee alice, got %s", task.Assignee)
	}

	pending := tl.ByStatus(TaskPending)
	if len(pending) != 0 {
		t.Errorf("expected 0 pending tasks, got %d", len(pending))
	}

	inProgress := tl.ByStatus(TaskInProgress)
	if len(inProgress) != 1 {
		t.Errorf("expected 1 in_progress task, got %d", len(inProgress))
	}

	byAssignee := tl.ByAssignee("alice")
	if len(byAssignee) != 1 {
		t.Errorf("expected 1 task for alice, got %d", len(byAssignee))
	}

	err = tl.Delete(id)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if tl.Len() != 0 {
		t.Errorf("expected 0 tasks after delete, got %d", tl.Len())
	}
}

func TestTaskDependencies(t *testing.T) {
	tl := NewTaskList("")

	id1, _ := tl.Create(Task{Title: "Task 1"})
	id2, _ := tl.Create(Task{Title: "Task 2", Dependencies: []string{id1}})

	task2, _ := tl.Get(id2)
	if len(task2.Dependencies) != 1 {
		t.Errorf("expected 1 dependency, got %d", len(task2.Dependencies))
	}

	claimed, ok := tl.Claim("worker-1")
	if !ok {
		t.Fatal("should be able to claim task 1")
	}
	if claimed.ID != id1 {
		t.Errorf("expected to claim task 1 first, got %s", claimed.ID)
	}

	err := tl.Update(id1, Task{Status: TaskCompleted})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	claimed2, ok := tl.Claim("worker-2")
	if !ok {
		t.Fatal("should be able to claim task 2 after task 1 is done")
	}
	if claimed2.ID != id2 {
		t.Errorf("expected to claim task 2, got %s", claimed2.ID)
	}
}

func TestMailbox(t *testing.T) {
	mb := NewMailbox("")

	id, err := mb.Send("alice", "bob", "Hello", "How are you?", "chat")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty message ID")
	}

	inbox := mb.Inbox("bob")
	if len(inbox) != 1 {
		t.Errorf("expected 1 message in bob's inbox, got %d", len(inbox))
	}
	if inbox[0].Subject != "Hello" {
		t.Errorf("expected subject 'Hello', got %q", inbox[0].Subject)
	}

	unread := mb.Unread("bob")
	if len(unread) != 1 {
		t.Errorf("expected 1 unread for bob, got %d", len(unread))
	}

	mb.MarkRead(id)
	unread = mb.Unread("bob")
	if len(unread) != 0 {
		t.Errorf("expected 0 unread after marking read, got %d", len(unread))
	}

	_, err = mb.Broadcast("alice", "Announcement", "Team meeting at 3pm")
	if err != nil {
		t.Fatalf("Broadcast failed: %v", err)
	}

	inboxAll := mb.Inbox("charlie")
	if len(inboxAll) != 1 {
		t.Errorf("expected 1 broadcast in charlie's inbox, got %d", len(inboxAll))
	}

	count := mb.UnreadCount("bob")
	if count != 1 {
		t.Errorf("expected 1 unread count for bob (broadcast), got %d", count)
	}

	mb.MarkAllRead("bob")
	count = mb.UnreadCount("bob")
	if count != 0 {
		t.Errorf("expected 0 unread after MarkAllRead, got %d", count)
	}
}

func TestManager(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir)

	team, err := mgr.CreateTeam("test-team", "/workspace")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}
	if team.Name() != "test-team" {
		t.Errorf("expected team name 'test-team', got %q", team.Name())
	}

	teams := mgr.ListTeams()
	if len(teams) != 1 {
		t.Errorf("expected 1 team, got %d", len(teams))
	}

	got, ok := mgr.GetTeam("test-team")
	if !ok {
		t.Fatal("should find test-team")
	}
	if got.Workspace() != "/workspace" {
		t.Errorf("expected workspace /workspace, got %q", got.Workspace())
	}

	tl, err := mgr.GetTaskList("test-team")
	if err != nil {
		t.Fatalf("GetTaskList failed: %v", err)
	}
	if tl.Len() != 0 {
		t.Errorf("expected 0 tasks in new team, got %d", tl.Len())
	}

	mbox, err := mgr.GetMailbox("test-team")
	if err != nil {
		t.Fatalf("GetMailbox failed: %v", err)
	}
	if mbox.UnreadCount("anyone") != 0 {
		t.Error("expected 0 unread messages in new mailbox")
	}

	err = mgr.DeleteTeam("test-team")
	if err != nil {
		t.Fatalf("DeleteTeam failed: %v", err)
	}
	teams = mgr.ListTeams()
	if len(teams) != 0 {
		t.Errorf("expected 0 teams after deletion, got %d", len(teams))
	}
}
