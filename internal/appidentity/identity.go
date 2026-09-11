package appidentity

const (
	// AppUserModelID belongs to Desktop, independently of installed Studio versions.
	// Keep it stable across upgrades and aligned with the Electron shell.
	AppUserModelID = "io.reasonix.desktop"
	DisplayName    = "Reasonix"

	// Old Desktop and Wails Studio shared this ID; ownership must precede migration.
	legacyAppUserModelID      = "Reasonix"
	studioAppUserModelID      = "io.reasonix.studio"
	legacyTauriAppUserModelID = "dev.reasonix.desktop"
)
