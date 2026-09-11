package agentview

import (
	"testing"
)

func TestManager(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir)

	info, err := mgr.Register("session-1", "Test Session", "/workspace", "deepseek-r1")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if info.ID != "session-1" {
		t.Errorf("expected ID 'session-1', got %q", info.ID)
	}
	if info.State != StateWorking {
		t.Errorf("expected state working, got %s", info.State)
	}

	got, ok := mgr.Get("session-1")
	if !ok {
		t.Fatal("should find session-1")
	}
	if got.Name != "Test Session" {
		t.Errorf("expected name 'Test Session', got %q", got.Name)
	}

	sessions := mgr.List()
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}

	mgr.UpdateState("session-1", StateCompleted)
	got, _ = mgr.Get("session-1")
	if got.State != StateCompleted {
		t.Errorf("expected state completed, got %s", got.State)
	}
	if got.Running {
		t.Error("completed session should not be running")
	}

	mgr.UpdateSummary("session-1", "All done!")
	got, _ = mgr.Get("session-1")
	if got.Summary != "All done!" {
		t.Errorf("expected summary 'All done!', got %q", got.Summary)
	}

	mgr.Pin("session-1")
	got, _ = mgr.Get("session-1")
	if !got.Pinned {
		t.Error("session should be pinned")
	}

	mgr.Unpin("session-1")
	got, _ = mgr.Get("session-1")
	if got.Pinned {
		t.Error("session should not be pinned after unpin")
	}

	mgr.Rename("session-1", "New Name")
	got, _ = mgr.Get("session-1")
	if got.Name != "New Name" {
		t.Errorf("expected name 'New Name', got %q", got.Name)
	}

	mgr.AddPullRequest("session-1", "PR #123")
	got, _ = mgr.Get("session-1")
	if len(got.PullRequests) != 1 {
		t.Errorf("expected 1 PR, got %d", len(got.PullRequests))
	}

	completed := mgr.ByState(StateCompleted)
	if len(completed) != 1 {
		t.Errorf("expected 1 completed session, got %d", len(completed))
	}

	if mgr.NeedsInputCount() != 0 {
		t.Errorf("expected 0 needs_input count, got %d", mgr.NeedsInputCount())
	}

	byWorkspace := mgr.ListByWorkspace("/workspace")
	if len(byWorkspace) != 1 {
		t.Errorf("expected 1 session in workspace, got %d", len(byWorkspace))
	}

	err = mgr.Remove("session-1")
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if mgr.CountByState(StateCompleted) != 0 {
		t.Errorf("expected 0 completed after remove, got %d", mgr.CountByState(StateCompleted))
	}
}

func TestManager_Load(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir)

	_, err := mgr.Register("sess-1", "First", "/workspace", "model-a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.Register("sess-2", "Second", "/other", "model-b")
	if err != nil {
		t.Fatal(err)
	}
	mgr.UpdateState("sess-1", StateNeedsInput)

	mgr2 := NewManager(tmpDir)
	err = mgr2.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	sessions := mgr2.List()
	if len(sessions) != 2 {
		t.Errorf("expected 2 loaded sessions, got %d", len(sessions))
	}

	if mgr2.NeedsInputCount() != 1 {
		t.Errorf("expected 1 needs_input after load, got %d", mgr2.NeedsInputCount())
	}
}

func TestStatePriority(t *testing.T) {
	if statePriority(StateNeedsInput) != 0 {
		t.Errorf("needs_input should be priority 0, got %d", statePriority(StateNeedsInput))
	}
	if statePriority(StateWorking) != 1 {
		t.Errorf("working should be priority 1, got %d", statePriority(StateWorking))
	}
	if statePriority(StateCompleted) != 4 {
		t.Errorf("completed should be priority 4, got %d", statePriority(StateCompleted))
	}
}
