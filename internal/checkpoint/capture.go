package checkpoint

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	fileenc "reasonix/internal/fileutil/encoding"
)

// Fingerprint is the identity of a path as observed on disk.
type Fingerprint struct {
	AbsPath      string
	Existed      bool
	IsDir        bool
	IsSymlink    bool
	Nlink        uint64
	Mode         uint32
	Size         int64
	SHA256       string
	EncodingKind fileenc.Kind
	// Content is populated only when requested (capture preimage).
	Content []byte
}

// CaptureOptions controls how CapturePath reads a file.
type CaptureOptions struct {
	// MaxBytes rejects files larger than this (0 = DefaultMaxFileBytes).
	MaxBytes int64
	// ReadContent loads file bytes into Fingerprint.Content.
	ReadContent bool
	// WorkspaceRoot rejects paths that escape it when non-empty.
	WorkspaceRoot string
}

// CapturePath Lstats path and optionally reads content. Coverage gaps are
// returned for symlink, hardlink, unreadable, oversized, and outside-workspace.
func CapturePath(path string, opts CaptureOptions) (Fingerprint, *CoverageGap, error) {
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxFileBytes
	}

	abs := path
	if !filepath.IsAbs(abs) {
		if opts.WorkspaceRoot == "" {
			return Fingerprint{}, nil, fmt.Errorf("relative path without workspace root")
		}
		abs = filepath.Join(opts.WorkspaceRoot, path)
	}
	abs = filepath.Clean(abs)

	if opts.WorkspaceRoot != "" {
		if _, err := safePath(opts.WorkspaceRoot, abs); err != nil {
			reason := GapOutsideWorkspace
			if errors.Is(err, errSymlinkPath) {
				reason = GapSymlink
			}
			return Fingerprint{}, &CoverageGap{
				Reason: reason,
				Detail: err.Error(),
				Path:   path,
			}, err
		}
	}

	f, err := secureOpenWorkspaceFile(opts.WorkspaceRoot, abs)
	if err != nil {
		if os.IsNotExist(err) {
			return Fingerprint{AbsPath: abs, Existed: false}, nil, nil
		}
		return Fingerprint{}, &CoverageGap{
			Reason: GapUnreadable,
			Detail: err.Error(),
			Path:   path,
		}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Fingerprint{}, &CoverageGap{Reason: GapUnreadable, Detail: err.Error(), Path: path}, err
	}

	fp := Fingerprint{
		AbsPath: abs,
		Existed: true,
		IsDir:   fi.IsDir(),
		Mode:    uint32(fi.Mode().Perm()),
		Size:    fi.Size(),
	}
	if nlink := fileNlink(fi); nlink > 1 {
		fp.Nlink = nlink
		return fp, &CoverageGap{
			Reason: GapHardlink,
			Detail: fmt.Sprintf("hard link nlink=%d", nlink),
			Path:   path,
		}, fmt.Errorf("hardlink not supported: %s", abs)
	}
	if fi.IsDir() {
		return fp, &CoverageGap{
			Reason: GapCaptureFailed,
			Detail: "path is a directory",
			Path:   path,
		}, fmt.Errorf("path is a directory: %s", abs)
	}
	if !opts.ReadContent {
		return fp, nil, nil
	}
	if fi.Size() > maxBytes {
		return fp, &CoverageGap{
			Reason: GapOversized,
			Detail: fmt.Sprintf("size %d exceeds limit %d", fi.Size(), maxBytes),
			Path:   path,
		}, fmt.Errorf("file too large: %s", abs)
	}

	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return fp, &CoverageGap{
			Reason: GapUnreadable,
			Detail: err.Error(),
			Path:   path,
		}, err
	}
	if int64(len(data)) > maxBytes {
		return fp, &CoverageGap{
			Reason: GapOversized,
			Detail: fmt.Sprintf("size exceeds limit %d", maxBytes),
			Path:   path,
		}, fmt.Errorf("file too large: %s", abs)
	}
	fp.Content = data
	fp.SHA256 = Digest(data)
	enc, _ := fileenc.Detect(data)
	fp.EncodingKind = enc
	return fp, nil, nil
}

// FingerprintPath is a lightweight identity check (no content) for conflict prechecks.
func FingerprintPath(root, path string) (Fingerprint, error) {
	fp, gap, err := CapturePath(path, CaptureOptions{
		WorkspaceRoot: root,
		ReadContent:   true, // need SHA for conflict detection
	})
	if err != nil && gap == nil {
		return fp, err
	}
	// Treat gaps as errors for fingerprinting during precheck — caller maps them.
	if gap != nil {
		return fp, fmt.Errorf("%s: %s", gap.Reason, gap.Detail)
	}
	return fp, nil
}

// CompareIdentity checks whether current disk state still matches the last
// Reasonix-owned after fingerprint. empty afterSHA with afterExisted==nil means
// no ownership tracking (legacy) — callers should treat as unverified.
func CompareIdentity(current Fingerprint, afterSHA string, afterExisted *bool, afterMode uint32) (conflict string) {
	if afterExisted == nil && afterSHA == "" {
		return ConflictCoverageLegacy
	}
	wantExist := false
	if afterExisted != nil {
		wantExist = *afterExisted
	} else if afterSHA != "" {
		wantExist = true
	}
	if current.Existed != wantExist {
		if !wantExist && current.Existed {
			return ConflictDeletedRecreate
		}
		return ConflictExternalChange
	}
	if !current.Existed {
		return ""
	}
	if afterMode != 0 && current.Mode != 0 && current.Mode != afterMode {
		// Permission-only changes are conflicts per the plan.
		return ConflictModeChange
	}
	if afterSHA != "" && current.SHA256 != afterSHA {
		return ConflictManualEdit
	}
	return ""
}

// MatchesRestoreImage reports that current disk already equals the before-image.
func MatchesRestoreImage(current Fingerprint, restoreSHA string, restoreExisted bool) bool {
	if !restoreExisted {
		return !current.Existed
	}
	return current.Existed && restoreSHA != "" && current.SHA256 == restoreSHA
}

// NormalizeRelPath returns a slash-cleaned workspace-relative path when possible.
func NormalizeRelPath(root, path string) string {
	if root == "" {
		return filepath.Clean(path)
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, path)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(filepath.Clean(root), abs)
	if err != nil || !filepath.IsLocal(rel) {
		return filepath.Clean(path)
	}
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(rel, "\\", "/")
	}
	return rel
}
