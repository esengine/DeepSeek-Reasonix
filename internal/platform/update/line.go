package update

import "os"

// Line is the product line an install belongs to. The desktop and Studio ship
// different binaries into different paths and answer to different package
// names, so each host declares its own identity here rather than the shared
// apply path naming either. A zero Line installs nothing: a caller that has not
// said which line it is must not fall through to some other line's layout.
type Line struct {
	// Members are the files a release archive must carry, in staging order.
	Members []ReleaseMember
	// Launchers are the entry points that survive the running binary being
	// replaced, most specific first. Empty relaunches the executable itself,
	// which is correct for a portable copy that was never installed.
	Launchers []string
	// Deb names this line to dpkg and to the privileged update path.
	Deb DebLine
	// Mac is what the bundle swap needs to recognize and trust a replacement.
	Mac MacLine
	// Windows is what starting this line's downloaded installer needs.
	Windows WindowsLine
}

// WindowsLine is what starting this line's installer needs to know.
type WindowsLine struct {
	// Installer is how this line's installer must be started. "" is a line that
	// has not said, and is refused rather than guessed.
	Installer WindowsInstaller
}

// WindowsInstaller names the shape of a line's Windows installer, because the
// two ways of starting one are not interchangeable in either direction:
// CreateProcess refuses an image whose manifest requests admin, and elevating
// one that does not request it installs into whichever account consented rather
// than the one that asked for the update.
type WindowsInstaller string

const (
	// WindowsInstallerElevated requests admin in its manifest — a per-machine
	// install under Program Files and HKLM. Only ShellExecute can start it.
	WindowsInstallerElevated WindowsInstaller = "elevated"
	// WindowsInstallerPerUser requests nothing in its manifest and raises
	// consent itself if it needs to. Starting it elevated puts it in the
	// consenting account, where a per-user default writes that profile and
	// leaves the user who asked for the update on the build they had.
	WindowsInstallerPerUser WindowsInstaller = "per-user"
)

// MacLine identifies this line's app bundle. SelfUpdate is false for a line
// whose releases are not Developer ID signed and notarized: writing a bundle
// Gatekeeper will refuse is worse than sending the user to the download page.
type MacLine struct {
	BundleID   string
	SelfUpdate bool
}

// ReleaseMember is one file in a release: what the archive calls it and what it
// installs as. The two differ because an archive is platform-neutral and the
// installed name is not. An empty Installed means the archive must carry the
// file but this version does not persist it — how the desktop keeps shipping
// reasonix-guard for updaters that still read these archives.
type ReleaseMember struct {
	Archive   string
	Installed string
	Mode      os.FileMode
}

// DebLine is what the Linux package path needs to recognize this line's install
// and ask Polkit for the one privileged action that may replace it. Two lines
// installed side by side share no file here, or dpkg would see one package
// overwriting the other's helper.
type DebLine struct {
	Package      string
	HelperPath   string
	PolkitAction string
}

// ArchiveNames returns the member names an archive must carry, which is what
// the extractor checks against.
func (l Line) ArchiveNames() []string {
	out := make([]string, 0, len(l.Members))
	for _, m := range l.Members {
		out = append(out, m.Archive)
	}
	return out
}

// InstalledNames returns the names members take inside a version directory,
// which is the exact allow-list an activation is held to. Members the archive
// only has to carry are not among them.
func (l Line) InstalledNames() []string {
	out := make([]string, 0, len(l.Members))
	for _, m := range l.Members {
		if m.Installed != "" {
			out = append(out, m.Installed)
		}
	}
	return out
}
