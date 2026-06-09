package codegraph

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// Version is the pinned CodeGraph release fetched on first use. Keep in sync
	// with CODEGRAPH_VERSION in the Makefile and .github/workflows.
	Version = "v0.9.7"
	cgRepo  = "colbymchenry/codegraph"

	officialMirrorBase         = "https://pub-147fb53b9c1e4bbf891a257968619ea7.r2.dev/codegraph"
	officialMainlandMirrorBase = ""
	perSourceDownloadTimeout   = 15 * time.Second

	renameAttempts = 5
	renameBackoff  = 200 * time.Millisecond
)

// CacheDir is where the CodeGraph bundle is unpacked on first use:
// <user cache>/reasonix/codegraph/<Version>. Versioned so a bump installs cleanly
// beside the old one. REASONIX_CACHE_DIR overrides the base (relocate the cache,
// or isolate it in tests). Empty when no cache/config dir resolves.
func CacheDir() string {
	base := os.Getenv("REASONIX_CACHE_DIR")
	if base == "" {
		var err error
		if base, err = os.UserCacheDir(); err != nil {
			if base, err = os.UserConfigDir(); err != nil {
				return ""
			}
		}
		base = filepath.Join(base, "reasonix")
	}
	return filepath.Join(base, "codegraph", Version)
}

// cached returns the launcher path inside CacheDir when the bundle is present.
func cached() (string, bool) {
	dir := CacheDir()
	if dir == "" {
		return "", false
	}
	for _, rel := range launcherNames() {
		if p := filepath.Join(dir, rel); isExec(p) {
			return p, true
		}
	}
	return "", false
}

// assetName is CodeGraph's release asset for the running platform (it names them
// codegraph-<darwin|linux|win32>-<x64|arm64>.<tar.gz|zip>).
func assetName() string {
	osName := map[string]string{"darwin": "darwin", "linux": "linux", "windows": "win32"}[runtime.GOOS]
	if osName == "" {
		osName = runtime.GOOS
	}
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	if arch == "" {
		arch = runtime.GOARCH
	}
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("codegraph-%s-%s.%s", osName, arch, ext)
}

// Install downloads and unpacks the CodeGraph bundle into CacheDir on first use,
// verifying it against the checksum baked into the reasonix binary, then returns
// the launcher path.
// It is idempotent: a present cache is returned untouched. log, if non-nil,
// receives a couple of progress lines. The extraction is staged in a temp dir and
// atomically renamed into place, so a cancelled or failed run leaves no partial
// install behind.
func Install(ctx context.Context, log func(string)) (string, error) {
	return InstallWithClient(ctx, http.DefaultClient, log)
}

// InstallWithClient is Install with an explicit HTTP client, used when Reasonix
// network proxy settings should apply.
func InstallWithClient(ctx context.Context, client *http.Client, log func(string)) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if p, ok := cached(); ok {
		return p, nil
	}
	dir := CacheDir()
	if dir == "" {
		return "", fmt.Errorf("codegraph: no cache directory available")
	}
	asset := assetName()
	logf(log, "codegraph: downloading %s (%s, one-time)…", asset, Version)

	data, err := downloadAsset(ctx, client, asset, log)
	if err != nil {
		return "", err
	}

	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(parent, ".dl-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	if strings.HasSuffix(asset, ".zip") {
		err = extractZip(data, tmp)
	} else {
		err = extractTarGz(data, tmp)
	}
	if err != nil {
		return "", fmt.Errorf("codegraph: extract: %w", err)
	}
	// The archive holds a single top-level dir (codegraph-<target>/). Promote it
	// to the versioned cache dir so the launcher lands at <dir>/bin/codegraph.
	root, err := singleChild(tmp)
	if err != nil {
		return "", err
	}
	if p, ok := cached(); ok {
		return p, nil // a concurrent session already populated dir
	}
	if err := promote(root, dir); err != nil {
		if p, ok := cached(); ok {
			return p, nil // a concurrent winner landed during our retries
		}
		return "", fmt.Errorf("codegraph: install to %s failed: %w — the cache directory may be read-only or locked by antivirus; set REASONIX_CACHE_DIR to a writable location to relocate it", dir, err)
	}
	p, ok := cached()
	if !ok {
		return "", fmt.Errorf("codegraph: launcher not found after install (unexpected bundle layout)")
	}
	logf(log, "codegraph: installed to %s", dir)
	return p, nil
}

func downloadAsset(ctx context.Context, client *http.Client, asset string, log func(string)) ([]byte, error) {
	want := expectedAssetSHA256(asset)
	if want == "" {
		return nil, fmt.Errorf("codegraph: no embedded checksum for %s (%s)", asset, Version)
	}
	return downloadAssetFromBases(ctx, client, asset, want, downloadBases(), log)
}

