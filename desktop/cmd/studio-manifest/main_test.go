package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/platform/update"
)

func TestManifestRecordsEveryArtifactWithItsSignature(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITHUB_REPOSITORY", "esengine/DeepSeek-Reasonix")
	body := []byte("studio archive bytes")
	for _, name := range []string{
		"ReasonixStudio-windows-amd64-installer.exe",
		"ReasonixStudio-darwin-universal.dmg",
		"ReasonixStudio-linux-amd64.deb",
		"ReasonixStudio-windows-amd64.zip",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".minisig"), []byte("sig"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := run(dir, "v0.1.0", "studio-v0.1.0", "", false); err != nil {
		t.Fatalf("run: %v", err)
	}

	var m update.Manifest
	raw, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("the manifest must parse as the type the updater reads: %v", err)
	}
	// The portable archive is a release asset but not an offer: downloads names
	// what installs itself, so a reader is not asked to choose between an
	// installer and a zip with no way to tell which one to take.
	if len(m.Downloads) != 3 {
		t.Fatalf("downloads = %d, want the installer, the disk image and the package", len(m.Downloads))
	}
	for name := range m.Downloads {
		if strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar.gz") {
			t.Errorf("downloads offers %q, which the reader has to place themselves", name)
		}
	}
	sum := sha256.Sum256(body)
	for name, a := range m.Downloads {
		if a.Sig != a.URL+".minisig" {
			t.Errorf("%s: sig %q does not point at the artifact's signature", name, a.Sig)
		}
		if a.Size != int64(len(body)) {
			t.Errorf("%s: size %d, want %d", name, a.Size, len(body))
		}
		if a.SHA256 != hex.EncodeToString(sum[:]) {
			t.Errorf("%s: sha256 %q does not match the bytes on disk", name, a.SHA256)
		}
	}
}

// An archive is not an install: the updater would have to place it, and nothing
// on the Studio side does. Only what installs itself may resolve as a platform
// asset, or the version panel offers a move it cannot make.
func TestManifestResolvesOnlyWhatCanInstallItself(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITHUB_REPOSITORY", "esengine/DeepSeek-Reasonix")
	for _, name := range []string{
		"ReasonixStudio-linux-amd64.tar.gz",
		"ReasonixStudio-windows-amd64.zip",
		"ReasonixStudio-windows-amd64-installer.exe",
		"ReasonixStudio-linux-amd64.deb",
		"ReasonixStudio-darwin-universal.zip",
		"ReasonixStudio-darwin-universal.dmg",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := run(dir, "v0.1.0", "studio-v0.1.0", "", false); err != nil {
		t.Fatalf("run: %v", err)
	}
	var m update.Manifest
	raw, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	// Windows updates by running the next installer.
	if got, ok := m.Platforms[update.PlatformKey("windows", "amd64")]; !ok ||
		!strings.HasSuffix(got.URL, "-installer.exe") {
		t.Errorf("windows platform asset = %+v, want the installer", got)
	}
	// One universal archive answers for both macOS architectures; the .dmg is a
	// human download, and handing it to the bundle swap would give it an image
	// to extract rather than an app.
	for _, arch := range []string{"amd64", "arm64"} {
		got, ok := m.Platforms[update.PlatformKey("darwin", arch)]
		if !ok || !strings.HasSuffix(got.URL, "-universal.zip") {
			t.Errorf("darwin-%s platform asset = %+v, want the universal archive", arch, got)
		}
	}
	if len(m.Platforms) != 3 {
		t.Errorf("platforms = %v, want the installer and both macOS keys", m.Platforms)
	}
	// Linux updates through dpkg: a package channel, not a platform asset,
	// because writing a .deb's files behind apt leaves the two disagreeing.
	if _, ok := m.NativePackages[update.PlatformKey("linux", "amd64")]; !ok {
		t.Errorf("native packages = %v, want the .deb", m.NativePackages)
	}
	if _, ok := m.Platforms[update.PlatformKey("linux", "amd64")]; ok {
		t.Error("the .deb must not also resolve as a platform asset")
	}
	if m.DownloadPage == "" {
		t.Error("a platform with no installable asset has only the download page, and it is empty")
	}
}

func TestEmptyDirIsAFailedRelease(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "esengine/DeepSeek-Reasonix")
	if err := run(t.TempDir(), "v0.1.0", "studio-v0.1.0", "", false); err == nil {
		t.Fatal("a release with no artifacts must fail, not publish an empty manifest")
	}
}

