// Package update defines the desktop auto-updater's shared types and signature
// verification — the single source of truth for the latest.json manifest format,
// the platform-asset lookup, and minisign verification. Both the CI signing tool
// (desktop/cmd/sign) and the running updater (desktop/updater.go) import this
// package, so the sign path and the verify path can never drift apart.
package update

import "runtime"

// Manifest is the latest.json published alongside a desktop release. The updater
// fetches it from the R2 mirror (primary) or GitHub releases (fallback), compares
// Version against the running build, and looks up the running platform's artifact
// via Asset / NativePackage.
type Manifest struct {
	Version         string           `json:"version"`                     // release version, e.g. "v1.1.0"
	Notes           string           `json:"notes"`                       // markdown release notes
	PubDate         string           `json:"pub_date"`                    // RFC3339, optional
	DownloadPage    string           `json:"download_page"`               // human-facing download page (macOS manual-update fallback)
	ReleaseNotesURL string           `json:"release_notes_url,omitempty"` // exact version history page; older manifests omit it
	Platforms       map[string]Asset `json:"platforms"`                   // keyed by PlatformKey, e.g. "darwin-arm64"
	NativePackages  map[string]Asset `json:"native_packages,omitempty"`   // optional OS package assets, e.g. linux-amd64 → .deb
	Downloads       map[string]Asset `json:"downloads,omitempty"`         // optional signed human-download assets keyed by exact filename
	Deltas          map[string]Delta `json:"deltas,omitempty"`            // optional chunked update per PlatformKey; see package delta
}

// Delta is one platform's chunked update: the signed, zstd-compressed index of
// the release tree, and the store its chunks are fetched from. A client that
// cannot use it falls back to the platform's full artifact.
type Delta struct {
	Index  Asset  `json:"index"`
	Chunks string `json:"chunks"` // base URL; a chunk is Chunks + "/" + delta.ObjectName(hash)
}

// StudioMirror is the release mirror Studio's catalog and deltas live on.
const StudioMirror = "https://dl.reasonix.io"

// StudioChunks is the one chunk store every Studio release shares.
const StudioChunks = StudioMirror + "/studio/chunks"

// DeltaIndexName is a platform's index file inside a release's delta directory.
const DeltaIndexName = "index.json.zst"

// Asset is one platform's downloadable artifact plus the metadata the updater
// needs to verify and report on it.
type Asset struct {
	URL    string `json:"url"`    // direct download URL for the artifact
	Sig    string `json:"sig"`    // URL of the detached minisign (.minisig) signature
	Size   int64  `json:"size"`   // artifact size in bytes (download-progress denominator)
	SHA256 string `json:"sha256"` // lowercase hex digest, for a second integrity check after verify
	// InstallLayout identifies the on-disk layout the artifact expects after
	// install. Empty means the pre-v1.20 flat layout (ignored by old clients).
	// New clients require "versioned-v1" for self-update and reject unknown
	// values without changing the active install.
	InstallLayout string `json:"install_layout,omitempty"`
	// Fallback and FallbackSig publish the same bytes at a second address,
	// tried once the first gives up. Clients that predate them read only URL.
	Fallback    string `json:"fallback,omitempty"`
	FallbackSig string `json:"fallback_sig,omitempty"`
}

// Sources is every address the artifact is published at, in the order to try.
func (a Asset) Sources() []string { return sources(a.URL, a.Fallback) }

// SigSources is Sources for the detached signature.
func (a Asset) SigSources() []string { return sources(a.Sig, a.FallbackSig) }

func sources(primary, fallback string) []string {
	if fallback == "" || fallback == primary {
		return []string{primary}
	}
	return []string{primary, fallback}
}

// PlatformKey is the map key used in Manifest.Platforms for the given OS/arch.
// The updater calls CurrentPlatform; the manifest generator builds keys the same
// way, so lookups always agree.
func PlatformKey(goos, goarch string) string { return goos + "-" + goarch }

// CurrentPlatform is PlatformKey for the running binary.
func CurrentPlatform() string { return PlatformKey(runtime.GOOS, runtime.GOARCH) }

// Asset returns the portable/tarball artifact for the running platform, if listed.
func (m Manifest) Asset() (Asset, bool) {
	a, ok := m.Platforms[CurrentPlatform()]
	return a, ok
}

// NativePackage returns the OS package artifact for the running platform, if listed.
// Older manifests omit native_packages; callers must treat absence as "no package".
func (m Manifest) NativePackage() (Asset, bool) {
	if m.NativePackages == nil {
		return Asset{}, false
	}
	a, ok := m.NativePackages[CurrentPlatform()]
	return a, ok
}
