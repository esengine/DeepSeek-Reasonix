package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// Manifest endpoints — R2 CDN first (fast, especially in CN), GitHub releases as
// fallback. The build channel picks the rolling pointer so a canary build polls
// the canary line and a stable build polls latest; the two never cross.
const (
	r2Base         = "https://dl.reasonix.io"
	ghReleasesBase = "https://github.com/esengine/reasonix/releases"
	httpTimeout    = 15 * time.Second
)

// manifestEndpoints returns the primary (R2) then fallback (GitHub) manifest URLs
// for the running build's channel.
func manifestEndpoints() []string {
	if channel == "canary" {
		// Canary publishes only to R2 (no GitHub release), so there is no
		// GitHub fallback for this channel.
		return []string{r2Base + "/canary/latest.json"}
	}
	return []string{
		r2Base + "/latest/latest.json",
		ghReleasesBase + "/latest/download/latest.json",
	}
}

// downloadPage is the human-facing releases page shown when self-update is
// unavailable (macOS) or the manifest omits its own link.
func downloadPage() string {
	if channel == "canary" {
		return ghReleasesBase // lists pre-releases too
	}
	return ghReleasesBase + "/latest"
}

// UpdateInfo is the CheckUpdate result that drives the frontend's update banner.
type UpdateInfo struct {
	Available     bool   `json:"available"`
	Current       string `json:"current"`
	Latest        string `json:"latest"`
	Notes         string `json:"notes"`
	CanSelfUpdate bool   `json:"canSelfUpdate"` // win/linux true; macOS false (unsigned → manual download)
	DownloadURL   string `json:"downloadUrl"`   // human-facing releases page (macOS path / fallback link)
	AssetSize     int64  `json:"assetSize"`     // running platform's artifact size, for the progress bar
	Err           string `json:"err,omitempty"` // set when the check itself failed (both endpoints down)
}

// updateProgress is the payload of the "updater:progress" Wails event emitted
// throughout ApplyUpdate.
type updateProgress struct {
	Phase    string `json:"phase"` // downloading | verifying | applying | done | error
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

// canSelfUpdate reports whether in-place update is possible. macOS is excluded:
// without a Developer ID signature + notarization, swapping the .app and relaunching
// trips Gatekeeper, so macOS falls back to a manual download.
func canSelfUpdate() bool { return runtime.GOOS != "darwin" }

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

// fetchManifest pulls latest.json from the primary endpoint, then the fallback,
// and decodes it.
func fetchManifest(ctx context.Context, c *http.Client) (*update.Manifest, error) {
	var lastErr error
	for _, url := range manifestEndpoints() {
		b, err := fetchBytes(ctx, c, url)
		if err != nil {
			lastErr = err
			continue
		}
		var m update.Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			lastErr = err
			continue
		}
		return &m, nil
	}
	return nil, fmt.Errorf("update: fetch manifest: %w", lastErr)
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
		CanSelfUpdate: canSelfUpdate(),
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
	}
	return info
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

// applyWindows writes the downloaded NSIS installer to a temp file and launches it.
// The per-user installer needs no admin rights and its finish page relaunches the
// app; the caller then exits so the installer can replace the running exe. The
// installer targets the running app's own directory (issue #3217) so an update
// overwrites in place instead of landing a second copy at the per-user default —
// this also covers upgrades from builds that predate the registry InstallLocation.
func applyWindows(installer []byte) error {
	f, err := os.CreateTemp("", "reasonix-update-*.exe")
	if err != nil {
		return err
	}
	name := f.Name()
	if _, err := f.Write(installer); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return installerCommand(name, currentInstallDir()).Start()
}

// currentInstallDir is the directory of the running executable — the location a
// Windows update must overwrite. Empty when it can't be resolved, in which case
// the installer falls back to its own InstallDir logic.
func currentInstallDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
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
