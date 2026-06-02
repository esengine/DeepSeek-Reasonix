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
