package codegraph

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarGzWithSymlink builds a one-entry tar.gz holding a symlink named link -> dest.
func tarGzWithSymlink(link, dest string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: link, Typeflag: tar.TypeSymlink, Linkname: dest, Mode: 0o777})
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// TestExtractRejectsEscapingSymlink proves a symlink pointing outside the
// extraction dir is refused — otherwise a later entry written through it escapes
// (tar-slip via symlink), letting a malicious third-party release plant files
// outside the cache. Relative escapes resolve the same on every host.
func TestExtractRejectsEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	for _, dest := range []string{"../escape", "../../etc/cron.d", "sub/../../escape"} {
		err := extractTarGz(tarGzWithSymlink("evil", dest), dir)
		if err == nil || !strings.Contains(err.Error(), "unsafe symlink") {
			t.Errorf("symlink -> %q should be rejected, got %v", dest, err)
		}
	}
}

// tarGz builds a tar.gz from the given headers in order, each with no body.
func tarGz(hdrs ...*tar.Header) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range hdrs {
		_ = tw.WriteHeader(h)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// TestExtractRejectsSymlinkRedirectEscape covers the variant a lexical path check
// misses: an in-bounds symlink ("real/up" -> "..", which lands on root) is made a
// parent of a second symlink, so the second link's real location is root/escape
// even though its archive path is real/up/escape. Resolving the parent before
// validating the target is what catches it (go/unsafe-unzip-symlink).
func TestExtractRejectsSymlinkRedirectEscape(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("probe-target", filepath.Join(dir, "probe-link")); err != nil {
		t.Skipf("symlink creation is not available in this environment: %v", err)
	}
	_ = os.Remove(filepath.Join(dir, "probe-link"))
	data := tarGz(
		&tar.Header{Name: "real/", Typeflag: tar.TypeDir, Mode: 0o755},
		&tar.Header{Name: "real/up", Typeflag: tar.TypeSymlink, Linkname: "..", Mode: 0o777},
		&tar.Header{Name: "real/up/escape", Typeflag: tar.TypeSymlink, Linkname: "..", Mode: 0o777},
	)
	err := extractTarGz(data, dir)
	if err == nil || !strings.Contains(err.Error(), "unsafe symlink") {
		t.Fatalf("symlink-redirect escape should be rejected, got %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(dir)); err == nil {
		if _, err := os.Lstat(filepath.Join(filepath.Dir(dir), "escape")); err == nil {
			t.Fatal("escape symlink was planted outside the extraction dir")
		}
	}
}

// TestExtractAllowsInternalSymlink keeps legitimate in-bundle symlinks working.
func TestExtractAllowsInternalSymlink(t *testing.T) {
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe-link")
	if err := os.Symlink("probe-target", probe); err != nil {
		t.Skipf("symlink creation is not available in this environment: %v", err)
	}
	_ = os.Remove(probe)
	for _, dest := range []string{"bin/codegraph", "./node", "sub/dir/../tool"} {
		if err := extractTarGz(tarGzWithSymlink("link", dest), dir); err != nil {
			t.Errorf("internal symlink -> %q should be allowed, got %v", dest, err)
		}
	}
}
