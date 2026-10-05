package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

// ErrWorkspaceMoved is an Apply refused because the source workspace no longer
// holds the state its candidates started from.
var ErrWorkspaceMoved = errors.New("the workspace changed after the candidates started")

// Snapshot is a workspace's whole current state as a commit over HEAD: tracked
// edits and every untracked file Git does not ignore. Taking one leaves the
// user's index, refs and files untouched.
type Snapshot struct {
	RepoRoot string
	Prefix   string
	Commit   string
	Tree     string
	Repo     gitcmd.Repo // the source workspace's identity, at its root
}

// Change is one path a candidate added, modified or deleted, relative to the
// repository root.
type Change struct {
	Status string // A, M or D; renames are split into D and A
	Path   string
}

// Candidate is a detached worktree holding one Snapshot, and what it became.
type Candidate struct {
	WorkspaceRoot string
	WorktreeRoot  string
	Repo          gitcmd.Repo // resolved when the host created the worktree
}

// TakeSnapshot records the current state of repo's workspace, resolved when
// the workspace opened, as a dangling commit.
func TakeSnapshot(ctx context.Context, repo gitcmd.Repo) (Snapshot, error) {
	info, err := inspect(ctx, repo)
	if err != nil {
		return Snapshot{}, err
	}
	tree, err := workingTree(ctx, info.repo)
	if err != nil {
		return Snapshot{}, err
	}
	commit, stderr, err := runGitEnv(ctx, info.repo, snapshotIdentity, "commit-tree", tree, "-p", info.head, "-m", "reasonix candidate base")
	if err != nil {
		return Snapshot{}, fmt.Errorf("record workspace snapshot: %w%s", err, stderrSuffix(stderr))
	}
	return Snapshot{RepoRoot: info.RepoRoot, Prefix: strings.Trim(strings.TrimSpace(info.prefix), "/"),
		Commit: strings.TrimSpace(commit), Tree: tree, Repo: info.repo}, nil
}

// repo is the snapshot's pinned identity. An entry persisted without one is
// resolved from its root.
func (s Snapshot) repo(ctx context.Context) (gitcmd.Repo, error) {
	if s.Repo.Valid() {
		return s.Repo.Top(), s.Repo.Top().Verify()
	}
	return gitcmd.Open(ctx, s.RepoRoot)
}

func (c Candidate) repo(ctx context.Context) (gitcmd.Repo, error) {
	if c.Repo.Valid() {
		return c.Repo.Top(), c.Repo.Top().Verify()
	}
	return gitcmd.Open(ctx, c.WorktreeRoot)
}

// CreateCandidate checks snap out into a new detached worktree under managedRoot.
func CreateCandidate(ctx context.Context, snap Snapshot, managedRoot string) (Candidate, error) {
	id, err := randomID()
	if err != nil {
		return Candidate{}, err
	}
	source, err := snap.repo(ctx)
	if err != nil {
		return Candidate{}, err
	}
	root := filepath.Join(managedRoot, id, safePathComponent(filepath.Base(snap.RepoRoot)))
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		return Candidate{}, fmt.Errorf("create candidate parent: %w", err)
	}
	// --no-checkout, then populate through gitcmd inside the new worktree: a
	// checkout during add runs in a child whose gitdir is the new worktree,
	// where the reset's driver listing sees an includeIf keyed on that gitdir.
	if _, stderr, err := runGit(ctx, source, "worktree", "add", "--no-checkout", "--detach", root, snap.Commit); err != nil {
		return Candidate{}, fmt.Errorf("create candidate worktree: %w%s", err, stderrSuffix(stderr))
	}
	repo, err := gitcmd.Open(ctx, root)
	if err != nil {
		_, _, _ = runGit(ctx, source, "worktree", "remove", "--force", root)
		return Candidate{}, fmt.Errorf("resolve candidate worktree: %w", err)
	}
	if _, stderr, err := runGit(ctx, repo, "reset", "--hard", snap.Commit); err != nil {
		_, _, _ = runGit(ctx, source, "worktree", "remove", "--force", root)
		return Candidate{}, fmt.Errorf("populate candidate worktree: %w%s", err, stderrSuffix(stderr))
	}
	c := Candidate{WorkspaceRoot: root, WorktreeRoot: root, Repo: repo}
	if snap.Prefix != "" {
		c.WorkspaceRoot = filepath.Join(root, filepath.FromSlash(snap.Prefix))
	}
	return c, nil
}

// CandidateTree is the candidate's current state as a tree, and the paths that
// differ from the snapshot it started from.
func CandidateTree(ctx context.Context, snap Snapshot, c Candidate) (string, []Change, error) {
	repo, err := c.repo(ctx)
	if err != nil {
		return "", nil, err
	}
	tree, err := workingTree(ctx, repo)
	if err != nil {
		return "", nil, err
	}
	source, err := snap.repo(ctx)
	if err != nil {
		return "", nil, err
	}
	changes, err := treeChanges(ctx, source, snap.Tree, tree)
	return tree, changes, err
}

