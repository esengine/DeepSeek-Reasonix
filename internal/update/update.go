// Package update implements CLI self-update from GitHub Releases. It fetches the
// latest release metadata, compares semver, downloads the matching platform
// archive, verifies its SHA256 checksum, extracts the binary, and atomically
// replaces the running executable via github.com/minio/selfupdate.
//
// The release artifacts are produced by GoReleaser (see .goreleaser.yaml) with
// the naming convention:
//
//	reasonix-{os}-{arch}.tar.gz   (linux, darwin)
//	reasonix-{os}-{arch}.zip      (windows)
//
// accompanied by a SHA256SUMS file.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
	"golang.org/x/mod/semver"
)

const (
	// GitHubRepo is the canonical {owner}/{repo} for CLI releases.
	GitHubRepo = "esengine/DeepSeek-Reasonix"
	apiTimeout = 15 * time.Second
)

// Release is a minimal view of the GitHub Releases API response.
type Release struct {
	TagName    string    `json:"tag_name"`
	Prerelease bool      `json:"prerelease"`
	Assets     []Asset   `json:"assets"`
}

// Asset is one downloadable file attached to a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Info is the Check result shown to the user.
type Info struct {
	Current   string
	Latest    string
	Available bool
	Prerelease bool
	AssetName string
	Err       string
}

// Check fetches the latest release from GitHub and compares it to current.
func Check(ctx context.Context, c *http.Client, current string) Info {
	info := Info{Current: current}

	cur, ok := normalizeVersion(current)
	if !ok {
		info.Err = "current version is not a valid semver (dev build?)"
		return info
	}

	rel, err := fetchLatestRelease(ctx, c)
	if err != nil {
		info.Err = fmt.Sprintf("fetch latest release: %v", err)
		return info
	}

	latest, ok := normalizeVersion(rel.TagName)
	if !ok {
		info.Err = "latest release has no valid version tag"
		return info
	}

	info.Latest = rel.TagName
	info.Prerelease = rel.Prerelease
	info.AssetName = assetName()

	if semver.Compare(latest, cur) > 0 {
		info.Available = true
	}
	return info
}

// Apply downloads the latest release archive for the current platform, verifies
// its SHA256 checksum against the release's SHA256SUMS file, extracts the
// reasonix binary, and replaces the running executable.
func Apply(ctx context.Context, c *http.Client, onProgress func(phase string, received, total int64)) error {
	report := func(phase string, received, total int64) {
		if onProgress != nil {
			onProgress(phase, received, total)
		}
	}

	report("fetching release", 0, 0)
	rel, err := fetchLatestRelease(ctx, c)
	if err != nil {
		return fmt.Errorf("fetch release: %w", err)
	}

	wantAsset := assetName()
	var assetURL string
	for _, a := range rel.Assets {
		if a.Name == wantAsset {
			assetURL = a.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		return fmt.Errorf("asset %q not found in release %s", wantAsset, rel.TagName)
	}

	// Download SHA256SUMS.
	report("downloading checksums", 0, 0)
	sumsURL := sumsAssetURL(rel.Assets)
	if sumsURL == "" {
		return fmt.Errorf("SHA256SUMS not found in release %s", rel.TagName)
	}
	sumsData, err := fetchBytes(ctx, c, sumsURL)
	if err != nil {
		return fmt.Errorf("download SHA256SUMS: %w", err)
	}
	wantSHA, err := parseSHA256SUMS(sumsData, wantAsset)
	if err != nil {
		return err
	}

	// Download archive.
	var archiveData []byte
	if rel.Assets != nil {
		for _, a := range rel.Assets {
			if a.Name == wantAsset {
				archiveData, err = downloadWithProgress(ctx, c, a.BrowserDownloadURL, a.Size, report)
				break
			}
		}
	}
	if archiveData == nil {
		archiveData, err = downloadWithProgress(ctx, c, assetURL, 0, report)
	}
	if err != nil {
		return fmt.Errorf("download archive: %w", err)
	}

	// Verify SHA256.
	report("verifying", int64(len(archiveData)), int64(len(archiveData)))
	if err := checkSHA256(archiveData, wantSHA); err != nil {
		return err
	}

	// Extract binary.
	report("extracting", 0, 0)
	binName := binaryName()
	binData, err := extractBinary(archiveData, wantAsset, binName)
	if err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}

	// Apply.
	report("applying update", 0, 0)
	if err := selfupdate.Apply(bytes.NewReader(binData), selfupdate.Options{}); err != nil {
		return fmt.Errorf("apply update: %w", err)
	}

	report("done", 0, 0)
	return nil
}

