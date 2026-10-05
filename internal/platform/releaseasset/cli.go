// Package releaseasset downloads and verifies immutable Reasonix CLI release
// artifacts for a requested platform. It is used when a local Desktop or CLI
// needs to provision the remote `reasonix serve` binary without requiring
// Node/npm on the remote machine.
package releaseasset

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"reasonix/internal/safety/redirectguard"
)

const (
	cliReleaseBase       = "https://github.com/esengine/DeepSeek-Reasonix/releases/download"
	maxCLIArchiveBytes   = int64(256 << 20)
	maxCLIChecksumBytes  = int64(1 << 20)
	maxExtractedCLIBytes = int64(128 << 20)
)

var cliReleaseVersionPattern = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:preview|rc)\.(?:0|[1-9][0-9]*))?$`)

// Line is a release line's tag namespace: the CLI line tags a release v1.2.3,
// the Studio line studio-v1.2.3. A version alone cannot say which release
// holds an asset. Not update.Line, which is an install layout — one type
// answering both questions is how a tag namespace decides where a .deb goes.
type Line string

const (
	// CLILine tags a release with the bare version.
	CLILine Line = ""
	// StudioLine tags a release studio-<version>.
	StudioLine Line = "studio-"
)

// tag is the release tag this line publishes version under.
func (l Line) tag(version string) string { return string(l) + version }

// DownloadCLI fetches the official CLI release for line, version and target,
// verifies it against SHA256SUMS from that same immutable release, and returns
// the extracted executable. A non-empty cacheDir keeps the verified bytes, so
// the next machine on this platform costs no download.
func DownloadCLI(ctx context.Context, client *http.Client, cacheDir string, line Line, version, goos, goarch string) ([]byte, error) {
	return cachedDownloadFromBase(ctx, client, cliReleaseBase, cacheDir, line, version, goos, goarch, true)
}

// CLIDownload is what another machine needs to fetch its own kernel: where the
// archive is, and the digest to check it against. The digest is resolved here,
// over the redirect-guarded connection this machine already trusts — a host
// that fetched both the archive and its checksums itself would be verifying
// one thing against another the same network handed it.
type CLIDownload struct {
	URL        string // the release archive
	Asset      string // its file name, which is also what the digest names
	SHA256     string // lowercase hex, 64 chars
	Executable string // the binary inside the archive
}

// ResolveCLIDownload reads the release's SHA256SUMS and returns where the
// archive is and what it must hash to. It downloads no archive: the point is
// to let the machine that will run it do that over its own connection.
func ResolveCLIDownload(ctx context.Context, client *http.Client, line Line, version, goos, goarch string) (CLIDownload, error) {
	if err := supportedCLITarget(version, goos, goarch); err != nil {
		return CLIDownload{}, err
	}
	if client == nil {
		return CLIDownload{}, errors.New("remote CLI download requires an HTTP client")
	}
	assetName, executable := cliAsset(goos, goarch)
	releaseBase := strings.TrimRight(cliReleaseBase, "/") + "/" + url.PathEscape(line.tag(version)) + "/"

	guarded := *client
	guarded.CheckRedirect = redirectguard.Follow(officialHosts...)
	checksums, err := fetchBounded(ctx, &guarded, releaseBase+"SHA256SUMS", maxCLIChecksumBytes)
	if err != nil {
		return CLIDownload{}, fmt.Errorf("download SHA256SUMS: %w", err)
	}
	digest, err := digestFor(assetName, checksums)
	if err != nil {
		return CLIDownload{}, err
	}
	return CLIDownload{
		URL: releaseBase + assetName, Asset: assetName,
		SHA256: digest, Executable: executable,
	}, nil
}

// SupportsTarget reports whether a published release could be downloaded for
// this version and platform, without asking the network. A caller uses it to
// close a route up front rather than discovering it at the download.
func SupportsTarget(version, goos, goarch string) error {
	return supportedCLITarget(version, goos, goarch)
}

func supportedCLITarget(version, goos, goarch string) error {
	if !cliReleaseVersionPattern.MatchString(strings.TrimSpace(version)) {
		return fmt.Errorf("remote CLI download requires a released version, got %q", version)
	}
	if goos != "linux" && goos != "darwin" && goos != "windows" {
		return fmt.Errorf("remote CLI download does not support OS %q", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return fmt.Errorf("remote CLI download does not support architecture %q", goarch)
	}
	return nil
}

func downloadCLIFromBase(ctx context.Context, client *http.Client, base, tag, goos, goarch string, official bool) ([]byte, error) {
	if client == nil {
		return nil, errors.New("remote CLI download requires an HTTP client")
	}
	assetName, executable := cliAsset(goos, goarch)
	releaseBase := strings.TrimRight(base, "/") + "/" + url.PathEscape(tag) + "/"
	archiveURL := releaseBase + assetName
	checksumURL := releaseBase + "SHA256SUMS"

	copyOfClient := *client
	if official {
		copyOfClient.CheckRedirect = redirectguard.Follow(officialHosts...)
	}
	archive, err := fetchBounded(ctx, &copyOfClient, archiveURL, maxCLIArchiveBytes)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", assetName, err)
	}
	checksums, err := fetchBounded(ctx, &copyOfClient, checksumURL, maxCLIChecksumBytes)
	if err != nil {
		return nil, fmt.Errorf("download SHA256SUMS: %w", err)
	}
	if err := verifyChecksum(archive, assetName, checksums); err != nil {
		return nil, err
	}
	binary, err := extractCLI(archive, assetName, executable)
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", assetName, err)
	}
	return binary, nil
}

// cliAsset names the release artifact for a target and the executable inside
// it. Windows ships a zip and an .exe; every other platform a tar.gz — the
// release line has always been built this way, and assuming otherwise is what
// made a Windows remote undownloadable.
func cliAsset(goos, goarch string) (asset, executable string) {
	if goos == "windows" {
		return fmt.Sprintf("reasonix-%s-%s.zip", goos, goarch), "reasonix.exe"
	}
	return fmt.Sprintf("reasonix-%s-%s.tar.gz", goos, goarch), "reasonix"
}

func fetchBounded(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "reasonix-remote-bootstrap")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("asset exceeds %d-byte limit", limit)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("asset exceeds %d-byte limit", limit)
	}
	return data, nil
}

func verifyChecksum(data []byte, assetName string, checksums []byte) error {
	want, err := digestFor(assetName, checksums)
	if err != nil {
		return err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("SHA-256 mismatch for %s", assetName)
	}
	return nil
}

// digestFor reads one asset's expected digest out of a SHA256SUMS body.
func digestFor(assetName string, checksums []byte) (string, error) {
	want := ""
	for line := range strings.SplitSeq(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != assetName {
			continue
		}
		if want != "" {
			return "", fmt.Errorf("SHA256SUMS contains duplicate entries for %s", assetName)
		}
		want = strings.ToLower(fields[0])
	}
	if len(want) != sha256.Size*2 {
		return "", fmt.Errorf("SHA256SUMS has no valid entry for %s", assetName)
	}
	if _, err := hex.DecodeString(want); err != nil {
		return "", fmt.Errorf("SHA256SUMS has an invalid digest for %s", assetName)
	}
	return want, nil
}

func extractCLI(archive []byte, assetName, executable string) ([]byte, error) {
	if strings.HasSuffix(assetName, ".zip") {
		return extractCLIZip(archive, executable)
	}
	return extractCLITarGz(archive, executable)
}

// extractCLIZip reads the one executable out of a Windows release. Bounded the
// same way the tar path is: a declared size is the archive's claim, so the read
// is limited rather than trusted.
func extractCLIZip(archive []byte, executable string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	var binary []byte
	for _, entry := range zr.File {
		if path.Base(path.Clean(entry.Name)) != executable {
			continue
		}
		if entry.FileInfo().IsDir() || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > uint64(maxExtractedCLIBytes) {
			return nil, errors.New("reasonix archive entry is not a bounded regular file")
		}
		if binary != nil {
			return nil, errors.New("reasonix archive contains duplicate binaries")
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, err
		}
		binary, err = io.ReadAll(io.LimitReader(rc, maxExtractedCLIBytes+1))
		rc.Close()
		if err != nil {
			return nil, err
		}
		if int64(len(binary)) > maxExtractedCLIBytes {
			return nil, errors.New("reasonix archive entry exceeds the size limit")
		}
	}
	if binary == nil {
		return nil, errors.New("reasonix archive does not contain the executable")
	}
	return binary, nil
}

func extractCLITarGz(archive []byte, executable string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var binary []byte
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if path.Base(path.Clean(header.Name)) != executable {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxExtractedCLIBytes {
			return nil, errors.New("reasonix archive entry is not a bounded regular file")
		}
		if binary != nil {
			return nil, errors.New("reasonix archive contains duplicate binaries")
		}
		binary, err = io.ReadAll(io.LimitReader(tr, maxExtractedCLIBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(binary)) != header.Size {
			return nil, errors.New("reasonix archive entry size mismatch")
		}
	}
	if len(binary) == 0 {
		return nil, errors.New("reasonix binary not found in archive")
	}
	return binary, nil
}

// officialHosts is this path's own trust decision, not a shared list. That the
// CLI upgrade happens to trust the same names is a coincidence of where we
// publish; merging them would let one download's decision move the other's.
var officialHosts = []string{"github.com", ".githubusercontent.com"}
