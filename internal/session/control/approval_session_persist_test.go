package control

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/sessionstore"
)

func conversationAt(t *testing.T, dir string) (*Controller, string) {
	t.Helper()
	path := filepath.Join(dir, "20261023-100000.000000000-test.jsonl")
	c := New(Options{WorkspaceRoot: testenv.TempDir(t)})
	t.Cleanup(c.Close)
	c.SetSessionPath(path)
	return c, path
}

// What a person allowed "for this session" is theirs for that conversation, so
// resuming it later must not ask the same question again.
func TestSessionGrantsAreRememberedWhenTheConversationIsResumed(t *testing.T) {
	dir := testenv.TempDir(t)
	first, path := conversationAt(t, dir)
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first.approval.grantSession("bash", "go test ./...")
	first.approval.grantSession(NetworkEgressApprovalTool, "example.com")
	first.persistSessionGrants()

	second := New(Options{WorkspaceRoot: testenv.TempDir(t)})
	t.Cleanup(second.Close)
	second.SetSessionPath(path)
	got := second.SessionAuthorizations().Grants
	for _, want := range []string{"Bash(go test:*)"} {
		if !slices.Contains(got, want) {
			t.Fatalf("resumed grants = %v, want %q restored", got, want)
		}
	}
	if len(got) != 2 {
		t.Fatalf("resumed grants = %v, want both answers restored", got)
	}
}

func TestARevokedSessionGrantStaysRevokedAfterResume(t *testing.T) {
	dir := testenv.TempDir(t)
	first, path := conversationAt(t, dir)
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first.approval.grantSession("bash", "go test ./...")
	first.persistSessionGrants()
	first.approval.revokeSessionGrant("")
	first.persistSessionGrants()

	second := New(Options{WorkspaceRoot: testenv.TempDir(t)})
	t.Cleanup(second.Close)
	second.SetSessionPath(path)
	if got := second.SessionAuthorizations().Grants; len(got) != 0 {
		t.Fatalf("resumed grants = %v, want none after the revocation", got)
	}
}

func TestAConversationWithoutAFileDoesNotCreateOne(t *testing.T) {
	dir := testenv.TempDir(t)
	c, path := conversationAt(t, dir)
	c.approval.grantSession("bash", "go test ./...")
	c.persistSessionGrants()
	if _, err := os.Stat(path + ".meta"); err == nil {
		t.Fatal("remembering a grant created a sidecar for a conversation that was never saved")
	}
}

// A sidecar is only a file: a grant it holds that the host could not have
// recorded is dropped when the conversation is resumed, not honoured.
func TestAForgedSessionGrantInTheSidecarIsNotRestored(t *testing.T) {
	dir := testenv.TempDir(t)
	first, path := conversationAt(t, dir)
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sessionstore.SaveSessionGrants(path, []string{"Bash(*)", "Bash(rm:*)", "Bash(go test:*)"}); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second := New(Options{WorkspaceRoot: testenv.TempDir(t)})
	t.Cleanup(second.Close)
	second.SetSessionPath(path)
	got := second.SessionAuthorizations().Grants
	if len(got) != 1 || got[0] != "Bash(go test:*)" {
		t.Fatalf("restored grants = %v, want only the one the host could have recorded", got)
	}
}

// Each change persists from its own goroutine, so an earlier one must not land
// after a later one and bring a revoked answer back.
func TestARevokeIsNotUndoneByAnEarlierPersistFinishingLate(t *testing.T) {
	for i := range 60 {
		first, path := conversationAt(t, testenv.TempDir(t))
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var persists sync.WaitGroup
		first.approval.onGrantChange = func() {
			persists.Go(first.persistSessionGrants)
		}
		for j := range 8 {
			first.approval.grantSession("bash", fmt.Sprintf("go test ./p%d", j))
		}
		first.approval.revokeSessionGrant("")
		persists.Wait()
		if got := sessionstore.LoadSessionGrants(path); len(got) != 0 {
			t.Fatalf("round %d: sidecar holds %v after every grant was revoked", i, got)
		}
	}
}
