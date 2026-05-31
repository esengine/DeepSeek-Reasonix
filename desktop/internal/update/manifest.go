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
// via Asset.
type Manifest struct {
	Version      string           `json:"version"`       // release version, e.g. "v1.1.0"
	Notes        string           `json:"notes"`         // markdown release notes
	PubDate      string           `json:"pub_date"`      // RFC3339, optional
	DownloadPage string           `json:"download_page"` // human-facing download page (macOS manual-update fallback)
	Platforms    map[string]Asset `json:"platforms"`     // keyed by PlatformKey, e.g. "darwin-arm64"
}

// Asset is one platform's downloadable artifact plus the metadata the updater
// needs to verify and report on it.
type Asset struct {
	URL    string `json:"url"`    // direct download URL for the artifact
	Sig    string `json:"sig"`    // URL of the detached minisign (.minisig) signature
	Size   int64  `json:"size"`   // artifact size in bytes (download-progress denominator)
	SHA256 string `json:"sha256"` // lowercase hex digest, for a second integrity check after verify
}

// PlatformKey is the map key used in Manifest.Platforms for the given OS/arch.
// The updater calls CurrentPlatform; the manifest generator builds keys the same
// way, so lookups always agree.
func PlatformKey(goos, goarch string) string { return goos + "-" + goarch }

// CurrentPlatform is PlatformKey for the running binary.
func CurrentPlatform() string { return PlatformKey(runtime.GOOS, runtime.GOARCH) }

// Asset returns the artifact for the running platform, if the manifest lists one.
func (m Manifest) Asset() (Asset, bool) {
	a, ok := m.Platforms[CurrentPlatform()]
	return a, ok
}