// Patch is the textual diff between two trees, for a reader rather than git apply.
func Patch(ctx context.Context, snap Snapshot, tree string) (string, error) {
	source, err := snap.repo(ctx)
	if err != nil {
		return "", err
	}
	out, stderr, err := runGit(ctx, source, "diff", "--no-ext-diff", "--no-renames", "--stat", "--patch", snap.Tree, tree)
	if err != nil {
		return "", fmt.Errorf("diff candidate: %w%s", err, stderrSuffix(stderr))
	}
	return out, nil
}

// Apply brings tree's version of changes into the source workspace's files. It
// refuses with ErrWorkspaceMoved unless the workspace still holds snap, and
// writes the working tree only: the user's index is not staged.
func Apply(ctx context.Context, snap Snapshot, tree string, changes []Change) error {
	source, err := snap.repo(ctx)
	if err != nil {
		return err
	}
	current, err := workingTree(ctx, source)
	if err != nil {
		return err
	}
	if current != snap.Tree {
		return ErrWorkspaceMoved
	}
	var restore []string
	for _, ch := range changes {
		if ch.Status == "D" {
			if err := os.Remove(filepath.Join(snap.RepoRoot, filepath.FromSlash(ch.Path))); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("apply deletion of %s: %w", ch.Path, err)
			}
			continue
		}
		restore = append(restore, ch.Path)
	}
	if len(restore) == 0 {
		return nil
	}
	args := append([]string{"restore", "--source=" + tree, "--worktree", "--"}, restore...)
	if _, stderr, err := runGit(ctx, source, args...); err != nil {
		return fmt.Errorf("apply candidate files: %w%s", err, stderrSuffix(stderr))
	}
	return nil
}

// RemoveCandidate deletes a candidate worktree and its administrative entry.
func RemoveCandidate(ctx context.Context, snap Snapshot, c Candidate) error {
	source, err := snap.repo(ctx)
	if err != nil {
		return err
	}
	if _, stderr, err := runGit(ctx, source, "worktree", "remove", "--force", c.WorktreeRoot); err != nil {
		return fmt.Errorf("remove candidate worktree: %w%s", err, stderrSuffix(stderr))
	}
	_ = os.Remove(filepath.Dir(c.WorktreeRoot))
	return nil
}

// workingTree writes repo's working state as a tree through a private copy of
// its index, so only changed files are hashed and the real index is untouched.
func workingTree(ctx context.Context, repo gitcmd.Repo) (string, error) {
	indexPath, _, err := runGit(ctx, repo, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return "", fmt.Errorf("locate Git index: %w", err)
	}
	dir, err := os.MkdirTemp("", "reasonix-snapshot-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	private := filepath.Join(dir, "index")
	if err := copyFile(strings.TrimSpace(indexPath), private); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("copy Git index: %w", err)
	}
	env := []string{"GIT_INDEX_FILE=" + private}
	stageCtx, cancel := context.WithTimeout(ctx, gitWorktreeAddTimeout)
	defer cancel()
	if err := repo.StageAll(stageCtx, env...); err != nil {
		return "", fmt.Errorf("stage workspace snapshot: %w", err)
	}
	tree, stderr, err := runGitEnv(ctx, repo, env, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write workspace tree: %w%s", err, stderrSuffix(stderr))
	}
	return strings.TrimSpace(tree), nil
}

func treeChanges(ctx context.Context, repo gitcmd.Repo, from, to string) ([]Change, error) {
	out, stderr, err := runGit(ctx, repo, "diff", "--no-ext-diff", "--no-renames", "--name-status", "-z", from, to)
	if err != nil {
		return nil, fmt.Errorf("list candidate changes: %w%s", err, stderrSuffix(stderr))
	}
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var changes []Change
	for i := 0; i+1 < len(fields); i += 2 {
		changes = append(changes, Change{Status: fields[i][:1], Path: fields[i+1]})
	}
	return changes, nil
}

// snapshotIdentity signs the snapshot commit, which no person authored and no
// ref ever points at; without it a machine with no git identity cannot take one.
var snapshotIdentity = []string{
	"GIT_AUTHOR_NAME=Reasonix", "GIT_AUTHOR_EMAIL=reasonix@localhost",
	"GIT_COMMITTER_NAME=Reasonix", "GIT_COMMITTER_EMAIL=reasonix@localhost",
}

func runGitEnv(parent context.Context, repo gitcmd.Repo, env []string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(parent, gitWorktreeAddTimeout)
	defer cancel()
	cmd := repo.Command(ctx, args...)
	cmd.Env = append(cmd.Env, env...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	err := cmd.Run()
	return outBuf.String(), strings.TrimSpace(errBuf.String()), err
}

// copyFile copies an index keeping its modification time: git re-hashes an
// entry written in the index's own second only while the index file's mtime
// says so, and a fresh mtime would hide a same-size edit made in that second.
func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(to, info.ModTime(), info.ModTime())
}
