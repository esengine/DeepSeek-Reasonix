package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/fileutil"
)

func TestContentSnapshotRetainsV1StreamingWalkDigest(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"!file", "-leading", ".hidden", "a/child", "a-sibling", "a.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	h := sha256.New()
	encoder := json.NewEncoder(h)
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		version, err := fileutil.ReadContentVersion(t.Context(), path)
		if err != nil {
			return err
		}
		return encoder.Encode(struct {
			Path    string
			Version fileutil.ContentVersion
		}{filepath.ToSlash(rel), version})
	})
	if err != nil {
		t.Fatal(err)
	}
	actual := CaptureContentSnapshot(t.Context(), root, 100)
	if !actual.Complete || actual.Digest != hex.EncodeToString(h.Sum(nil)) {
		t.Fatalf("v1 content digest changed without changed inputs: %+v", actual)
	}
}

func TestContentSnapshotBindsBytesDespiteUnchangedMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "dependency.lock")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	before := CaptureContentSnapshot(t.Context(), root, 100)
	if err := os.WriteFile(path, []byte("after!"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	after := CaptureContentSnapshot(t.Context(), root, 100)
	if !before.Complete || !after.Complete || before.Digest == after.Digest {
		t.Fatalf("same-size same-time dependency change was lost: %+v %+v", before, after)
	}
	if repeated := CaptureContentSnapshot(t.Context(), root, 100); repeated != after {
		t.Fatalf("snapshot is not deterministic: %+v %+v", after, repeated)
	}
}

func TestContentSnapshotIncludesIgnoredAndNewInputs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("deps/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := CaptureContentSnapshot(t.Context(), root, 100)
	if err := os.MkdirAll(filepath.Join(root, "deps"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deps", "library"), []byte("version"), 0600); err != nil {
		t.Fatal(err)
	}
	after := CaptureContentSnapshot(t.Context(), root, 100)
	if !after.Complete || before.Digest == after.Digest {
		t.Fatalf("ignored dependency missing from snapshot: %+v %+v", before, after)
	}
}

func TestContentSnapshotNeverHashesAnIncompleteWalkAsComplete(t *testing.T) {
	for _, condition := range []string{"cancelled", "limit", "nested", "ambiguous_git", "external_link", "missing"} {
		t.Run(condition, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			limit := 100
			switch condition {
			case "cancelled":
				cancel()
			case "limit":
				limit = 0
			case "nested":
				if err := os.MkdirAll(filepath.Join(root, "nested", ".git"), 0700); err != nil {
					t.Fatal(err)
				}
			case "ambiguous_git":
				if err := os.WriteFile(filepath.Join(root, ".GIT"), []byte("ordinary input"), 0600); err != nil {
					t.Fatal(err)
				}
			case "external_link":
				if err := os.Symlink(t.TempDir(), filepath.Join(root, "external")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			case "missing":
				root = filepath.Join(root, "absent")
			}
			result := CaptureContentSnapshot(ctx, root, limit)
			if result.Complete || result.Digest != "" || result.Reason == "" {
				t.Fatalf("incomplete inputs accepted: %+v", result)
			}
		})
	}
}

func TestContentSnapshotDeclaresGitMetadataAsASeparateScope(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	before := CaptureContentSnapshot(t.Context(), root, 100)
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	after := CaptureContentSnapshot(t.Context(), root, 100)
	if !after.Complete || before.Digest != after.Digest || after.Scope != "workspace-content-v1" {
		t.Fatalf("workspace content scope includes Git metadata: %+v %+v", before, after)
	}
}