// A mirrored release is fetched from the mirror first, with GitHub kept as the
// address a client falls back to; every full artifact and its signature alike.
func TestMirroredManifestPutsTheMirrorFirst(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITHUB_REPOSITORY", "esengine/DeepSeek-Reasonix")
	for _, name := range []string{
		"ReasonixStudio-windows-amd64-installer.exe",
		"ReasonixStudio-darwin-universal.zip",
		"ReasonixStudio-linux-amd64.deb",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := run(dir, "v0.1.0", "studio-v0.1.0", "", true); err != nil {
		t.Fatalf("run: %v", err)
	}
	var m update.Manifest
	raw, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	var all []update.Asset
	for _, group := range []map[string]update.Asset{m.Platforms, m.Downloads, m.NativePackages} {
		for _, a := range group {
			all = append(all, a)
		}
	}
	if len(all) != 6 {
		t.Fatalf("assets = %d, want 3 platforms, 2 downloads and 1 package", len(all))
	}
	for _, a := range all {
		name := a.URL[strings.LastIndex(a.URL, "/")+1:]
		mirror := update.StudioMirror + "/studio-v0.1.0/" + name
		github := "https://github.com/esengine/DeepSeek-Reasonix/releases/download/studio-v0.1.0/" + name
		if a.URL != mirror || a.Sig != mirror+".minisig" {
			t.Errorf("%s: primary = %q / %q, want the mirror", name, a.URL, a.Sig)
		}
		if a.Fallback != github || a.FallbackSig != github+".minisig" {
			t.Errorf("%s: fallback = %q / %q, want the GitHub release", name, a.Fallback, a.FallbackSig)
		}
	}
	if !strings.HasPrefix(m.DownloadPage, "https://github.com/") {
		t.Errorf("download page = %q, want the GitHub release page", m.DownloadPage)
	}
}

func TestUnmirroredManifestNamesOnlyGitHub(t *testing.T) {
	a := releaseAsset("o/r", "studio-v1", "ReasonixStudio-linux-amd64.deb", false)
	if !strings.HasPrefix(a.URL, "https://github.com/o/r/") || a.Fallback != "" || a.FallbackSig != "" {
		t.Fatalf("asset = %+v, want GitHub alone when nothing is mirrored", a)
	}
}

// Both Windows architectures ship an installer and a portable archive under
// the same naming rule; each installer answers only its own platform key.
func TestWindowsArchitecturesResolveToTheirOwnInstallers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITHUB_REPOSITORY", "esengine/DeepSeek-Reasonix")
	for _, name := range []string{
		"ReasonixStudio-windows-amd64-installer.exe",
		"ReasonixStudio-windows-amd64.zip",
		"ReasonixStudio-windows-arm64-installer.exe",
		"ReasonixStudio-windows-arm64.zip",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := run(dir, "v0.1.0", "studio-v0.1.0", "", false); err != nil {
		t.Fatalf("run: %v", err)
	}
	var m update.Manifest
	raw, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Platforms) != 2 || len(m.Downloads) != 2 {
		t.Fatalf("platforms %d, downloads %d; want one installer per architecture", len(m.Platforms), len(m.Downloads))
	}
	for arch, name := range map[string]string{
		"amd64": "ReasonixStudio-windows-amd64-installer.exe",
		"arm64": "ReasonixStudio-windows-arm64-installer.exe",
	} {
		a, ok := m.Platforms[update.PlatformKey("windows", arch)]
		if !ok || !strings.HasSuffix(a.URL, "/"+name) {
			t.Errorf("windows-%s resolves to %+v, want %s", arch, a, name)
		}
	}
	if len(m.Deltas) != 0 {
		t.Errorf("no delta directory was given, yet deltas = %v", m.Deltas)
	}
}
