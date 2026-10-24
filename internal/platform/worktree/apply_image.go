package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/platform/gitcmd"
)

type fileImage struct {
	Kind string      `json:"kind"`
	Mode os.FileMode `json:"mode"`
	Data []byte      `json:"data"`
}

func (f fileImage) equal(other fileImage) bool {
	return f.Kind == other.Kind && imageMode(f.Mode) == imageMode(other.Mode) && bytes.Equal(f.Data, other.Data)
}

func imageMode(mode os.FileMode) os.FileMode {
	if runtime.GOOS == "windows" {
		if mode&0200 != 0 {
			return 0666
		}
		return 0444
	}
	return mode
}

func imagePath(root, rel string) (string, error) {
	if _, err := ChangePaths(Snapshot{RepoRoot: root}, []Change{{Path: rel}}); err != nil {
		return "", err
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	for parent := filepath.Dir(path); parent != root; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: parent of %s is not a real directory", ErrOutsideWorkspace, rel)
		}
	}
	return path, nil
}

func readImage(root, rel string) (fileImage, error) {
	path, err := imagePath(root, rel)
	if err != nil {
		return fileImage{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileImage{}, nil
	}
	if err != nil {
		return fileImage{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return fileImage{Kind: "symlink", Data: []byte(target)}, err
	}
	if !info.Mode().IsRegular() {
		return fileImage{}, fmt.Errorf("%w: unsupported file type at %s", ErrApplyConflict, rel)
	}
	raw, err := os.ReadFile(path)
	return fileImage{Kind: "file", Mode: info.Mode().Perm(), Data: raw}, err
}

func checkImage(root, rel string, expected fileImage) error {
	live, err := readImage(root, rel)
	if err != nil {
		return err
	}
	if !live.equal(expected) {
		return &ConflictError{Paths: []string{rel}}
	}
	return nil
}

func publishImage(root, rel string, before, after fileImage) error {
	path, err := imagePath(root, rel)
	if err != nil {
		return err
	}
	validate := func() error { return checkImage(root, rel, before) }
	if err := validate(); err != nil {
		return err
	}
	switch after.Kind {
	case "":
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	case "file":
		if before.Kind == "" {
			if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
				return err
			}
			return fileutil.AtomicCreateFile(path, after.Data, after.Mode)
		}
		return fileutil.AtomicWriteFileStrictValidated(path, after.Data, after.Mode, validate)
	case "symlink":
		return publishLink(path, string(after.Data), before.Kind == "", validate)
	default:
		return fmt.Errorf("%w: unsupported image kind", ErrApplyRecovery)
	}
}

func publishLink(path, target string, absent bool, validate func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Dir(path), ".reasonix-link-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	tmp := filepath.Join(dir, "link")
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := validate(); err != nil {
		return err
	}
	if absent {
		return fileutil.RenameNoReplace(tmp, path)
	}
	return os.Rename(tmp, path)
}

func prepareApply(ctx context.Context, repo gitcmd.Repo, tree string, changes []Change, expected map[string]gitFileVersion) (*applyJournal, error) {
	paths := make([]string, 0, len(changes))
	for _, ch := range changes {
		paths = append(paths, ch.Path)
	}
	targets, err := treeBlobs(ctx, repo, tree, paths)
	if err != nil {
		return nil, err
	}
	trust, err := readWorktreeTrust(ctx, repo)
	if err != nil {
		return nil, err
	}
	j := &applyJournal{Version: 1, Root: repo.WorkTree, Tree: tree}
	for _, ch := range changes {
		before, err := readImage(repo.WorkTree, ch.Path)
		if err != nil {
			return nil, err
		}
		after, err := targetImage(ctx, repo, ch.Path, targets[ch.Path], before, trust.symlinks)
		if err != nil {
			return nil, err
		}
		j.Files = append(j.Files, applyRecord{Path: ch.Path, Before: before, After: after})
	}
	live, err := liveBlobs(ctx, repo, paths)
	if err != nil {
		return nil, err
	}
	for _, record := range j.Files {
		if live[record.Path] != expected[record.Path] {
			return nil, &ConflictError{Paths: []string{record.Path}}
		}
		if err := checkImage(repo.WorkTree, record.Path, record.Before); err != nil {
			return nil, err
		}
	}
	return j, nil
}

// targetImage is what checking target out writes. Where Git does not trust
// symlinks, a link is a regular file holding its target text.
func targetImage(ctx context.Context, repo gitcmd.Repo, path string, target gitFileVersion, before fileImage, symlinks bool) (fileImage, error) {
	if target == absent {
		return fileImage{}, nil
	}
	image := fileImage{Kind: "file", Mode: os.FileMode(newFileMode())}
	if before.Kind == "file" {
		image.Mode = before.Mode &^ 0111
	}
	args := []string{"cat-file", "--filters", "--path=" + path, target.Blob}
	switch target.Mode {
	case "120000":
		if symlinks {
			image.Kind, image.Mode = "symlink", 0
		}
		args = []string{"cat-file", "blob", target.Blob}
	case "100644":
	case "100755":
		image.Mode |= 0111
	default:
		return fileImage{}, fmt.Errorf("unsupported candidate mode %s at %s", target.Mode, path)
	}
	out, stderr, err := runGit(ctx, repo, args...)
	if err != nil {
		return fileImage{}, fmt.Errorf("materialize %s: %w%s", path, err, stderrSuffix(stderr))
	}
	image.Data = []byte(out)
	if target.Mode == "120000" && strings.ContainsRune(out, 0) {
		return fileImage{}, errors.New("invalid symlink target")
	}
	return image, nil
}