func downloadAssetFromBases(ctx context.Context, client *http.Client, asset, want string, bases []string, log func(string)) ([]byte, error) {
	var errs []string
	for _, base := range bases {
		url := strings.TrimRight(base, "/") + "/" + asset
		getCtx := ctx
		cancel := func() {}
		if perSourceDownloadTimeout > 0 {
			getCtx, cancel = context.WithTimeout(ctx, perSourceDownloadTimeout)
		}
		data, err := httpGet(getCtx, client, url)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", base, err))
			continue
		}
		got := fmt.Sprintf("%x", sha256.Sum256(data))
		if got != want {
			errs = append(errs, fmt.Sprintf("%s: checksum mismatch for %s", base, asset))
			continue
		}
		logf(log, "codegraph: downloaded from %s", base)
		return data, nil
	}
	return nil, fmt.Errorf("codegraph: download %s failed (%s)", asset, strings.Join(errs, "; "))
}

func expectedAssetSHA256(asset string) string {
	return releaseAssetSHA256[asset]
}

func downloadBases() []string {
	bases := []string{officialMirrorBase + "/" + Version}
	if strings.TrimSpace(officialMainlandMirrorBase) != "" {
		bases = append(bases, strings.TrimRight(officialMainlandMirrorBase, "/")+"/"+Version)
	}
	bases = append(bases, fmt.Sprintf("https://github.com/%s/releases/download/%s", cgRepo, Version))
	return dedupeStrings(bases)
}

func dedupeStrings(values []string) []string {
	var out []string
	seen := make(map[string]bool, len(values))
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// promote moves the freshly extracted bundle (root) into its versioned home
// (dir). On Windows os.Rename onto an existing directory fails with "Access is
// denied", so a stale/partial dest from an earlier interrupted install (cached()
// already reported it incomplete) is cleared first; a just-extracted .exe can
// also stay briefly locked by an AV scanner, so the move is retried.
func promote(root, dir string) error {
	_ = os.RemoveAll(dir)
	var err error
	for i := 0; i < renameAttempts; i++ {
		if err = os.Rename(root, dir); err == nil {
			return nil
		}
		time.Sleep(renameBackoff)
	}
	return err
}

func httpGet(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// sha256For returns the hex checksum recorded for name in a SHA256SUMS body
// (lines of "<hex>  <name>").
func sha256For(sums, name string) (string, error) {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == name {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("codegraph: %s not listed in SHA256SUMS", name)
}

// resolveWithin returns the real path to write archive entry name under root
// (parents created), rejecting escapes. EvalSymlinks on the parent also catches
// the symlink-redirect variant a lexical "../" check misses: a parent component
// an earlier entry turned into a symlink, written *through* to land outside root.
func resolveWithin(root, name string) (string, error) {
	target := filepath.Join(root, name)
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path %q in archive", name)
	}
	if target == root {
		return root, nil
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}
	if realParent != root && !strings.HasPrefix(realParent, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path %q in archive: escapes via symlink", name)
	}
	return filepath.Join(realParent, filepath.Base(target)), nil
}

// symlinkWithin rejects a symlink whose target escapes root. linkPath is the
// symlink's already-resolved real location (from resolveWithin), so a relative
// target is judged from where the link truly lands, not its lexical archive path.
func symlinkWithin(root, linkPath, linkname string) error {
	dest := linkname
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(linkPath), linkname)
	}
	dest = filepath.Clean(dest)
	if dest != root && !strings.HasPrefix(dest, root+string(os.PathSeparator)) {
		return fmt.Errorf("unsafe symlink %q -> %q in archive", linkPath, linkname)
	}
	return nil
}

func extractTarGz(data []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := resolveWithin(root, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := symlinkWithin(root, target, hdr.Linkname); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFileFromReader(target, tr, hdr.FileInfo().Mode()); err != nil {
				return err
			}
		}
	}
}

func extractZip(data []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		target, err := resolveWithin(root, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFileFromReader(target, rc, f.Mode())
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeFileFromReader(target string, r io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// singleChild returns the sole entry in dir (the archive's top-level
// codegraph-<target>/ directory).
func singleChild(dir string) (string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(ents) != 1 || !ents[0].IsDir() {
		return "", fmt.Errorf("codegraph: expected one top-level dir in archive, got %d entries", len(ents))
	}
	return filepath.Join(dir, ents[0].Name()), nil
}

func logf(log func(string), format string, a ...any) {
	if log != nil {
		log(fmt.Sprintf(format, a...))
	}
}
