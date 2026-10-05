package control

import (
	"path/filepath"
	"testing"

	"reasonix/internal/state/sessionstore"
)

// versionsOf lists the versions kept for the controller's session.
func versionsOf(t *testing.T, c *Controller) []sessionstore.SessionVersion {
	t.Helper()
	byParent, err := sessionstore.ListSessionVersions(filepath.Dir(c.SessionPath()))
	if err != nil {
		t.Fatal(err)
	}
	return byParent[sessionstore.BranchID(c.SessionPath())]
}

// A conversation rewind keeps what it cut as a version of the session: out of
// every session list, reachable from its parent, and whole.
func TestConversationRewindKeepsTheCutVersion(t *testing.T) {
	c, ag, _ := runTwoTurns(t)
	before := len(ag.Session().Snapshot())

	if err := c.Rewind(1, RewindConversation); err != nil {
		t.Fatal(err)
	}
	versions := versionsOf(t, c)
	if len(versions) != 1 {
		t.Fatalf("versions = %d, want the one the rewind cut", len(versions))
	}
	kept, err := sessionstore.LoadSession(versions[0].Path)
	if err != nil || len(kept.Snapshot()) != before {
		t.Fatalf("version holds %d messages (%v), want the %d before the cut", len(kept.Snapshot()), err, before)
	}
	listed, _ := sessionstore.ListSessions(filepath.Dir(c.SessionPath()))
	for _, s := range listed {
		if s.Path == versions[0].Path {
			t.Fatal("a version shows up in the session list as a conversation of its own")
		}
	}
}

// Undoing the rewind restores the cut conversation, so its version would show
// the same conversation twice and is dropped.
func TestUndoRewindDropsTheRedundantVersion(t *testing.T) {
	c, _, _ := runTwoTurns(t)
	plan, err := c.PrepareRewind(1, RewindConversation)
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.CommitRewind(plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versionsOf(t, c)) != 1 {
		t.Fatal("the rewind kept no version")
	}
	if _, err := c.UndoRewind(result.TransactionID); err != nil {
		t.Fatal(err)
	}
	if got := versionsOf(t, c); len(got) != 0 {
		t.Fatalf("versions after undo = %v, want none", got)
	}
}

// A code-only rewind cuts no conversation and keeps nothing.
func TestCodeRewindKeepsNoVersion(t *testing.T) {
	c, _, _ := runTwoTurns(t)
	plan, err := c.PrepareRewind(1, RewindCode)
	if err == nil {
		_, _ = c.CommitRewind(plan.PlanID)
	}
	if got := versionsOf(t, c); len(got) != 0 {
		t.Fatalf("versions after a code rewind = %v, want none", got)
	}
}
