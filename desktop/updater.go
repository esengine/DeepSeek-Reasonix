package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"reasonix/desktop/internal/update"
	"reasonix/internal/config"
	"reasonix/internal/installlayout"
	"reasonix/internal/netclient"
	"reasonix/internal/repair"
)

// updater.go is the transport-free core of the desktop auto-updater: manifest
// fetch, version comparison, signed download, and per-platform apply/relaunch. It
// has no Wails dependency so the logic is unit-tested directly; updater_app.go is
// the thin Wails binding that wires these into App methods and progress events.

// Manifest endpoints — R2 CDN first (fast, especially in CN), then the crash
// worker release gateway, then GitHub as the stable channel's last resort. The
// selected update channel picks the rolling pointer; it is user-configurable and
// independent from the build channel embedded for diagnostics/backcompat. The
// gateway still avoids GitHub's repository-wide /releases/latest shortcut so the
// app is not coupled to GitHub's homepage badge semantics.
const (
	r2Base                     = "https://dl.reasonix.io"
	releaseGatewayBase         = "https://crash.reasonix.io/v1/desktop/releases"
	downloadPageURL            = "https://reasonix.io/#start"
	manifestDownloadPageURL    = "https://reasonix.io/?download=desktop#start"
	httpTimeout                = 15 * time.Second
	manifestEndpointTimeout    = 5 * time.Second
	maxDesktopReleaseAssetSize = int64(1 << 30)
	maxDesktopManifestSize     = int64(1 << 20)
	maxDesktopSignatureSize    = int64(64 << 10)
)

var fetchAttemptTimeout = 5 * time.Second

