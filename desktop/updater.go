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
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/minio/selfupdate"
	"golang.org/x/mod/semver"

	"reasonix/desktop/internal/update"
	"reasonix/internal/config"
	"reasonix/internal/netclient"
)

// updater.go is the transport-free core of the desktop auto-updater: manifest
// fetch, version comparison, signed download, and per-platform apply/relaunch. It
// has no Wails dependency so the logic is unit-tested directly; updater_app.go is
// the thin Wails binding that wires these into App methods and progress events.

// Manifest endpoints — R2 CDN first (fast, especially in CN), then the crash
// worker release gateway, then GitHub as the stable channel's last resort. The
// build channel picks the rolling pointer so a canary build polls the canary
// line and a stable build polls latest; the two never cross. The gateway
// deliberately avoids GitHub's repository-wide /releases/latest shortcut
// because CLI tags (v*) and desktop tags (desktop-v*) are separate release
// lines in the same repo.
const (
	r2Base             = "https://dl.reasonix.io"
	releaseGatewayBase = "https://crash.reasonix.io/v1/desktop/releases"
	downloadPageURL    = "https://reasonix.io/#start"
	httpTimeout        = 15 * time.Second
)

// githubManifestFallback is the stable channel's last-resort manifest source.
// dl.reasonix.io and crash.reasonix.io share one Cloudflare zone, so bot
// protection that 403s a user's egress IP takes out both first-party endpoints
// at once (#6005); GitHub is separate infrastructure. The URL is safe despite
// the repository-wide /releases/latest caveat above: release.yml pins the
// repo-wide latest badge to the CLI line and attaches a desktop-manifest mirror
// to every stable CLI release ("desktop manifest compatibility asset"), so this
// asset is always the desktop manifest. Canary has no GitHub release, so its
// chain stays two-deep.
const githubManifestFallback = "https://github.com/esengine/DeepSeek-Reasonix/releases/latest/download/latest.json"

