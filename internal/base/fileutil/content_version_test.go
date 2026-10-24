package fileutil

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestContentVersionTracksBytesKindAndAbsence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	absent, err := ReadContentVersion(t.Context(), path)
	if err != nil || absent.Kind != "absent" {
		t.Fatalf("absent: %+v %v", absent, err)
	}
	var versions []ContentVersion
	for _, body := range []string{"before", "after!", "before"} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		v, err := ReadContentVersion(t.Context(), path)
		if err != nil || v.Kind != "file" || v.Digest == "" {
			t.Fatalf("file: %+v %v", v, err)
		}
		versions = append(versions, v)
	}
	if versions[0] == versions[1] || versions[0] != versions[2] {
		t.Fatalf("content identity depends on metadata or misses same-size changes: %+v", versions)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := ReadContentVersion(t.Context(), path)
	if err != nil || dir.Kind != "directory" || dir.Digest != "" {
		t.Fatalf("directory: %+v %v", dir, err)
	}
}

func TestContentVersionCancellationPrecedesFilesystemAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ReadContentVersion(ctx, filepath.Join(t.TempDir(), "missing")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
}

func TestContentVersionHashesSymlinkTextWithoutReadingTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink("missing-target", path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	first, err := ReadContentVersion(t.Context(), path)
	if err != nil || first.Kind != "symlink" || first.Digest == "" {
		t.Fatalf("link: %+v %v", first, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("another-missing-target", path); err != nil {
		t.Fatal(err)
	}
	second, err := ReadContentVersion(t.Context(), path)
	if err != nil || second == first {
		t.Fatalf("retargeted link: %+v %v", second, err)
	}
}
