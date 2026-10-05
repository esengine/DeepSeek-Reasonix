package control

import (
	"slices"
	"testing"
	"time"

	"reasonix/internal/safety/permission"
)

// A grant given on a prompt lives in memory and in nothing else, so a person
// reading the rules file is reading less than the agent may do. Listing them is
// half of it; the half that matters is that taking one back stops the call.
func TestASessionGrantIsListedAndCanBeTakenBack(t *testing.T) {
	a := newApprovalManager(permission.New("ask", nil, nil, nil), ToolApprovalAsk, time.Minute, true)
	a.grantSession("computer_act", "com.example.Notes")
	a.grantSession("browser_open", "https://example.com")

	listed := a.sessionGrants()
	want := []string{"Browser=https://example.com", "Computer=com.example.Notes"}
	if !slices.Equal(listed, want) {
		t.Fatalf("the session allows %v, want %v", listed, want)
	}
	if !a.preApprovedForRequiredHuman("computer_act", "com.example.Notes") {
		t.Fatal("the grant does not answer the call it was given for")
	}

	if n := a.revokeSessionGrant("Computer=com.example.Notes"); n != 1 {
		t.Fatalf("revoking one grant took back %d", n)
	}
	if a.preApprovedForRequiredHuman("computer_act", "com.example.Notes") {
		t.Fatal("the call is still answered after the grant was taken back")
	}
	if listed := a.sessionGrants(); !slices.Equal(listed, []string{"Browser=https://example.com"}) {
		t.Fatalf("after revoking one, the session allows %v", listed)
	}
	// A rule nobody holds is not an error and is not a revocation either.
	if n := a.revokeSessionGrant("Computer=com.example.Notes"); n != 0 {
		t.Fatalf("revoking what was already gone took back %d", n)
	}
	if n := a.revokeSessionGrant(""); n != 1 {
		t.Fatalf("revoking everything took back %d, want the one that was left", n)
	}
	if listed := a.sessionGrants(); len(listed) != 0 {
		t.Fatalf("after revoking everything the session still allows %v", listed)
	}
}

// What a rebuild carries forward is what the map holds, so a grant taken back
// does not come back with it.
func TestARevokedGrantIsNotCarriedThroughARebuild(t *testing.T) {
	a := newApprovalManager(permission.New("ask", nil, nil, nil), ToolApprovalAsk, time.Minute, true)
	a.grantSession("computer_act", "com.example.Notes")
	a.revokeSessionGrant("Computer=com.example.Notes")
	if auth := a.snapshotSessionAuthorizations(); len(auth.Grants) != 0 {
		t.Fatalf("a rebuild would carry %v", auth.Grants)
	}
}