// semverRe matches a vX.Y.Z semver segment, optionally followed by a prerelease
// suffix (-xxx) but NOT by a git-describe distance suffix (-N-gHASH). Anchored
// at a word boundary on the left so it doesn't match a trailing vX.Y.Z inside a
// longer token.
var semverRe = regexp.MustCompile(`(?:^|\b)(v?\d+\.\d+\.\d+(?:-[a-zA-Z0-9.]+)?)`)

// normalizeVersion extracts a semver string from v, handling git-describe output
// like "desktop-v1.0.0-105-gf3894d6f" by stripping the distance/hash suffix and
// any tag prefix before the vX.Y.Z portion. Returns ok=false for "dev", empty
// strings, or input that contains no parseable semver.
func normalizeVersion(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" || v == "dev" {
		return "", false
	}
	// Strip git-describe distance+hash suffix: -N-gXXXXXXX
	if i := strings.LastIndex(v, "-g"); i > 0 {
		rest := v[i+2:]
		if len(rest) >= 7 {
			allHex := true
			for _, c := range rest {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					allHex = false
					break
				}
			}
			if allHex {
				// Also strip the "-N" distance before "-g".
				head := v[:i]
				if j := strings.LastIndex(head, "-"); j > 0 {
					distance := head[j+1:]
					isNum := len(distance) > 0
					for _, c := range distance {
						if c < '0' || c > '9' {
							isNum = false
							break
						}
					}
					if isNum {
						v = head[:j]
					}
				}
			}
		}
	}
	// Extract the vX.Y.Z[-prerelease] segment.
	m := semverRe.FindString(v)
	if m == "" {
		return "", false
	}
	if !strings.HasPrefix(m, "v") {
		m = "v" + m
	}
	if !semver.IsValid(m) {
		return "", false
	}
	return m, true
}

// assetName returns the GoReleaser archive filename for the running platform.
func assetName() string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("reasonix-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

// binaryName returns the executable name for the running platform.
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "reasonix.exe"
	}
	return "reasonix"
}

// fetchLatestRelease queries the GitHub Releases API for the latest release.
func fetchLatestRelease(ctx context.Context, c *http.Client) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", GitHubRepo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// fetchBytes GETs a URL into memory.
func fetchBytes(ctx context.Context, c *http.Client, url string) ([]byte, error) {
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

// downloadWithProgress fetches url, calling onProgress as bytes arrive.
func downloadWithProgress(ctx context.Context, c *http.Client, url string, total int64, onProgress func(phase string, received, total int64)) ([]byte, error) {
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
	if resp.ContentLength > 0 {
		total = resp.ContentLength
	}

	var buf bytes.Buffer
	pr := &progressReader{r: resp.Body, total: total, onProgress: onProgress}
	if _, err := io.Copy(&buf, pr); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type progressReader struct {
	r          io.Reader
	received   int64
	total      int64
	lastEmit   int64
	onProgress func(phase string, received, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.received += int64(n)
	if p.onProgress != nil && (p.received-p.lastEmit >= 256<<10 || err == io.EOF) {
		p.lastEmit = p.received
		p.onProgress("downloading", p.received, p.total)
	}
	return n, err
}

// sumsAssetURL finds the BrowserDownloadURL for SHA256SUMS in the asset list.
func sumsAssetURL(assets []Asset) string {
	for _, a := range assets {
		if a.Name == "SHA256SUMS" {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// parseSHA256SUMS extracts the expected SHA256 hex digest for wantName from a
// GoReleaser SHA256SUMS file.
func parseSHA256SUMS(data []byte, wantName string) (string, error) {
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[1] == wantName {
			return strings.TrimSpace(parts[0]), nil
		}
	}
	return "", fmt.Errorf("checksum for %q not found in SHA256SUMS", wantName)
}

// checkSHA256 verifies data's digest matches the expected hex string.
func checkSHA256(data []byte, want string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("SHA256 mismatch: got %s, want %s", got, want)
	}
	return nil
}

// extractBinary pulls the named binary from an archive (.tar.gz or .zip).
func extractBinary(archive []byte, archiveName, binaryName string) ([]byte, error) {
	if strings.HasSuffix(archiveName, ".zip") {
		return extractFromZip(archive, binaryName)
	}
	return extractFromTarGz(archive, binaryName)
}

// extractFromTarGz extracts a named regular file from a .tar.gz blob.
func extractFromTarGz(targz []byte, name string) ([]byte, error) {
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
	return nil, fmt.Errorf("%q not found in archive", name)
}

// extractFromZip extracts a named file from a .zip blob.
func extractFromZip(data []byte, name string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range r.File {
		if f.Name == name || strings.HasSuffix(f.Name, "/"+name) {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%q not found in zip archive", name)
}
