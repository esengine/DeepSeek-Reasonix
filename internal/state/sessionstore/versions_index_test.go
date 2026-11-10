package sessionstore

import (
	"reasonix/internal/base/testenv"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
)

// The sidebar asks for versions on every refresh. In a folder the listing has
// already told apart, finding the few versions must not open every session's
// sidecar again.
func TestFindingVersionsInAnIndexedFolderOpensOnlyWhatMightBeOne(t *testing.T) {
	dir := testenv.TempDir(t)
	writeSessions(t, dir, 300)
	parent := sessionPath(dir, 7)
	v1, err := SaveSupersededVersion(parent, []provider.Message{{Role: provider.RoleUser, Content: "first take"}})
	if err != nil {
		t.Fatal(err)
	}
	list(t, dir)
	reads := countSidecarReads(t)

	byParent, err := ListSessionVersions(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := byParent[BranchID(parent)]
	if len(got) != 1 || got[0].Path != v1 {
		t.Fatalf("versions of the parent = %+v, want just %s", got, v1)
	}
	if n := reads(); n > 5 {
		t.Fatalf("finding versions opened %d sidecars in a folder of 300, want only the version's", n)
	}
}

// A sidecar changed since the listing was remembered is read again: the stamps
// are what the shortcut trusts.
func TestAVersionMadeAfterTheListingIsStillFound(t *testing.T) {
	dir := testenv.TempDir(t)
	writeSessions(t, dir, 300)
	list(t, dir)
	parent := sessionPath(dir, 11)
	time.Sleep(10 * time.Millisecond)
	v, err := SaveSupersededVersion(parent, []provider.Message{{Role: provider.RoleUser, Content: "later take"}})
	if err != nil {
		t.Fatal(err)
	}
	byParent, err := ListSessionVersions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := byParent[BranchID(parent)]; len(got) != 1 || got[0].Path != v {
		t.Fatalf("versions = %+v, want the one saved after the listing", got)
	}
}
