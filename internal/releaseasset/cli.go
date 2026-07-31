// Package releaseasset downloads and verifies immutable Reasonix CLI release
// artifacts for a requested platform. It is used when a local Desktop needs to
// provision the matching Workbench host binary without requiring Node/npm on
// the remote machine.
package releaseasset

import (
	"archive/tar"
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
)

const (
	cliReleaseBase       = "https://github.com/esengine/DeepSeek-Reasonix/releases/download"
	maxCLIArchiveBytes   = int64(256 << 20)
	maxCLIChecksumBytes  = int64(1 << 20)
	maxExtractedCLIBytes = int64(128 << 20)
)

var cliReleaseVersionPattern = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:preview|rc)\.(?:0|[1-9][0-9]*))?$`)

// DownloadCLI downloads the exact official CLI release for version and target,
// verifies it against SHA256SUMS from the same immutable release, and returns
// the extracted executable bytes. V1 Remote Workbench supports Unix hosts.
func DownloadCLI(ctx context.Context, client *http.Client, version, goos, goarch string) ([]byte, error) {
	if !cliReleaseVersionPattern.MatchString(strings.TrimSpace(version)) {
		return nil, fmt.Errorf("remote CLI download requires a released version, got %q", version)
	}
	if goos != "linux" && goos != "darwin" {
		return nil, fmt.Errorf("remote CLI download does not support OS %q", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return nil, fmt.Errorf("remote CLI download does not support architecture %q", goarch)
	}
	return downloadCLIFromBase(ctx, client, cliReleaseBase, version, goos, goarch, true)
}

func downloadCLIFromBase(ctx context.Context, client *http.Client, base, version, goos, goarch string, official bool) ([]byte, error) {
	if client == nil {
		return nil, errors.New("remote CLI download requires an HTTP client")
	}
	assetName := fmt.Sprintf("reasonix-%s-%s.tar.gz", goos, goarch)
	releaseBase := strings.TrimRight(base, "/") + "/" + url.PathEscape(version) + "/"
	archiveURL := releaseBase + assetName
	checksumURL := releaseBase + "SHA256SUMS"

	copyOfClient := *client
	if official {
		copyOfClient.CheckRedirect = validateOfficialRedirect
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
	binary, err := extractCLI(archive)
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", assetName, err)
	}
	return binary, nil
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
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != assetName {
			continue
		}
		if want != "" {
			return fmt.Errorf("SHA256SUMS contains duplicate entries for %s", assetName)
		}
		want = strings.ToLower(fields[0])
	}
	if len(want) != sha256.Size*2 {
		return fmt.Errorf("SHA256SUMS has no valid entry for %s", assetName)
	}
	if _, err := hex.DecodeString(want); err != nil {
		return fmt.Errorf("SHA256SUMS has an invalid digest for %s", assetName)
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("SHA-256 mismatch for %s", assetName)
	}
	return nil
}

func extractCLI(archive []byte) ([]byte, error) {
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
		if path.Base(path.Clean(header.Name)) != "reasonix" {
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

func validateOfficialRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("remote CLI download stopped after 10 redirects")
	}
	if req == nil || req.URL == nil || !strings.EqualFold(req.URL.Scheme, "https") || req.URL.User != nil || req.URL.Port() != "" {
		return errors.New("remote CLI download refused an unsafe redirect")
	}
	host := strings.ToLower(strings.TrimSuffix(req.URL.Hostname(), "."))
	if host != "github.com" && !strings.HasSuffix(host, ".githubusercontent.com") {
		return fmt.Errorf("remote CLI download refused redirect host %q", req.URL.Host)
	}
	return nil
}
