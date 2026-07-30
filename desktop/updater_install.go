package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/desktop/internal/update"
)

// Install modes reported to the frontend and used to pick the update asset.
const (
	installModePortable = "portable"
	installModeDeb      = "deb"
	installModeManual   = "manual"

	artifactKindTarball = "tarball"
	artifactKindDeb     = "deb"

	linuxUpdateHelperPath = "/usr/lib/reasonix/reasonix-update-helper"
	linuxPkexecPath       = "/usr/bin/pkexec"
	linuxDebPackageName   = "reasonix-desktop"
)

// installProfile is the runtime install classification used by evaluate and install.
type installProfile struct {
	Mode          string
	CanSelfUpdate bool
	RequiresElev  bool
	ManualReason  string
	ArtifactKind  string // artifactKindTarball | artifactKindDeb | ""
}

// detectInstallProfile classifies how this binary was installed and which update
// path is available. profileForManifest then may downgrade deb → manual when the
// manifest has no native package entry.
func detectInstallProfile() installProfile {
	switch runtime.GOOS {
	case "linux":
		return detectLinuxInstallProfile()
	case "windows":
		return installProfile{
			Mode:          installModePortable,
			CanSelfUpdate: true,
			ArtifactKind:  artifactKindTarball,
		}
	case "darwin":
		if canSelfUpdate() {
			return installProfile{
				Mode:          installModePortable,
				CanSelfUpdate: true,
				ArtifactKind:  artifactKindTarball,
			}
		}
		return installProfile{
			Mode:          installModeManual,
			CanSelfUpdate: false,
			ManualReason:  manualUpdateReason(),
		}
	default:
		return installProfile{
			Mode:          installModeManual,
			CanSelfUpdate: false,
			ManualReason:  fmt.Sprintf("self-update unsupported on %s", runtime.GOOS),
		}
	}
}

// profileForManifest adjusts a detected profile against the published assets:
// deb installs need native_packages; portable needs platforms.
func profileForManifest(base installProfile, m *update.Manifest) installProfile {
	if m == nil {
		return base
	}
	switch base.Mode {
	case installModeDeb:
		if _, ok := m.NativePackage(); !ok {
			base.Mode = installModeManual
			base.CanSelfUpdate = false
			base.RequiresElev = false
			base.ArtifactKind = ""
			base.ManualReason = "this update does not include a Debian package; install manually from the download page"
			return base
		}
		if !linuxDebHelperReady() {
			base.Mode = installModeManual
			base.CanSelfUpdate = false
			base.RequiresElev = false
			base.ArtifactKind = ""
			base.ManualReason = "system update helper is unavailable; install with: sudo apt install ./Reasonix-linux-amd64.deb"
			return base
		}
		base.ArtifactKind = artifactKindDeb
		base.CanSelfUpdate = true
		base.RequiresElev = true
		return base
	case installModePortable:
		if _, ok := m.Asset(); !ok {
			base.Mode = installModeManual
			base.CanSelfUpdate = false
			base.ArtifactKind = ""
			base.ManualReason = "no update artifact is published for this platform"
			return base
		}
		base.ArtifactKind = artifactKindTarball
		return base
	default:
		return base
	}
}

// selectUpdateAsset returns the downloadable asset for the active install profile.
func selectUpdateAsset(m *update.Manifest, profile installProfile) (update.Asset, string, bool) {
	switch profile.Mode {
	case installModeDeb:
		if a, ok := m.NativePackage(); ok {
			return a, artifactKindDeb, true
		}
	case installModePortable:
		if a, ok := m.Asset(); ok {
			return a, artifactKindTarball, true
		}
	}
	return update.Asset{}, "", false
}

// dirIsWritable reports whether the process can create a temporary file in dir.
func dirIsWritable(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	f, err := os.CreateTemp(dir, ".reasonix-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func linuxDebHelperReady() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if _, err := os.Stat(linuxUpdateHelperPath); err != nil {
		return false
	}
	if _, err := os.Stat(linuxPkexecPath); err != nil {
		return false
	}
	return true
}

// resolveExecutablePath returns the real path of the running binary.
func resolveExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe
}

func manualDebInstallHint() string {
	return "Install manually with: sudo apt install ./Reasonix-linux-amd64.deb"
}

func artifactKindFromMeta(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case artifactKindDeb:
		return artifactKindDeb
	case artifactKindTarball, "":
		// Empty means legacy portable cache written before artifactKind existed.
		return artifactKindTarball
	default:
		return kind
	}
}
