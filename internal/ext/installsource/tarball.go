package installsource

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Installing a plugin must not depend on the user having git: GitHub serves the
// same tree as a tarball. Two deliberate differences from a clone — no symlink is
// unpacked, and the commit comes from the archive root instead of rev-parse.

// A tarball is a whole repository rather than one manifest, so it gets its own
// budget: generous enough for a real plugin, bounded against a mirror that
// streams forever or expands into a disk-filling bomb.
const (
	tarballFetchTimeout = 2 * time.Minute
	tarballFetchLimit   = 64 << 20
	tarballEntryLimit   = 16 << 20
	tarballTotalLimit   = 256 << 20
	tarballFileLimit    = 20_000
)

// GitHub names the archive root "{repo}-{sha}" for a branch and "{repo}-{tag}"
// for a tag, so only a full object name counts as the commit.
var tarballRootCommit = regexp.MustCompile(`-([0-9a-f]{40})$`)

// fetchGitHubTarball unpacks owner/repo at its branch (the default branch when
// empty) into dir, returning the commit the archive was cut from — empty when
// the archive root does not name one.
func (t *Tool) fetchGitHubTarball(ctx context.Context, src githubRepoSource, dir string) (string, error) {
	sourceURL := fmt.Sprintf("%s/repos/%s/%s/tarball", strings.TrimRight(githubAPIBaseURL, "/"), src.Owner, src.Repo)
	if src.Branch != "" {
		sourceURL += "/" + src.Branch
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, tarballFetchTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", newErr(ErrSourceUnreadable, "%s: %v", sourceURL, err)
	}
	req.Header.Set("User-Agent", "reasonix-install/1.0")
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", newErr(ErrSourceUnreadable, "%s: %v", sourceURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", &sourceHTTPError{sentinel: ErrAuthRequired, status: resp.StatusCode}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &sourceHTTPError{sentinel: ErrSourceUnreadable, status: resp.StatusCode}
	}
	root, err := unpackTarball(io.LimitReader(resp.Body, tarballFetchLimit), dir)
	if err != nil {
		return "", err
	}
	if m := tarballRootCommit.FindStringSubmatch(root); m != nil {
		return m[1], nil
	}
	return "", nil
}

// unpackTarball writes a gzipped tar into dir, dropping the single root
// directory every entry shares so the result matches what a clone leaves
// behind. It returns that root's name for the caller to read the commit from.
func unpackTarball(r io.Reader, dir string) (string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return "", newErr(ErrSourceUnreadable, "tarball is not gzip data: %v", err)
	}
	defer gz.Close()

	reader := tar.NewReader(gz)
	root := ""
	files := 0
	var total int64
	for {
		head, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", newErr(ErrSourceUnreadable, "read tarball: %v", err)
		}
		// Anything that is not a plain file or directory is skipped rather than
		// recreated: symlinks, devices, and the pax_global_header GitHub puts
		// first — which must not be mistaken for the archive root below.
		if head.Typeflag != tar.TypeReg && head.Typeflag != tar.TypeDir {
			continue
		}
		name := path.Clean(head.Name)
		if root == "" {
			root, _, _ = strings.Cut(name, "/")
		}
		rest, inRoot := strings.CutPrefix(name, root+"/")
		if !inRoot {
			if name == root {
				continue
			}
			return "", &sourceBoundaryError{code: sourceEscape}
		}
		rel := filepath.FromSlash(rest)
		if !filepath.IsLocal(rel) {
			return "", &sourceBoundaryError{code: sourceEscape}
		}
		dest := filepath.Join(dir, rel)
		if head.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return "", newErr(ErrSourceUnreadable, "create %s: %v", rel, err)
			}
			continue
		}
		files++
		if files > tarballFileLimit {
			return "", &sourceBoundaryError{code: sourceFileCount, limit: tarballFileLimit}
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", newErr(ErrSourceUnreadable, "create %s: %v", filepath.Dir(rel), err)
		}
		written, err := writeTarEntry(dest, reader, head.Mode)
		if err != nil {
			return "", err
		}
		total += written
		if total > tarballTotalLimit {
			return "", &sourceBoundaryError{code: sourceTotalSize, limit: tarballTotalLimit}
		}
	}
	if root == "" {
		return "", newErr(ErrSourceUnreadable, "tarball is empty")
	}
	return root, nil
}

// writeTarEntry copies one file, bounded by the entry limit rather than by the
// header's size field — a header may understate what the stream then delivers.
// Only the owner execute bit survives, so a hook script stays runnable without
// the archive being able to hand out setuid or group-writable modes.
func writeTarEntry(dest string, r io.Reader, mode int64) (int64, error) {
	perm := os.FileMode(0o644)
	if mode&0o100 != 0 {
		perm = 0o755
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return 0, newErr(ErrSourceUnreadable, "write %s: %v", filepath.Base(dest), err)
	}
	defer f.Close()
	written, err := io.Copy(f, io.LimitReader(r, tarballEntryLimit+1))
	if err != nil {
		return written, newErr(ErrSourceUnreadable, "write %s: %v", filepath.Base(dest), err)
	}
	if written > tarballEntryLimit {
		return written, &sourceBoundaryError{code: sourceEntrySize, limit: tarballEntryLimit}
	}
	return written, nil
}
