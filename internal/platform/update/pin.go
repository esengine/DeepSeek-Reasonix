package update

// Offer is what the shell should do about a release it just learned about.
// Separating "there is something newer" from "install it" is the whole point:
// a user who rolled back must still be told a fix exists without being moved
// onto it again.
type Offer struct {
	Available   bool   // a version other than the running one is published
	Newer       bool   // that version is ahead of what is running
	AutoInstall bool   // the shell may install it without being asked
	Pinned      bool   // the user chose to stay on the running build
	PinnedTo    string // the version they chose, when it is still the running one
	StalePin    bool   // pinned to something this build is not; the pin no longer describes reality
}

// OfferFor decides what a published version means for a running build: a pin
// suppresses installation, never discovery. It prevents the failure every
// rollback UI gets wrong — the user rolls back on Monday, the updater puts the
// broken build back on Tuesday, and they uninstall on Wednesday.
func OfferFor(current, latest, pinned string) Offer {
	offer := Offer{
		Available: latest != "" && !SameVersion(latest, current),
		Newer:     latest != "" && CompareVersions(latest, current) > 0,
	}
	switch {
	case pinned == "":
		offer.AutoInstall = offer.Newer
	case SameVersion(pinned, current):
		// Honoured: stay put, but keep reporting what exists.
		offer.Pinned = true
		offer.PinnedTo = pinned
	default:
		// The running build is not the pinned one — the user moved on by other
		// means (a manual install, a reinstall). A pin that describes a version
		// nobody is running must not keep suppressing updates forever.
		offer.StalePin = true
		offer.AutoInstall = offer.Newer
	}
	return offer
}
