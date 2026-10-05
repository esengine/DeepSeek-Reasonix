package appidentity

const (
	// AppUserModelID is shared by every current-generation Windows process,
	// shortcut, and toast that users perceive as Reasonix. Keep it
	// version-independent across upgrades.
	AppUserModelID = "Reasonix"

	// legacyTauriAppUserModelID was written to Windows shortcuts by Reasonix
	// Desktop 0.53. Keep the current identity distinct so a separately
	// installed older generation does not merge into one taskbar group.
	legacyTauriAppUserModelID = "dev.reasonix.desktop"
)
