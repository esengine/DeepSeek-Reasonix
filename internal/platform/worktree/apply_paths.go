package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

// ErrApplyConflict is an ApplyPaths refused because a path it would write was
// changed in the workspace since the snapshot, to something other than the
// candidate's version.
var ErrApplyConflict = errors.New("the workspace changed a path the candidate also changed")

// ErrOutsideWorkspace is a candidate change that lies outside the folder the
// snapshot was taken of.
var ErrOutsideWorkspace = errors.New("the candidate changed a path outside the workspace")

// ConflictError names the paths an ApplyPaths refused over.
type ConflictError struct {
	Paths []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s: %s", ErrApplyConflict, strings.Join(e.Paths, ", "))
}

func (e *ConflictError) Unwrap() error { return ErrApplyConflict }

// FileStat is one changed path's line counts; Binary marks a path git could not
// count lines in.
type FileStat struct {
	Status  string
	Path    string
	Added   int
	Removed int
	Binary  bool
}

// absent is the blob id a path has when it does not exist on that side.
const absent = ""

// ApplyPaths brings tree's version of changes into the source workspace, judged
// path by path: the rest of the workspace may have moved on since snap. A path
// the workspace changed to anything but the candidate's version is a conflict,
// and one conflict writes nothing. A path already holding the candidate's
// version is left as it is.
func ApplyPaths(ctx context.Context, snap Snapshot, tree string, changes []Change) error {
	if len(changes) == 0 {
		return nil
	}
	if _, err := ChangePaths(snap, changes); err != nil {
		return err
	}
	paths := make([]string, 0, len(changes))
	for _, ch := range changes {
		paths = append(paths, ch.Path)
	}
	source, err := snap.repo(ctx)
	if err != nil {
		return err
	}
	base, err := treeBlobs(ctx, source, snap.Tree, paths)
	if err != nil {
		return err
	}
	target, err := treeBlobs(ctx, source, tree, paths)
	if err != nil {
		return err
	}
	live, err := liveBlobs(ctx, source, paths)
	if err != nil {
		return err
	}
	var conflicts []string
	pending := make([]Change, 0, len(changes))
	for _, ch := range changes {
		switch live[ch.Path] {
		case target[ch.Path]:
		case base[ch.Path]:
			pending = append(pending, ch)
		default:
			conflicts = append(conflicts, ch.Path)
		}
	}
	if len(conflicts) > 0 {
		return &ConflictError{Paths: conflicts}
	}
	return writeChanges(ctx, source, tree, pending)
}

// ChangePaths is where each change lands in the source workspace, refusing any
// that falls outside the folder the snapshot was taken of.
func ChangePaths(snap Snapshot, changes []Change) ([]string, error) {
	prefix := strings.Trim(snap.Prefix, "/")
	out := make([]string, 0, len(changes))
	for _, ch := range changes {
		rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(ch.Path)))
		if rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(ch.Path) ||
			(prefix != "" && rel != prefix && !strings.HasPrefix(rel, prefix+"/")) {
			return nil, fmt.Errorf("%w: %s", ErrOutsideWorkspace, ch.Path)
		}
		out = append(out, filepath.Join(snap.RepoRoot, filepath.FromSlash(rel)))
	}
	return out, nil
}