var (
	stableDesktopVersionRE = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	sha256RE               = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type requiredDesktopAsset struct {
	group    string
	key      string
	filename string
}

var (
	requiredDesktopUpdaterAssets = []requiredDesktopAsset{
		{group: "platforms", key: "darwin-arm64", filename: "Reasonix-darwin-arm64.zip"},
		{group: "platforms", key: "darwin-amd64", filename: "Reasonix-darwin-amd64.zip"},
		{group: "platforms", key: "windows-amd64", filename: "Reasonix-windows-amd64-installer.exe"},
		{group: "platforms", key: "windows-arm64", filename: "Reasonix-windows-arm64-installer.exe"},
		{group: "platforms", key: "linux-amd64", filename: "Reasonix-linux-amd64.tar.gz"},
		{group: "native_packages", key: "linux-amd64", filename: "Reasonix-linux-amd64.deb"},
	}
	requiredDesktopDownloadAssets = []requiredDesktopAsset{
		{group: "downloads", key: "Reasonix-darwin-universal.dmg", filename: "Reasonix-darwin-universal.dmg"},
		{group: "downloads", key: "Reasonix-windows-amd64.zip", filename: "Reasonix-windows-amd64.zip"},
	}
)

// githubManifestFallback is the stable channel's last-resort manifest source.
// dl.reasonix.io and crash.reasonix.io share one Cloudflare zone, so bot
// protection that 403s a user's egress IP takes out both first-party endpoints
// at once (#6005); GitHub is separate infrastructure. Stable desktop releases
// own the repo-wide latest badge and publish latest.json directly, while
// The unified official Release carries the desktop manifest as a final fallback
// when both first-party endpoints are unavailable.
const githubManifestFallback = "https://github.com/esengine/DeepSeek-Reasonix/releases/latest/download/latest.json"

func normalizeUpdateChannel(ch string) string {
	return config.NormalizeDesktopUpdateChannel(ch)
}

func configuredUpdateChannel() string {
	cfg, err := config.Load()
	if err != nil {
		return "stable"
	}
	return cfg.DesktopUpdateChannel()
}

func targetUpdateChannel(selected string) string {
	_ = selected
	return configuredUpdateChannel()
}

func runningUpdateChannel() string {
	return normalizeUpdateChannel(channel)
}

// manifestEndpoints returns the manifest URLs for the selected update channel,
// in the order fetchManifest tries them.
func manifestEndpoints(selected string) []string {
	_ = selected
	return []string{
		r2Base + "/latest/latest.json",
		releaseGatewayBase + "/stable/latest.json",
		githubManifestFallback,
	}
}

// updaterUserAgent identifies updater traffic. Go's default Go-http-client UA
// is exactly what edge bot protection scores worst (#6005); a descriptive UA
// lets the release edge allowlist updater requests and makes them attributable
// in server logs.
func updaterUserAgent(selected string) string {
	return fmt.Sprintf("Reasonix-Updater/%s (%s/%s; build=%s; update=%s)", version, runtime.GOOS, runtime.GOARCH, channel, normalizeUpdateChannel(selected))
}

// downloadPage is the human-facing releases page shown when self-update is
// unavailable (macOS) or the manifest omits its own link.
func downloadPage(selected string) string {
	_ = selected
	u, _ := url.Parse(downloadPageURL)
	query := u.Query()
	query.Set("download", "desktop")
	query.Del("channel")
	u.RawQuery = query.Encode()
	return u.String()
}

func manifestDownloadPage(selected, manifestPage string) string {
	manifestPage = strings.TrimSpace(manifestPage)
	if manifestPage == "" {
		return downloadPage(selected)
	}
	u, err := url.Parse(manifestPage)
	if err != nil ||
		u.Scheme != "https" ||
		u.Hostname() == "" ||
		u.User != nil {
		return downloadPage(selected)
	}
	host := strings.ToLower(u.Hostname())
	if host != "reasonix.io" && !strings.HasSuffix(host, ".reasonix.io") {
		return u.String()
	}
	query := u.Query()
	query.Set("download", "desktop")
	query.Del("channel")
	u.RawQuery = query.Encode()
	u.Fragment = "start"
	return u.String()
}

// UpdateInfo is the CheckUpdate result that drives the frontend's update banner.
type UpdateInfo struct {
	Available         bool   `json:"available"`
	Current           string `json:"current"`
	Latest            string `json:"latest"`
	Notes             string `json:"notes"`
	Channel           string `json:"channel"`
	CanSelfUpdate     bool   `json:"canSelfUpdate"` // win/linux true; macOS true only for signed/notarized builds
	ManualOnly        bool   `json:"manualOnly,omitempty"`
	ManualReason      string `json:"manualReason,omitempty"`
	InstallMode       string `json:"installMode"`                 // portable | deb | manual
	RequiresElevation bool   `json:"requiresElevation,omitempty"` // deb/Polkit path
	Downloaded        bool   `json:"downloaded"`
	DownloadURL       string `json:"downloadUrl"`   // human-facing releases page (macOS path / fallback link)
	AssetSize         int64  `json:"assetSize"`     // running platform's artifact size, for the progress bar
	Err               string `json:"err,omitempty"` // set when the check itself failed (both endpoints down)
}

// UpdateDownloadResult is returned after an artifact has been downloaded,
// verified, and stored in the local updater cache.
type UpdateDownloadResult struct {
	RequestID string `json:"requestId"`
	Version   string `json:"version"`
	Channel   string `json:"channel"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}

// updateProgress is the payload of the "updater:progress" Wails event emitted
// throughout DownloadUpdate / InstallUpdate.
type updateProgress struct {
	RequestID string `json:"requestId"`
	Version   string `json:"version"`
	Channel   string `json:"channel"`
	Phase     string `json:"phase"` // downloading | verifying | downloaded | authorizing | recovering | installing | done | error
	Received  int64  `json:"received"`
	Total     int64  `json:"total"`
	Err       string `json:"err,omitempty"`
}

func httpClient() (*http.Client, error) { return newHTTPClient(false) }

// httpClientIPv4 pins the dialer to IPv4 — the download fallback when the default
// (often IPv6-first) route to Cloudflare keeps resetting mid-transfer.
func httpClientIPv4() (*http.Client, error) { return newHTTPClient(true) }

func newHTTPClient(forceIPv4 bool) (*http.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	c, err := netclient.NewHTTPClient(cfg.NetworkProxySpec(), netclient.TransportOptions{ForceIPv4: forceIPv4})
	if err != nil {
		return nil, err
	}
	c.CheckRedirect = validateUpdateRedirect
	return c, nil
}

func validateUpdateRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("update: stopped after 10 redirects")
	}
	if req == nil || req.URL == nil {
		return errors.New("update: redirect has no target URL")
	}
	if !strings.EqualFold(req.URL.Scheme, "https") {
		return fmt.Errorf("update: refusing redirect to non-HTTPS URL %q", req.URL.String())
	}
	if req.URL.Hostname() == "" {
		return fmt.Errorf("update: refusing redirect without a hostname %q", req.URL.String())
	}
	if req.URL.User != nil {
		return fmt.Errorf("update: refusing redirect with userinfo %q", req.URL.String())
	}
	if req.URL.Port() != "" || !isTrustedUpdateRedirectHost(req.URL.Hostname()) {
		return fmt.Errorf("update: refusing redirect to untrusted host %q", req.URL.Host)
	}
	return nil
}

func isTrustedUpdateRedirectHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return host == "reasonix.io" ||
		strings.HasSuffix(host, ".reasonix.io") ||
		host == "github.com" ||
		strings.HasSuffix(host, ".githubusercontent.com")
}

// canSelfUpdate reports whether in-place update is possible. Windows and Linux
// can replace the verified artifact directly; macOS requires an explicitly
// signed/notarized build flag so local or ad-hoc builds stay manual.
func canSelfUpdate() bool {
	return runtime.GOOS != "darwin" || macSelfUpdateAllowed()
}

func manualUpdateReason() string {
	if runtime.GOOS == "darwin" && !macSelfUpdateAllowed() {
		return "macOS automatic updates require a Developer ID signed and notarized build"
	}
	return ""
}

// normalizeVersion canonicalizes a version to semver "vX.Y.Z". It reports ok=false
// for the un-injected "dev" build (and anything not valid semver), so a dev build
// never prompts to update.
func normalizeVersion(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" || v == "dev" {
		return "", false
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return "", false
	}
	return semver.Canonical(v), true
}

// validateManifestChannel rejects every prerelease. The selected value remains
// in the signature for compatibility with existing callers.
func validateManifestChannel(selected string, m *update.Manifest) error {
	_ = selected
	if !stableDesktopVersionRE.MatchString(m.Version) {
		return fmt.Errorf("official manifest has invalid release version %q", m.Version)
	}
	return nil
}

func desktopReleaseTag(_ string, version string) string {
	return "desktop-" + version
}

func desktopAssetBases(selected, version string, allowLegacyPreview bool) []string {
	_ = selected
	_ = allowLegacyPreview
	tag := desktopReleaseTag(selected, version)
	return []string{
		fmt.Sprintf("%s/%s/", r2Base, tag),
		fmt.Sprintf("https://github.com/esengine/DeepSeek-Reasonix/releases/download/%s/", tag),
		fmt.Sprintf("https://github.com/esengine/DeepSeek-Reasonix/releases/download/%s/", version),
	}
}

func validateManifestAsset(selected, version, filename string, asset update.Asset, allowLegacyPreview bool) (string, error) {
	base := ""
	for _, candidate := range desktopAssetBases(selected, version, allowLegacyPreview) {
		if asset.URL == candidate+filename {
			base = candidate
			break
		}
	}
	if base == "" {
		return "", fmt.Errorf("asset URL %q is not the official %s path for %s", asset.URL, normalizeUpdateChannel(selected), filename)
	}
	if asset.Sig != asset.URL+".minisig" {
		return "", fmt.Errorf("asset signature URL %q does not match %q", asset.Sig, asset.URL+".minisig")
	}
	if asset.Size <= 0 || asset.Size > maxDesktopReleaseAssetSize {
		return "", fmt.Errorf("asset %s has invalid size %d", filename, asset.Size)
	}
	if !sha256RE.MatchString(asset.SHA256) {
		return "", fmt.Errorf("asset %s has invalid SHA-256 %q", filename, asset.SHA256)
	}
	if err := validateAssetInstallLayout(asset.InstallLayout); err != nil {
		return "", err
	}
	return base, nil
}

// validateAssetInstallLayout accepts the pre-v1.20 empty layout (flat install)
// and the v1.20+ versioned-v1 layout. Unknown values must fail closed so a new
// client never partially installs an unrecognized package shape.
func validateAssetInstallLayout(layout string) error {
	switch strings.TrimSpace(layout) {
	case "", installlayout.InstallLayoutVersionedV1:
		return nil
	default:
		return fmt.Errorf("unsupported install_layout %q (keeping current version)", layout)
	}
}

func validateDesktopManifest(selected string, m *update.Manifest) error {
	selected = normalizeUpdateChannel(selected)
	if err := validateManifestChannel(selected, m); err != nil {
		return err
	}
	if m.DownloadPage != manifestDownloadPageURL {
		return fmt.Errorf("%s manifest has invalid download page %q", selected, m.DownloadPage)
	}
	// Older public manifests predate the two website-only download assets. Keep
	// accepting their six signed updater artifacts so an upgrade to the first
	// single-channel release does not strand existing users. Once downloads is
	// present it is a new-format manifest and all eight assets are mandatory.
	legacyManifest := m.Downloads == nil
	requiredAssets := append([]requiredDesktopAsset(nil), requiredDesktopUpdaterAssets...)
	if !legacyManifest {
		requiredAssets = append(requiredAssets, requiredDesktopDownloadAssets...)
	}
	base := ""
	for _, required := range requiredAssets {
		var assets map[string]update.Asset
		switch required.group {
		case "platforms":
			assets = m.Platforms
		case "native_packages":
			assets = m.NativePackages
		case "downloads":
			assets = m.Downloads
		default:
			return fmt.Errorf("unsupported manifest asset group %q", required.group)
		}
		asset, ok := assets[required.key]
		if !ok {
			return fmt.Errorf("%s manifest has no %s asset for %s", selected, required.group, required.key)
		}
		assetBase, err := validateManifestAsset(selected, m.Version, required.filename, asset, legacyManifest)
		if err != nil {
			return fmt.Errorf("%s %s asset: %w", required.group, required.key, err)
		}
		if base != "" && assetBase != base {
			return fmt.Errorf("%s manifest mixes asset bases %q and %q", selected, base, assetBase)
		}
		base = assetBase
	}
	return nil
}

// fetchManifest pulls latest.json from each endpoint in order until one both
// responds, decodes, and matches an official release. Every endpoint's
// failure is kept — a user staring at a gateway 403 (#6005) needs to see that
// the R2 pointer failed too, not just whichever endpoint happened to die last.
func fetchManifest(ctx context.Context, c, fallback *http.Client, selected string) (*update.Manifest, error) {
	var errs []error
	selected = normalizeUpdateChannel(selected)
	for _, url := range manifestEndpoints(selected) {
		endpointCtx, cancel := context.WithTimeout(ctx, manifestEndpointTimeout)
		b, err := fetchManifestBytes(endpointCtx, c, fallback, selected, url)
		cancel()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var m update.Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		if err := validateDesktopManifest(selected, &m); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		return &m, nil
	}
	return nil, fmt.Errorf("update: fetch manifest: %w", errors.Join(errs...))
}

// fetchManifestBytes gives the default and IPv4 transports separate halves of
// the endpoint budget. A stalled IPv6 dial must not consume the whole timeout
// before the IPv4 fallback gets a chance to run (#6713).
func fetchManifestBytes(ctx context.Context, c, fallback *http.Client, selected, url string) ([]byte, error) {
	attemptTimeout := manifestEndpointTimeout / 2
	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	data, err := fetchBytesOnce(attemptCtx, c, selected, url, maxDesktopManifestSize)
	cancel()
	if err == nil || !isTransientFetchError(err) || fallback == nil {
		return data, err
	}
	attemptCtx, cancel = context.WithTimeout(ctx, attemptTimeout)
	fallbackData, fallbackErr := fetchBytesOnce(attemptCtx, fallback, selected, url, maxDesktopManifestSize)
	cancel()
	if fallbackErr == nil {
		return fallbackData, nil
	}
	return nil, errors.Join(err, fallbackErr)
}

// evaluateForChannel compares the running version against the selected channel's
// manifest and builds the frontend-facing result. I/O is limited to install-profile
// detection and cache probes so tests can inject a fixed profile below.
func evaluateForChannel(current, selected string, m *update.Manifest) UpdateInfo {
	return evaluateWithProfileForChannel(current, selected, m, profileForManifest(detectInstallProfile(), m))
}

func evaluateWithProfile(current string, m *update.Manifest, profile installProfile) UpdateInfo {
	return evaluateWithProfileForChannel(current, runningUpdateChannel(), m, profile)
}

// evaluateWithProfileForChannel is the pure comparison core once the install
// profile and selected update channel are known.
func evaluateWithProfileForChannel(current, selected string, m *update.Manifest, profile installProfile) UpdateInfo {
	selected = normalizeUpdateChannel(selected)
	page := manifestDownloadPage(selected, m.DownloadPage)
	info := UpdateInfo{
		Current:           current,
		Latest:            m.Version,
		Notes:             m.Notes,
		Channel:           selected,
		CanSelfUpdate:     profile.CanSelfUpdate,
		ManualOnly:        !profile.CanSelfUpdate,
		ManualReason:      profile.ManualReason,
		InstallMode:       profile.Mode,
		RequiresElevation: profile.RequiresElev,
		DownloadURL:       page,
	}
	// Preserve the pre-existing macOS gate when profile detection would otherwise
	// claim portable self-update on an unsigned build.
	if runtime.GOOS == "darwin" && !canSelfUpdate() {
		info.CanSelfUpdate = false
		info.ManualOnly = true
		info.RequiresElevation = false
		info.InstallMode = installModeManual
		if info.ManualReason == "" {
			info.ManualReason = manualUpdateReason()
		}
	}
	cur, okCur := normalizeVersion(current)
	latest, okLatest := normalizeVersion(m.Version)
	if !okLatest {
		info.Err = "manifest has no valid version"
		return info
	}
	// A dev/invalid running version never auto-prompts. Within a channel, only a
	// newer semver is an update. Across channels, a different target latest is an
	// explicit channel switch, so allow installing stable over a newer preview.
	if okCur {
		if selected != runningUpdateChannel() {
			info.Available = latest != cur
		} else if semver.Compare(latest, cur) > 0 {
			info.Available = true
		}
	}
	if a, kind, ok := selectUpdateAsset(m, profile); ok {
		info.AssetSize = a.Size
		info.Downloaded = cachedUpdateMatchesForChannel(selected, m.Version, a, kind)
	} else if a, ok := m.Asset(); ok {
		// Manual installs (or a missing native package) still surface the portable
		// artifact size so the UI can show how large the download is on the page.
		info.AssetSize = a.Size
	}
	return info
}

type cachedUpdate struct {
	Version       string `json:"version"`
	Channel       string `json:"channel"`
	Platform      string `json:"platform"`
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
	DownloadedAt  string `json:"downloadedAt"`
	ArtifactKind  string `json:"artifactKind,omitempty"`  // tarball | deb
	SignaturePath string `json:"signaturePath,omitempty"` // required for deb
}

var updateCacheBaseDir = defaultUpdateCacheBaseDir

func defaultUpdateCacheBaseDir() (string, error) {
	if cd := config.CacheDir(); cd != "" {
		return filepath.Join(cd, "updates"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "Reasonix", "updates"), nil
}

func updateCacheDir() (string, error) {
	dir, err := updateCacheBaseDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func updateMetadataPath() (string, error) {
	dir, err := updateCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "downloaded.json"), nil
}

func assetFileName(asset update.Asset, version string) string {
	if u, err := url.Parse(asset.URL); err == nil {
		if base := filepath.Base(u.Path); base != "." && base != "/" {
			return base
		}
	}
	clean := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-").Replace(version)
	return "Reasonix-" + clean + "-" + update.CurrentPlatform() + ".update"
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func saveCachedUpdate(version string, asset update.Asset, data []byte, kind string, signature []byte) (*cachedUpdate, error) {
	return saveCachedUpdateForChannel(runningUpdateChannel(), version, asset, data, kind, signature)
}

func saveCachedUpdateForChannel(selected, version string, asset update.Asset, data []byte, kind string, signature []byte) (*cachedUpdate, error) {
	selected = normalizeUpdateChannel(selected)
	if err := checkSHA256(data, asset.SHA256); err != nil {
		return nil, err
	}
	kind = artifactKindFromMeta(kind)
	dir, err := updateCacheDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, assetFileName(asset, version))
	if err := writeAtomic(path, data, 0o600); err != nil {
		return nil, err
	}
	meta := &cachedUpdate{
		Version:      version,
		Channel:      selected,
		Platform:     update.CurrentPlatform(),
		Path:         path,
		Size:         int64(len(data)),
		SHA256:       asset.SHA256,
		DownloadedAt: time.Now().UTC().Format(time.RFC3339),
		ArtifactKind: kind,
	}
	if kind == artifactKindDeb {
		if len(signature) == 0 {
			return nil, fmt.Errorf("update: deb cache requires a signature")
		}
		sigPath := path + ".minisig"
		if err := writeAtomic(sigPath, signature, 0o600); err != nil {
			return nil, err
		}
		meta.SignaturePath = sigPath
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	metadataPath, err := updateMetadataPath()
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(metadataPath, append(raw, '\n'), 0o600); err != nil {
		return nil, err
	}
	return meta, nil
}

func loadCachedUpdate() (*cachedUpdate, error) {
	path, err := updateMetadataPath()
	if err != nil {
		return nil, err
	}
	raw, err := readFileUTF8(path)
	if err != nil {
		return nil, err
	}
	var meta cachedUpdate
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, err
	}
	if meta.Version == "" || meta.Channel == "" || meta.Platform == "" || meta.Path == "" || meta.SHA256 == "" {
		return nil, fmt.Errorf("update: cached metadata is incomplete")
	}
	return &meta, nil
}

func cachedUpdateMatches(version string, asset update.Asset, kind string) bool {
	return cachedUpdateMatchesForChannel(runningUpdateChannel(), version, asset, kind)
}

func cachedUpdateMatchesForChannel(selected, version string, asset update.Asset, kind string) bool {
	selected = normalizeUpdateChannel(selected)
	meta, err := loadCachedUpdate()
	if err != nil {
		return false
	}
	kind = artifactKindFromMeta(kind)
	metaKind := artifactKindFromMeta(meta.ArtifactKind)
	// Legacy portable caches omit artifactKind and remain valid for tarball only.
	// Deb installs never reuse a cache that lacks a matching signature file.
	if kind == artifactKindDeb {
		if metaKind != artifactKindDeb || meta.SignaturePath == "" {
			return false
		}
		if _, err := os.Stat(meta.SignaturePath); err != nil {
			return false
		}
	} else if metaKind != artifactKindTarball {
		return false
	}
	return meta.Version == version &&
		meta.Channel == selected &&
		meta.Platform == update.CurrentPlatform() &&
		strings.EqualFold(meta.SHA256, asset.SHA256) &&
		meta.Size == asset.Size &&
		fileSHA256Matches(meta.Path, meta.SHA256)
}

func fileSHA256Matches(path, want string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want)
}

func readVerifiedCachedUpdate() (*cachedUpdate, []byte, error) {
	return readVerifiedCachedUpdateForChannel(runningUpdateChannel())
}

func readVerifiedCachedUpdateForChannel(selected string) (*cachedUpdate, []byte, error) {
	selected = normalizeUpdateChannel(selected)
	meta, err := loadCachedUpdate()
	if err != nil {
		return nil, nil, err
	}
	if meta.Channel != selected {
		return nil, nil, fmt.Errorf("update: cached update is for %s channel, selected channel is %s", meta.Channel, selected)
	}
	if meta.Platform != update.CurrentPlatform() {
		return nil, nil, fmt.Errorf("update: cached update is for %s, current platform is %s", meta.Platform, update.CurrentPlatform())
	}
	data, err := os.ReadFile(meta.Path)
	if err != nil {
		return nil, nil, err
	}
	if err := checkSHA256(data, meta.SHA256); err != nil {
		return nil, nil, err
	}
	meta.ArtifactKind = artifactKindFromMeta(meta.ArtifactKind)
	if meta.ArtifactKind == artifactKindDeb {
		if meta.SignaturePath == "" {
			return nil, nil, fmt.Errorf("update: cached deb is missing its signature")
		}
		if _, err := os.Stat(meta.SignaturePath); err != nil {
			return nil, nil, fmt.Errorf("update: cached deb signature is missing")
		}
	}
	return meta, data, nil
}

// downloadAttempts caps how many times a transient transport failure (connection
// reset, read timeout, gateway 5xx) is retried before the update gives up. CN IPv6
// routes to Cloudflare reset mid-transfer often enough that a retry or two usually
// completes the download instead of surfacing a "forcibly closed" error.
const downloadAttempts = 3

// retryBackoff is the pause before the Nth retry; a package var so tests shrink it.
var retryBackoff = func(attempt int) time.Duration { return time.Duration(attempt) * 500 * time.Millisecond }

// retryTransient runs attempt 1..downloadAttempts of fetch, pausing between tries,
// until one succeeds. fetch receives the 1-based attempt number so a caller can
// switch transports on a retry. It stops early when ctx is cancelled (window closed
// / user cancelled). Only the transport is retried; the signature and sha256 checks
// run downstream in downloadVerify and are not retried.
func retryTransient(ctx context.Context, fetch func(attempt int) error) error {
	var err error
	for attempt := 1; attempt <= downloadAttempts; attempt++ {
		if err = fetch(attempt); err == nil {
			return nil
		}
		if !isTransientFetchError(err) {
			break
		}
		if ctx.Err() != nil || attempt == downloadAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryBackoff(attempt)):
		}
	}
	return err
}

type httpStatusError struct {
	url    string
	status string
	code   int
}

func (e *httpStatusError) Error() string { return fmt.Sprintf("GET %s: %s", e.url, e.status) }

func isTransientFetchError(err error) bool {
	if errors.Is(err, errUpdateResponseTooLarge) {
		return false
	}
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return true
	}
	return statusErr.code == http.StatusRequestTimeout || statusErr.code == http.StatusTooManyRequests || statusErr.code >= 500
}

// fetchBytes GETs a URL fully into memory, retrying transient transport failures.
func fetchBytes(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	return fetchBytesFallbackForChannel(ctx, c, nil, runningUpdateChannel(), url)
}

// fetchBytesFallback retries transport failures with the IPv4-pinned client.
// This covers small manifest/signature requests as well as the artifact body;
// previously only the large artifact download escaped a broken IPv6 route.
func fetchBytesFallback(ctx context.Context, c, fallback *http.Client, url string) ([]byte, error) {
	return fetchBytesFallbackForChannel(ctx, c, fallback, runningUpdateChannel(), url)
}

func fetchBytesFallbackForChannel(ctx context.Context, c, fallback *http.Client, selected, url string) ([]byte, error) {
	return fetchBytesFallbackForChannelSized(ctx, c, fallback, selected, url, maxDesktopManifestSize)
}

func fetchBytesFallbackForChannelSized(
	ctx context.Context,
	c, fallback *http.Client,
	selected, url string,
	maxBytes int64,
) ([]byte, error) {
	selected = normalizeUpdateChannel(selected)
	var data []byte
	err := retryTransient(ctx, func(attempt int) error {
		client := c
		if attempt > 1 && fallback != nil {
			client = fallback
		}
		var e error
		attemptCtx, cancel := context.WithTimeout(ctx, fetchAttemptTimeout)
		data, e = fetchBytesOnce(attemptCtx, client, selected, url, maxBytes)
		cancel()
		return e
	})
	return data, err
}

var errUpdateResponseTooLarge = errors.New("update: response exceeds allowed size")

func fetchBytesOnce(ctx context.Context, c *http.Client, selected, url string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("update: invalid response size limit %d", maxBytes)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", updaterUserAgent(selected))
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{url: url, status: resp.Status, code: resp.StatusCode}
	}
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("%w: GET %s declared %d bytes, maximum is %d", errUpdateResponseTooLarge, url, resp.ContentLength, maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: GET %s exceeded %d bytes", errUpdateResponseTooLarge, url, maxBytes)
	}
	return data, nil
}

// download fetches url into memory, invoking onProgress as bytes arrive. A transient
// transport failure is retried; the retry resumes from the bytes already received
// via a Range request instead of restarting, and switches to the IPv4 fallback
// client (when provided) since a reset usually means the IPv6 route is the problem.
// total is the expected size for the progress denominator (refined from the response).
func download(ctx context.Context, c, fallback *http.Client, url string, total int64, onProgress func(received, total int64)) ([]byte, error) {
	return downloadForChannel(ctx, c, fallback, runningUpdateChannel(), url, total, onProgress)
}

func downloadForChannel(ctx context.Context, c, fallback *http.Client, selected, url string, total int64, onProgress func(received, total int64)) ([]byte, error) {
	selected = normalizeUpdateChannel(selected)
	if total < 0 || total > maxDesktopReleaseAssetSize {
		return nil, fmt.Errorf("update: invalid expected asset size %d", total)
	}
	expectedSize := total
	var buf bytes.Buffer
	err := retryTransient(ctx, func(attempt int) error {
		client := c
		if attempt > 1 && fallback != nil {
			client = fallback
		}
		return downloadInto(ctx, client, selected, url, expectedSize, &buf, &total, onProgress)
	})
	if err != nil {
		return nil, err
	}
	if expectedSize > 0 && int64(buf.Len()) != expectedSize {
		return nil, fmt.Errorf("update: downloaded size mismatch: got %d want %d", buf.Len(), expectedSize)
	}
	return buf.Bytes(), nil
}

// downloadInto appends url's body to buf, resuming from buf's current length via a
// Range request so a retry continues the partial download. A 206 carries the
// remaining bytes; a 200 means the server ignored Range, so buf is reset and the
// whole file re-downloaded. total is refined from the response for the progress
// denominator (Content-Length on 200, the size field of Content-Range on 206).
func downloadInto(ctx context.Context, c *http.Client, selected, url string, expectedSize int64, buf *bytes.Buffer, total *int64, onProgress func(received, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", updaterUserAgent(selected))
	if buf.Len() > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", buf.Len()))
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		buf.Reset()
		if resp.ContentLength > 0 {
			if resp.ContentLength > maxDesktopReleaseAssetSize {
				return fmt.Errorf("update: response size %d exceeds maximum %d", resp.ContentLength, maxDesktopReleaseAssetSize)
			}
			*total = resp.ContentLength
		}
	case http.StatusPartialContent:
		if t := totalFromContentRange(resp.Header.Get("Content-Range")); t > 0 {
			if t > maxDesktopReleaseAssetSize {
				return fmt.Errorf("update: response size %d exceeds maximum %d", t, maxDesktopReleaseAssetSize)
			}
			*total = t
		}
	default:
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	have := int64(buf.Len())
	if expectedSize > 0 && have > expectedSize {
		return fmt.Errorf("update: downloaded size exceeds manifest: got at least %d want %d", have, expectedSize)
	}
	limit := maxDesktopReleaseAssetSize - have + 1
	if expectedSize > 0 {
		limit = expectedSize - have + 1
	}
	body := io.LimitReader(resp.Body, limit)
	pr := &progressReader{r: body, received: have, lastEmit: have, total: *total, onProgress: onProgress}
	_, err = io.Copy(buf, pr)
	if err == nil && expectedSize > 0 && int64(buf.Len()) > expectedSize {
		return fmt.Errorf("update: downloaded size exceeds manifest: got at least %d want %d", buf.Len(), expectedSize)
	}
	if err == nil && int64(buf.Len()) > maxDesktopReleaseAssetSize {
		return fmt.Errorf("update: downloaded size exceeds maximum %d", maxDesktopReleaseAssetSize)
	}
	return err
}

// totalFromContentRange parses the total size out of a "bytes 200-999/1000" header,
// returning 0 when it's absent or "*" (unknown).
func totalFromContentRange(v string) int64 {
	i := strings.LastIndex(v, "/")
	if i < 0 {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v[i+1:]), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// progressReader reports cumulative bytes read, throttled so the event channel
// isn't flooded.
type progressReader struct {
	r          io.Reader
	received   int64
	total      int64
	lastEmit   int64
	onProgress func(received, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.received += int64(n)
	// Emit roughly every 256 KiB, and always on the final read (io.EOF).
	if p.onProgress != nil && (p.received-p.lastEmit >= 256<<10 || err == io.EOF) {
		p.lastEmit = p.received
		p.onProgress(p.received, p.total)
	}
	return n, err
}

// checkSHA256 verifies data's digest matches the lowercase-hex want.
func checkSHA256(data []byte, want string) error {
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, want) {
		return fmt.Errorf("update: sha256 mismatch: got %s want %s", got, want)
	}
	return nil
}

// extractBinary pulls a single named regular file out of a .tar.gz blob.
func extractBinary(targz []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(targz))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && (h.Name == name || strings.HasSuffix(h.Name, "/"+name)) {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("update: %q not found in archive", name)
}

func extractLinuxReleaseUnit(targz []byte) (map[string][]byte, error) {
	const (
		desktop = "reasonix-desktop"
		guard   = "reasonix-guard"
		cli     = "reasonix"
	)
	want := map[string]struct{}{desktop: {}, guard: {}, cli: {}}
	found := make(map[string][]byte, len(want))
	gz, err := gzip.NewReader(bytes.NewReader(targz))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		name := path.Base(strings.TrimSpace(h.Name))
		if _, ok := want[name]; !ok {
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 {
			return nil, fmt.Errorf("update: release member %q is not a regular file", name)
		}
		if _, duplicate := found[name]; duplicate {
			return nil, fmt.Errorf("update: release member %q appears more than once", name)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		found[name] = body
	}
	if len(found) != len(want) {
		for name := range want {
			if _, ok := found[name]; !ok {
				return nil, fmt.Errorf("update: release member %q not found in archive", name)
			}
		}
	}
	return found, nil
}

// applyLinux replaces the running binary with the one inside the downloaded
// tar.gz; the caller relaunches afterwards.
func applyLinux(targz []byte, prepared *repair.UpdateTransaction) error {
	release, err := extractLinuxReleaseUnit(targz)
	if err != nil {
		return err
	}
	bin := release["reasonix-desktop"]
	guard := release["reasonix-guard"]
	cli := release["reasonix"]
	exe := currentExecutablePathForLinux()
	if exe == "" {
		return fmt.Errorf("update: current executable path is unavailable")
	}
	releasePaths := releaseUnitPathsFor(filepath.Dir(exe), "linux")
	if prepared == nil {
		return fmt.Errorf("update: prepared transaction is unavailable")
	}
	claimed, releaseClaim, err := repair.ClaimPendingFileUpdateExact(
		prepared.ToVersion,
		prepared.CreatedAt,
		repair.UpdateTransactionID(prepared),
		exe,
		releasePaths,
		2*time.Minute,
	)
	if err != nil {
		return fmt.Errorf("update: claim prepared transaction: %w", err)
	}
	defer releaseClaim()
	if err := repair.MarkUpdateApplyFailedExact(claimed, "Linux update publish did not complete"); err != nil {
		return fmt.Errorf("update: record recovery intent: %w", err)
	}
	receipts, err := applyLinuxReleaseUnit(claimed, exe, bin, guard, cli)
	if err != nil {
		return err
	}
	if _, err := repair.RecordClaimedFileUpdateInstalled(claimed, receipts...); err != nil {
		return fmt.Errorf("update: record installed release unit: %w", err)
	}
	// pending-update.json remains immutable; the transaction-unique sidecar now
	// binds every installed member. A crash before marker cleanup is safe:
	// startup correlates the exact transaction and rolls the release unit back.
	_ = repair.ClearUpdateApplyFailureExact(claimed)
	return nil
}

// applyLinuxVersioned publishes a verified compatibility tarball into a new
// version directory and swaps current.json last. The tar still contains the
// one-shot reasonix-guard member for v1.18-v1.19 updaters, but v1.20+ ignores
// that member and never persists it again.
func applyLinuxVersioned(targz []byte, targetVersion string) error {
	release, err := extractLinuxReleaseUnit(targz)
	if err != nil {
		return err
	}
	root := currentInstallDirForLinuxUpdate()
	if _, err := installlayout.ReadCurrent(root); err != nil {
		return fmt.Errorf("update: resolve active Linux layout: %w", err)
	}
	targetVersion = strings.TrimSpace(targetVersion)
	if !strings.HasPrefix(targetVersion, "v") {
		targetVersion = "v" + targetVersion
	}
	if err := installlayout.ValidateVersionName(targetVersion); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(root, ".reasonix-linux-update-*")
	if err != nil {
		return fmt.Errorf("update: create Linux version staging: %w", err)
	}
	defer os.RemoveAll(staging)
	desktopPath := filepath.Join(staging, installlayout.DesktopBinaryName())
	cliPath := filepath.Join(staging, installlayout.CLIBinaryName())
	if err := os.WriteFile(desktopPath, release["reasonix-desktop"], 0o700); err != nil {
		return fmt.Errorf("update: stage Linux desktop: %w", err)
	}
	if err := os.WriteFile(cliPath, release["reasonix"], 0o700); err != nil {
		return fmt.Errorf("update: stage Linux CLI: %w", err)
	}
	if err := installlayout.ActivateVersion(installlayout.ActivationRequest{
		InstallRoot: root,
		Version:     targetVersion,
		RequestID:   "linux-" + targetVersion,
		Members: []installlayout.Member{
			{Name: installlayout.DesktopBinaryName(), Path: desktopPath, Mode: 0o700},
			{Name: installlayout.CLIBinaryName(), Path: cliPath, Mode: 0o700},
		},
		RequiredNames: []string{installlayout.DesktopBinaryName(), installlayout.CLIBinaryName()},
	}); err != nil {
		return fmt.Errorf("update: activate Linux version: %w", err)
	}
	_ = installlayout.RetainPreviousVersions(root, 0)
	return nil
}

var currentExecutablePathForLinux = currentExecutablePath
var currentInstallDirForLinuxUpdate = currentInstallDir

var applyLinuxReleaseUnit = func(
	claimed *repair.UpdateTransaction,
	exe string,
	bin, guard, cli []byte,
) ([]repair.FileUpdateInstallReceipt, error) {
	receipts := make([]repair.FileUpdateInstallReceipt, 0, 3)
	receipt, err := repair.PublishClaimedFileUpdateMemberExact(claimed, filepath.Join(filepath.Dir(exe), "reasonix"), cli, 0o700)
	if err != nil {
		return receipts, fmt.Errorf("update CLI sidecar: %w", err)
	}
	receipts = append(receipts, receipt)
	receipt, err = repair.PublishClaimedFileUpdateMemberExact(claimed, filepath.Join(filepath.Dir(exe), "reasonix-guard"), guard, 0o700)
	if err != nil {
		return receipts, fmt.Errorf("update Guard: %w", err)
	}
	receipts = append(receipts, receipt)
	receipt, err = repair.PublishClaimedFileUpdateMemberExact(claimed, exe, bin, 0o700)
	if err != nil {
		return receipts, fmt.Errorf("update desktop: %w", err)
	}
	receipts = append(receipts, receipt)
	return receipts, nil
}

func applyWindowsFile(path, expectedSHA256, targetVersion string, prepared *repair.UpdateTransaction) error {
	installDir := currentInstallDir()
	if installlayout.HasCurrent(installDir) {
		return startWindowsVersionedUpdateHandoff(
			path,
			expectedSHA256,
			installDir,
			currentLauncherPath(),
			targetVersion,
		)
	}
	if prepared == nil {
		return fmt.Errorf("update: prepared transaction is unavailable")
	}
	return startWindowsUpdateHandoff(
		path,
		expectedSHA256,
		installDir,
		currentLauncherPath(),
		prepared,
	)
}

func currentExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe
}

// currentInstallDir is the InstallRoot for updates. For the versioned layout it
// is the directory that owns current.json (not versions/<ver>/). For flat
// installs it is the directory of the running executable.
func currentInstallDir() string {
	exe := currentExecutablePath()
	if exe == "" {
		return ""
	}
	if root, err := installlayout.ResolveInstallRoot(exe); err == nil && root != "" {
		return root
	}
	return filepath.Dir(exe)
}

// archiveSupersededPendingUpdateAfterReady retires a transaction only after the
// current desktop has shown a usable UI. App-bundle recovery handles interrupted
// macOS generations; the versioned-layout branch handles older flat Windows and
// Linux transactions.
func archiveSupersededPendingUpdateAfterReady() (bool, error) {
	exe := currentExecutablePath()
	if exe == "" || version == "" || version == "dev" {
		return false, nil
	}
	if archived, err := repair.ArchiveSupersededPendingAppBundleUpdate(version); err != nil || archived {
		return archived, err
	}
	if runtime.GOOS == "darwin" {
		return false, nil
	}
	root, err := installlayout.ResolveInstallRoot(exe)
	if err != nil {
		return false, err
	}
	ptr, err := installlayout.ReadCurrent(root)
	if err != nil {
		// Package-managed and legacy flat installs have no versioned pointer and
		// therefore are not authorized to retire a transaction.
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	running := strings.TrimSpace(version)
	if !strings.HasPrefix(running, "v") {
		running = "v" + running
	}
	if ptr.ActiveVersion != running {
		return false, fmt.Errorf("active install version %s does not match running version %s", ptr.ActiveVersion, running)
	}
	return repair.ArchiveSupersededPendingFileUpdate(running, root)
}

func capturePendingUpdateHealthIdentity(app *App) {
	if app == nil {
		return
	}
	tx, err := readPendingUpdateForHealth()
	if err != nil || tx == nil || strings.TrimSpace(tx.ToVersion) != strings.TrimSpace(version) {
		return
	}
	app.healthyUpdateCreatedAt = tx.CreatedAt
	app.healthyUpdateTransactionID = repair.UpdateTransactionID(tx)
}

// updateSiblingArtifacts lists the packaged binaries an update replaces beside
// the main executable, so PrepareFileUpdate can snapshot the complete release
// unit. Paths that do not exist on disk are skipped by the backup.
func updateSiblingArtifacts() []string {
	dir := currentInstallDir()
	if dir == "" {
		return nil
	}
	paths := releaseUnitPathsFor(dir, runtime.GOOS)
	if len(paths) <= 1 {
		return nil
	}
	return paths[1:]
}

func releaseUnitPathsFor(dir, goos string) []string {
	if dir == "" {
		return nil
	}
	// Versioned-v1 layout: primary is the active desktop under versions/.
	if goos == "windows" && installlayout.HasCurrent(dir) {
		paths := make([]string, 0, 6)
		if desktop, err := installlayout.ActiveDesktopPath(dir); err == nil {
			paths = append(paths, desktop)
		} else {
			paths = append(paths, filepath.Join(dir, "reasonix-desktop.exe"))
		}
		if helper, err := installlayout.ActiveUpdateHelperPath(dir); err == nil {
			paths = append(paths, helper)
		}
		if cli, err := installlayout.ActiveCLIPath(dir); err == nil {
			paths = append(paths, cli)
		}
		for _, name := range []string{"reasonix-launcher.exe", "reasonix-cli.exe", "Reasonix.exe"} {
			paths = append(paths, filepath.Join(dir, name))
		}
		return paths
	}
	names := updateSiblingNames(goos)
	paths := make([]string, 0, len(names)+1)
	switch goos {
	case "linux":
		paths = append(paths, filepath.Join(dir, "reasonix-desktop"))
	case "windows":
		paths = append(paths, filepath.Join(dir, "reasonix-desktop.exe"))
	}
	if len(names) == 0 {
		return paths
	}
	for _, name := range names {
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths
}

func updateSiblingNames(goos string) []string {
	switch goos {
	case "windows":
		// Legacy flat release unit. reasonix-guard.exe may still exist on disk
		// during migration from 1.18–1.19.1; the new layout omits it.
		return []string{"reasonix-guard.exe", "reasonix-launcher.exe", "reasonix-update-helper.exe", "reasonix-cli.exe", "Reasonix.exe"}
	case "linux":
		return []string{"reasonix-guard", "reasonix"}
	default:
		return nil
	}
}

// relaunchThroughLauncher starts the permanent thin launcher (or falls back to
// the running executable). A legacy Guard binary is considered only as a
// one-release migration fallback for flat 1.18-1.19.1 installations.
func relaunchThroughLauncher() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	root := filepath.Dir(exe)
	if resolved, err := installlayout.ResolveInstallRoot(exe); err == nil && resolved != "" {
		root = resolved
	}
	candidates := []string{
		filepath.Join(root, "reasonix-launcher"),
		filepath.Join(root, "Reasonix.exe"),
		filepath.Join(root, "reasonix-guard"), // migration window only
	}
	if runtime.GOOS == "windows" {
		candidates[0] += ".exe"
		candidates[2] += ".exe"
	}
	launcher := exe
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			launcher = path
			break
		}
	}
	args := []string{}
	// Only legacy guard understands "launch --detach"; the thin launcher strips it.
	if strings.Contains(strings.ToLower(filepath.Base(launcher)), "guard") {
		args = []string{"launch", "--detach"}
	}
	cmd := exec.Command(launcher, args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Start()
}

func currentLauncherPath() string {
	exe := currentExecutablePath()
	if exe == "" {
		return ""
	}
	root := filepath.Dir(exe)
	if resolved, err := installlayout.ResolveInstallRoot(exe); err == nil && resolved != "" {
		root = resolved
	}
	for _, name := range []string{"reasonix-launcher.exe", "Reasonix.exe", "reasonix-launcher", "reasonix-guard.exe", "reasonix-guard"} {
		if runtime.GOOS != "windows" && strings.HasSuffix(name, ".exe") {
			continue
		}
		if runtime.GOOS == "windows" && !strings.HasSuffix(name, ".exe") && name != "Reasonix.exe" {
			// Unix names on Windows are unused.
			if !strings.HasSuffix(name, ".exe") {
				continue
			}
		}
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	// Fall through to previous flat-dir behavior for incomplete installs.
	if runtime.GOOS == "windows" {
		guard := filepath.Join(filepath.Dir(exe), "reasonix-guard.exe")
		if _, err := os.Stat(guard); err == nil {
			return guard
		}
	}
	return exe
}
