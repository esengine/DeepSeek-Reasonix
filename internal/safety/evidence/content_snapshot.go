package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/fileutil"
)

// ContentSnapshot binds workspace bytes, types and modes, including ignored files.
// Its explicit scope excludes top-level Git metadata and external environment;
// completeness of this scope alone does not establish verification validity.
type ContentSnapshot struct {
	Scope    string `json:"scope"`
	Digest   string `json:"digest"`
	Entries  int    `json:"entries"`
	Complete bool   `json:"complete"`
	Reason   string `json:"reason"`
}

// CaptureContentSnapshot requires its caller to coordinate cooperative writers.
// A bound, unreadable input, external link or nested repository yields no digest.
func CaptureContentSnapshot(ctx context.Context, root string, limit int) ContentSnapshot {
	result, versions := captureContentVersions(ctx, root, limit)
	if result.Reason != "" {
		return result
	}
	return digestContentVersions(result, versions)
}

func captureContentVersions(ctx context.Context, root string, limit int) (ContentSnapshot, map[string]fileutil.ContentVersion) {
	result := ContentSnapshot{Scope: "workspace-content-v1"}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		result.Reason = "unreadable_root"
		return result, nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		result.Reason = "invalid_root"
		return result, nil
	}
	versions := map[string]fileutil.ContentVersion{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Reason = "unreadable_input"
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			result.Reason = "cancelled"
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if strings.EqualFold(entry.Name(), ".git") && rel != "." {
			if entry.Name() != ".git" {
				result.Reason = "ambiguous_git_metadata"
				return fs.SkipAll
			}
			if filepath.Dir(rel) != "." {
				result.Reason = "nested_repository"
				return fs.SkipAll
			}
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if result.Entries >= limit {
			result.Reason = "entry_limit"
			return fs.SkipAll
		}
		version, err := fileutil.ReadContentVersion(ctx, path)
		if err != nil {
			result.Reason = "unstable_or_unreadable_input"
			return err
		}
		if version.Kind == "absent" {
			result.Reason = "input_disappeared"
			return fs.SkipAll
		}
		if version.Kind == "symlink" && !snapshotLinkInside(root, path) {
			result.Reason = "unresolved_or_external_link"
			return fs.SkipAll
		}
		result.Entries++
		versions[filepath.ToSlash(rel)] = version
		return nil
	})
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil || result.Reason != "" {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			result.Reason = "cancelled"
		} else if result.Reason == "" {
			result.Reason = "observation_failed"
		}
		return result, nil
	}
	return result, versions
}

func digestContentVersions(result ContentSnapshot, versions map[string]fileutil.ContentVersion) ContentSnapshot {
	paths := make([]string, 0, len(versions))
	for path := range versions {
		paths = append(paths, path)
	}
	// Preserve WalkDir's component order, including a directory's descendants
	// before its next sibling. This keeps existing v1 digests byte-compatible.
	slices.SortFunc(paths, func(a, b string) int {
		if a == b {
			return 0
		}
		if a == "." {
			return -1
		}
		if b == "." {
			return 1
		}
		return slices.Compare(strings.Split(a, "/"), strings.Split(b, "/"))
	})
	h := sha256.New()
	encode := json.NewEncoder(h)
	for _, path := range paths {
		_ = encode.Encode(struct {
			Path    string
			Version fileutil.ContentVersion
		}{path, versions[path]})
	}
	result.Complete, result.Digest, result.Entries = true, hex.EncodeToString(h.Sum(nil)), len(paths)
	return result
}

func snapshotLinkInside(root, path string) bool {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, target)
	return err == nil && filepath.IsLocal(rel)
}
