package update

import "testing"

func TestOfferForUnpinnedFollowsTheChannel(t *testing.T) {
	got := OfferFor("2.0.0", "2.1.0", "")
	if !got.Available || !got.Newer || !got.AutoInstall {
		t.Fatalf("offer = %+v, want an installable newer release", got)
	}
	if got.Pinned {
		t.Error("nothing was pinned")
	}
}

// The failure every rollback UI gets wrong: the user leaves a broken build on
// Monday and the updater puts them back on it on Tuesday.
func TestOfferForPinnedNeverInstalls(t *testing.T) {
	got := OfferFor("1.25.1", "2.0.0", "1.25.1")
	if got.AutoInstall {
		t.Error("a pinned build must not be updated out from under the user")
	}
	if !got.Available || !got.Newer {
		t.Error("a pin suppresses installing, not knowing: the fix must still be reported")
	}
	if !got.Pinned || got.PinnedTo != "1.25.1" {
		t.Errorf("offer = %+v, want the pin surfaced so the user can release it", got)
	}
}

// A pin naming a version nobody is running has stopped describing reality, and
// must not suppress updates forever.
func TestOfferForStalePinResumesUpdates(t *testing.T) {
	got := OfferFor("2.0.0", "2.1.0", "1.25.1")
	if !got.StalePin {
		t.Error("pinned to 1.25.1 while running 2.0.0 is a stale pin")
	}
	if !got.AutoInstall {
		t.Error("a stale pin must not keep the user off updates")
	}
	if got.Pinned {
		t.Error("a stale pin is not an honoured pin")
	}
}

func TestOfferForIgnoresTheLeadingV(t *testing.T) {
	got := OfferFor("v2.0.0", "2.0.0", "")
	if got.Available || got.Newer || got.AutoInstall {
		t.Fatalf("offer = %+v, want nothing to do when the versions match", got)
	}
	if pinned := OfferFor("2.0.0", "2.1.0", "v2.0.0"); !pinned.Pinned {
		t.Errorf("offer = %+v, want the pin honoured despite the leading v", pinned)
	}
}

// Rolling back leaves the published latest ahead of the running build. That is
// the state the pin exists for, and it must not read as "nothing available".
func TestOfferAfterARollback(t *testing.T) {
	got := OfferFor("1.25.1", "2.0.0", "1.25.1")
	if !got.Available {
		t.Error("the version they left must stay visible so they can go forward again")
	}
	if got.AutoInstall {
		t.Error("going forward again is the user's decision, not the updater's")
	}
}

func TestOfferForNoPublishedVersion(t *testing.T) {
	got := OfferFor("2.0.0", "", "")
	if got.Available || got.Newer || got.AutoInstall {
		t.Fatalf("offer = %+v, want nothing offered when the catalog is unknown", got)
	}
}
