package update

import "runtime"

// StudioLine is Studio's identity to the shared update path, beside
// StudioCatalog because they answer one question together: what this product
// line publishes, and what installing one of them means. It is the line rather
// than a shell, so both read the same one. Every system path here is Studio's
// own, or dpkg would see one package overwriting another's helper.
func StudioLine() Line {
	return Line{
		Members:   []ReleaseMember{{Archive: studioBinName(), Installed: studioBinName(), Mode: 0o700}},
		Launchers: []string{studioBinName()},
		Deb: DebLine{
			Package:      "reasonix-studio",
			HelperPath:   "/usr/lib/reasonix-studio/reasonix-studio-update-helper",
			PolkitAction: "io.reasonix.studio.update",
		},
		// Studio's releases are Developer ID signed and notarized, so a swapped
		// bundle is one Gatekeeper still opens. The identifier is what the swap
		// checks a downloaded bundle against before it replaces anything.
		Mac: MacLine{BundleID: "io.reasonix.studio", SelfUpdate: true},
		// The Electron installer requests nothing and raises consent itself for
		// an all-users install. Starting it elevated would put it in the
		// consenting account, where its per-user default writes that profile.
		Windows: WindowsLine{Installer: WindowsInstallerPerUser},
	}
}

// The single binary the Wails shell installs. The Electron shell reads neither
// Members nor Launchers -- every platform it ships takes a channel that
// replaces a whole install rather than staging files -- so this stays what it
// has always named rather than becoming a name that fits neither shell.
func studioBinName() string {
	if runtime.GOOS == "windows" {
		return "reasonix-studio.exe"
	}
	return "reasonix-studio"
}
