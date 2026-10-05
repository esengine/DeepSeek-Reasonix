// Command studio-manifest writes Studio's latest.json from a directory of built
// artifacts. cmd/sign's manifest subcommand is the desktop line's: it hardcodes
// desktop's download page and drops any windows file that is not -installer.exe.
//
// Downloads is what a person is offered, so it lists only what installs itself.
// Platforms and NativePackages are what the updater resolves, which is why a
// portable archive never appears in either.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/platform/update"
)

const artifactPrefix = "ReasonixStudio-"

func main() {
	if len(os.Args) != 4 && len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: studio-manifest <dir> <version> <tag> [delta-dir]")
		os.Exit(2)
	}
	deltaDir := ""
	if len(os.Args) == 5 {
		deltaDir = os.Args[4]
	}
	// HAS_R2 is the release workflow's own gate on uploading the mirror, so the
	// manifest names mirror addresses exactly when that upload will run.
	mirrored := os.Getenv("HAS_R2") == "true"
	if err := run(os.Args[1], os.Args[2], os.Args[3], deltaDir, mirrored); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(dir, version, tag, deltaDir string, mirrored bool) error {
	repo := os.Getenv("GITHUB_REPOSITORY")
	if strings.TrimSpace(repo) == "" {
		return fmt.Errorf("studio-manifest: GITHUB_REPOSITORY is unset, so asset URLs cannot be built")
	}
	page := fmt.Sprintf("https://github.com/%s/releases/tag/%s", repo, tag)
	m := update.Manifest{
		Version:         version,
		DownloadPage:    page,
		ReleaseNotesURL: page,
		Platforms:       map[string]update.Asset{},
		NativePackages:  map[string]update.Asset{},
		Downloads:       map[string]update.Asset{},
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	seen := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, artifactPrefix) || strings.HasSuffix(name, ".minisig") {
			continue
		}
		seen++
		size, sum, err := hashFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		asset := releaseAsset(repo, tag, name, mirrored)
		asset.Size, asset.SHA256 = size, sum
		// Downloads is what a person is offered, so it lists what installs
		// itself. A portable archive stays a release asset, but offering it
		// beside the installer only asks the reader to choose blind.
		if installable(name) {
			m.Downloads[name] = asset
			fmt.Printf("download: %s (%d bytes)\n", name, size)
		}
		// A .deb installs through dpkg, which carries a binary and the SPA tree
		// alike, so Linux updates through its package.
		if key, ok := nativePackageKey(name); ok {
			m.NativePackages[key] = asset
			fmt.Printf("native package: %s -> %s\n", name, key)
		}
		// Windows updates by running the next installer, so that is what the
		// platform lookup has to resolve. The portable archive is never listed:
		// resolving it would hand the updater an artifact it cannot install.
		if key, ok := windowsInstallerKey(name); ok {
			m.Platforms[key] = asset
			fmt.Printf("platform: %s -> %s\n", name, key)
		}
		// macOS swaps the bundle out of a zip. One universal archive serves both
		// architectures, so it answers to both keys rather than asking the
		// release to carry two identical downloads.
		for _, key := range macArchiveKeys(name) {
			m.Platforms[key] = asset
			fmt.Printf("platform: %s -> %s\n", name, key)
		}
	}
	if seen == 0 {
		return fmt.Errorf("studio-manifest: no %s* artifacts in %s", artifactPrefix, dir)
	}
	if deltaDir != "" {
		if m.Deltas, err = deltas(deltaDir, tag); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "latest.json"), append(b, '\n'), 0o644)
}

// releaseAsset addresses one artifact. The mirror serves it first when there is
// one: GitHub's release CDN is what a CN route reaches slowest, and it stays as
// the fallback for a mirror that is down.
func releaseAsset(repo, tag, name string, mirrored bool) update.Asset {
	github := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, name)
	if !mirrored {
		return update.Asset{URL: github, Sig: github + ".minisig"}
	}
	mirror := fmt.Sprintf("%s/%s/%s", update.StudioMirror, tag, name)
	return update.Asset{
		URL: mirror, Sig: mirror + ".minisig",
		Fallback: github, FallbackSig: github + ".minisig",
	}
}

// installable reports whether an artifact installs itself rather than expecting
// the reader to place it: a disk image to drag from, an installer to run, a
// package for dpkg.
func installable(name string) bool {
	return strings.HasSuffix(name, ".dmg") ||
		strings.HasSuffix(name, "-installer.exe") ||
		strings.HasSuffix(name, ".deb")
}

// macArchiveKeys maps a macOS archive to the platforms it can install. The
// bundle swap extracts a zip, so the .dmg is a human download only; a universal
// archive answers for both architectures because the slices ride in one file.
func macArchiveKeys(name string) []string {
	base, ok := strings.CutSuffix(name, ".zip")
	if !ok {
		return nil
	}
	arch, ok := strings.CutPrefix(base, artifactPrefix+"darwin-")
	if !ok || arch == "" {
		return nil
	}
	if arch == "universal" {
		return []string{update.PlatformKey("darwin", "amd64"), update.PlatformKey("darwin", "arm64")}
	}
	return []string{update.PlatformKey("darwin", arch)}
}

// windowsInstallerKey maps an installer artifact to the platform it upgrades.
// Artifacts are named ReasonixStudio-windows-<arch>-installer.exe.
func windowsInstallerKey(name string) (string, bool) {
	base, ok := strings.CutSuffix(name, "-installer.exe")
	if !ok {
		return "", false
	}
	arch, ok := strings.CutPrefix(base, artifactPrefix+"windows-")
	if !ok || arch == "" {
		return "", false
	}
	return update.PlatformKey("windows", arch), true
}

// nativePackageKey maps a .deb artifact name to the platform whose dpkg install
// it upgrades. Artifacts are named ReasonixStudio-linux-<arch>.deb, and only a
// package channel belongs here — a tarball is a download, not an install.
func nativePackageKey(name string) (string, bool) {
	base, ok := strings.CutSuffix(name, ".deb")
	if !ok {
		return "", false
	}
	arch, ok := strings.CutPrefix(base, artifactPrefix+"linux-")
	if !ok || arch == "" {
		return "", false
	}
	return update.PlatformKey("linux", arch), true
}

func hashFile(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(h.Sum(nil)), nil
}