// DiffStat counts each changed path's lines between the snapshot and tree.
func DiffStat(ctx context.Context, snap Snapshot, tree string, changes []Change) ([]FileStat, error) {
	source, err := snap.repo(ctx)
	if err != nil {
		return nil, err
	}
	out, stderr, err := runGit(ctx, source, "diff", "--no-ext-diff", "--no-renames", "--numstat", "-z", snap.Tree, tree)
	if err != nil {
		return nil, fmt.Errorf("count candidate changes: %w%s", err, stderrSuffix(stderr))
	}
	counts := map[string]FileStat{}
	for record := range strings.SplitSeq(strings.TrimSuffix(out, "\x00"), "\x00") {
		fields := strings.SplitN(record, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		st := FileStat{Path: fields[2]}
		if fields[0] == "-" {
			st.Binary = true
		} else {
			st.Added, _ = strconv.Atoi(fields[0])
			st.Removed, _ = strconv.Atoi(fields[1])
		}
		counts[st.Path] = st
	}
	stats := make([]FileStat, 0, len(changes))
	for _, ch := range changes {
		st := counts[ch.Path]
		st.Path, st.Status = ch.Path, ch.Status
		stats = append(stats, st)
	}
	return stats, nil
}

// treeBlobs maps each path to its blob in tree, or to absent.
func treeBlobs(ctx context.Context, repo gitcmd.Repo, tree string, paths []string) (map[string]string, error) {
	args := append([]string{"ls-tree", "-z", "--full-tree", tree, "--"}, paths...)
	out, stderr, err := runGit(ctx, repo, args...)
	if err != nil {
		return nil, fmt.Errorf("read candidate tree: %w%s", err, stderrSuffix(stderr))
	}
	blobs := make(map[string]string, len(paths))
	for _, p := range paths {
		blobs[p] = absent
	}
	for record := range strings.SplitSeq(strings.TrimSuffix(out, "\x00"), "\x00") {
		meta, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		blobs[path] = fields[2]
	}
	return blobs, nil
}

// liveBlobs hashes each path as it stands in the workspace, through the same
// filters a snapshot stages it with, so an unchanged file hashes to its blob. A
// symlink hashes as its target text, which is how git stores one.
func liveBlobs(ctx context.Context, repo gitcmd.Repo, paths []string) (map[string]string, error) {
	repoRoot := repo.WorkTree
	blobs := make(map[string]string, len(paths))
	var regular []string
	for _, p := range paths {
		info, err := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(p)))
		switch {
		case os.IsNotExist(err):
			blobs[p] = absent
		case err != nil:
			return nil, fmt.Errorf("read %s: %w", p, err)
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(filepath.Join(repoRoot, filepath.FromSlash(p)))
			if err != nil {
				return nil, fmt.Errorf("read link %s: %w", p, err)
			}
			id, err := hashText(ctx, repo, target)
			if err != nil {
				return nil, err
			}
			blobs[p] = id
		case info.IsDir():
			blobs[p] = "dir"
		default:
			regular = append(regular, p)
		}
	}
	if len(regular) == 0 {
		return blobs, nil
	}
	out, stderr, err := runGitInput(ctx, repo, strings.Join(regular, "\n")+"\n", "hash-object", "--stdin-paths")
	if err != nil {
		return nil, fmt.Errorf("hash workspace files: %w%s", err, stderrSuffix(stderr))
	}
	ids := strings.Fields(out)
	if len(ids) != len(regular) {
		return nil, fmt.Errorf("hash workspace files: got %d ids for %d paths", len(ids), len(regular))
	}
	for i, p := range regular {
		blobs[p] = ids[i]
	}
	return blobs, nil
}

func hashText(ctx context.Context, repo gitcmd.Repo, text string) (string, error) {
	out, stderr, err := runGitInput(ctx, repo, text, "hash-object", "--no-filters", "--stdin")
	if err != nil {
		return "", fmt.Errorf("hash link target: %w%s", err, stderrSuffix(stderr))
	}
	return strings.TrimSpace(out), nil
}

// writeChanges writes tree's version of each change into repo's files.
func writeChanges(ctx context.Context, repo gitcmd.Repo, tree string, changes []Change) error {
	repoRoot := repo.WorkTree
	var restore []string
	for _, ch := range changes {
		if ch.Status == "D" {
			if err := os.Remove(filepath.Join(repoRoot, filepath.FromSlash(ch.Path))); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("apply deletion of %s: %w", ch.Path, err)
			}
			continue
		}
		restore = append(restore, ch.Path)
	}
	if len(restore) == 0 {
		return nil
	}
	slices.Sort(restore)
	args := append([]string{"restore", "--source=" + tree, "--worktree", "--"}, restore...)
	if _, stderr, err := runGit(ctx, repo, args...); err != nil {
		return fmt.Errorf("apply candidate files: %w%s", err, stderrSuffix(stderr))
	}
	return nil
}

func runGitInput(parent context.Context, repo gitcmd.Repo, input string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(parent, gitTimeout(args))
	defer cancel()
	cmd := repo.Command(ctx, args...)
	cmd.Stdin = strings.NewReader(input)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return outBuf.String(), strings.TrimSpace(errBuf.String()), err
}