// manifestEndpoints returns the manifest URLs for the running build's channel,
// in the order fetchManifest tries them.
func manifestEndpoints() []string {
	if channel == "canary" {
		return []string{
			r2Base + "/canary/latest.json",
			releaseGatewayBase + "/canary/latest.json",
		}
	}
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
func updaterUserAgent() string {
	return fmt.Sprintf("Reasonix-Updater/%s (%s/%s; %s)", version, runtime.GOOS, runtime.GOARCH, channel)
}

// downloadPage is the human-facing releases page shown when self-update is
// unavailable (macOS) or the manifest omits its own link.
func downloadPage() string {
	return downloadPageURL
}

// UpdateInfo is the CheckUpdate result that drives the frontend's update banner.
type UpdateInfo struct {
	Available     bool   `json:"available"`
	Current       string `json:"current"`
	Latest        string `json:"latest"`
	Notes         string `json:"notes"`
	Channel       string `json:"channel"`
	CanSelfUpdate bool   `json:"canSelfUpdate"` // win/linux true; macOS true only for signed/notarized builds
	ManualOnly    bool   `json:"manualOnly,omitempty"`
	ManualReason  string `json:"manualReason,omitempty"`
	Downloaded    bool   `json:"downloaded"`
	DownloadURL   string `json:"downloadUrl"`   // human-facing releases page (macOS path / fallback link)
	AssetSize     int64  `json:"assetSize"`     // running platform's artifact size, for the progress bar
	Err           string `json:"err,omitempty"` // set when the check itself failed (both endpoints down)
}

// UpdateDownloadResult is returned after an artifact has been downloaded,
// verified, and stored in the local updater cache.
type UpdateDownloadResult struct {
	Version string `json:"version"`
	Channel string `json:"channel"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

// updateProgress is the payload of the "updater:progress" Wails event emitted
// throughout DownloadUpdate / InstallUpdate.
type updateProgress struct {
	Phase    string `json:"phase"` // downloading | verifying | downloaded | installing | done | error
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
	Err      string `json:"err,omitempty"`
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
	return netclient.NewHTTPClient(cfg.NetworkProxySpec(), netclient.TransportOptions{ForceIPv4: forceIPv4})
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

// fetchManifest pulls latest.json from each endpoint in order until one both
// responds and decodes. Every endpoint's failure is kept — a user staring at a
// gateway 403 (#6005) needs to see that the R2 pointer failed too, not just
// whichever endpoint happened to die last.
func fetchManifest(ctx context.Context, c *http.Client) (*update.Manifest, error) {
	var errs []error
	for _, url := range manifestEndpoints() {
		b, err := fetchBytes(ctx, c, url)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var m update.Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		return &m, nil
	}
	return nil, fmt.Errorf("update: fetch manifest: %w", errors.Join(errs...))
}

// evaluate compares the running version against the manifest and builds the
// frontend-facing result. Pure (no I/O) so the comparison is unit-tested.
func evaluate(current string, m *update.Manifest) UpdateInfo {
	page := m.DownloadPage
	if page == "" {
		page = downloadPage()
	}
	info := UpdateInfo{
		Current:       current,
		Latest:        m.Version,
		Notes:         m.Notes,
		Channel:       channel,
		CanSelfUpdate: canSelfUpdate(),
		ManualOnly:    !canSelfUpdate(),
		ManualReason:  manualUpdateReason(),
		DownloadURL:   page,
	}
	cur, okCur := normalizeVersion(current)
	latest, okLatest := normalizeVersion(m.Version)
	if !okLatest {
		info.Err = "manifest has no valid version"
		return info
	}
	// A dev/invalid running version never auto-prompts.
	if okCur && semver.Compare(latest, cur) > 0 {
		info.Available = true
	}
	if a, ok := m.Asset(); ok {
		info.AssetSize = a.Size
		info.Downloaded = cachedUpdateMatches(m.Version, a)
	}
	return info
}

type cachedUpdate struct {
	Version      string `json:"version"`
	Channel      string `json:"channel"`
	Platform     string `json:"platform"`
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	DownloadedAt string `json:"downloadedAt"`
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

func saveCachedUpdate(version string, asset update.Asset, data []byte) (*cachedUpdate, error) {
	if err := checkSHA256(data, asset.SHA256); err != nil {
		return nil, err
	}
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
		Channel:      channel,
		Platform:     update.CurrentPlatform(),
		Path:         path,
		Size:         int64(len(data)),
		SHA256:       asset.SHA256,
		DownloadedAt: time.Now().UTC().Format(time.RFC3339),
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
	raw, err := os.ReadFile(path)
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

func cachedUpdateMatches(version string, asset update.Asset) bool {
	meta, err := loadCachedUpdate()
	if err != nil {
		return false
	}
	return meta.Version == version &&
		meta.Channel == channel &&
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
	meta, err := loadCachedUpdate()
	if err != nil {
		return nil, nil, err
	}
	if meta.Channel != channel {
		return nil, nil, fmt.Errorf("update: cached update is for %s channel, current channel is %s", meta.Channel, channel)
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

// fetchBytes GETs a URL fully into memory, retrying transient transport failures.
func fetchBytes(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	var data []byte
	err := retryTransient(ctx, func(int) error {
		var e error
		data, e = fetchBytesOnce(ctx, c, url)
		return e
	})
	return data, err
}

func fetchBytesOnce(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", updaterUserAgent())
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// download fetches url into memory, invoking onProgress as bytes arrive. A transient
// transport failure is retried; the retry resumes from the bytes already received
// via a Range request instead of restarting, and switches to the IPv4 fallback
// client (when provided) since a reset usually means the IPv6 route is the problem.
// total is the expected size for the progress denominator (refined from the response).
func download(ctx context.Context, c, fallback *http.Client, url string, total int64, onProgress func(received, total int64)) ([]byte, error) {
	var buf bytes.Buffer
	err := retryTransient(ctx, func(attempt int) error {
		client := c
		if attempt > 1 && fallback != nil {
			client = fallback
		}
		return downloadInto(ctx, client, url, &buf, &total, onProgress)
	})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// downloadInto appends url's body to buf, resuming from buf's current length via a
// Range request so a retry continues the partial download. A 206 carries the
// remaining bytes; a 200 means the server ignored Range, so buf is reset and the
// whole file re-downloaded. total is refined from the response for the progress
// denominator (Content-Length on 200, the size field of Content-Range on 206).
func downloadInto(ctx context.Context, c *http.Client, url string, buf *bytes.Buffer, total *int64, onProgress func(received, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", updaterUserAgent())
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
			*total = resp.ContentLength
		}
	case http.StatusPartialContent:
		if t := totalFromContentRange(resp.Header.Get("Content-Range")); t > 0 {
			*total = t
		}
	default:
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	have := int64(buf.Len())
	pr := &progressReader{r: resp.Body, received: have, lastEmit: have, total: *total, onProgress: onProgress}
	_, err = io.Copy(buf, pr)
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

// applyLinux replaces the running binary with the one inside the downloaded
// tar.gz; the caller relaunches afterwards.
func applyLinux(targz []byte) error {
	bin, err := extractBinary(targz, "reasonix-desktop")
	if err != nil {
		return err
	}
	return selfupdate.Apply(bytes.NewReader(bin), selfupdate.Options{})
}

func applyWindowsFile(path string) error {
	return startWindowsUpdateHandoff(path, currentInstallDir(), currentExecutablePath())
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

// currentInstallDir is the directory of the running executable — the location a
// Windows update must overwrite. Empty when it can't be resolved, in which case
// the installer falls back to its own InstallDir logic.
func currentInstallDir() string {
	exe := currentExecutablePath()
	if exe == "" {
		return ""
	}
	return filepath.Dir(exe)
}

// relaunch starts a fresh copy of the (just-replaced) executable.
func relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Start()
}
