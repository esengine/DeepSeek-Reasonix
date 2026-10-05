package sessionstore

import (
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

func twoTurnMessages() []provider.Message {
	return []provider.Message{
		{Role: provider.RoleUser, Content: "first"}, {Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleUser, Content: "second"}, {Role: provider.RoleAssistant, Content: "two"},
	}
}

// A version someone carries on becomes a conversation of its own, listed like
// any other; one nobody touched stays under its parent.
func TestVersionCarriedOnIsListedAgain(t *testing.T) {
	dir := testenv.TempDir(t)
	parent := NewSessionPath(dir, "test")
	path, err := SaveSupersededVersion(parent, twoTurnMessages())
	if err != nil {
		t.Fatal(err)
	}
	if listed, _ := ListSessions(dir); len(listed) != 0 {
		t.Fatalf("an untouched version is listed: %+v", listed)
	}
	if err := UpdateSessionMeta(path, "", "second", 2, false); err != nil {
		t.Fatal(err)
	}
	if listed, _ := ListSessions(dir); len(listed) != 0 {
		t.Fatal("a save that added no turn promoted the version")
	}
	if err := UpdateSessionMeta(path, "", "third", 3, true); err != nil {
		t.Fatal(err)
	}
	listed, _ := ListSessions(dir)
	if len(listed) != 1 || listed[0].Path != path || listed[0].ParentID != BranchID(parent) {
		t.Fatalf("carried-on version = %+v, want it listed with its parent kept", listed)
	}
}

// Undo drops only a version that holds exactly the restored conversation.
func TestDropVersionMatchingLeavesADifferentVersion(t *testing.T) {
	dir := testenv.TempDir(t)
	parent := NewSessionPath(dir, "test")
	if _, err := SaveSupersededVersion(parent, twoTurnMessages()); err != nil {
		t.Fatal(err)
	}
	if err := DropVersionMatching(parent, twoTurnMessages()[:2]); err != nil {
		t.Fatal(err)
	}
	if got := SessionVersionPaths(parent); len(got) != 1 {
		t.Fatalf("a non-matching restore dropped the version: %v", got)
	}
	if err := DropVersionMatching(parent, twoTurnMessages()); err != nil {
		t.Fatal(err)
	}
	if got := SessionVersionPaths(parent); len(got) != 0 {
		t.Fatalf("the matching version survived: %v", got)
	}
}
