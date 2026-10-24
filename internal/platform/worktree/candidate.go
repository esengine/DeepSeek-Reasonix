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
	Metadata string      `json:",omitempty"`
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
	metadata, err := captureSnapshotMetadata(ctx, info.repo, tree)
	if err != nil {
		return Snapshot{}, err
	}
	metadataParent, err := snapshotMetadataAnchor(ctx, info.repo, metadata)
	if err != nil {
		return Snapshot{}, err
	}
	commit, stderr, err := runGitEnv(ctx, info.repo, snapshotIdentity, "commit-tree", tree, "-p", info.head, "-p", metadataParent, "-m", "reasonix candidate base")
	if err != nil {
		return Snapshot{}, fmt.Errorf("record workspace snapshot: %w%s", err, stderrSuffix(stderr))
	}
	return Snapshot{RepoRoot: info.RepoRoot, Prefix: strings.Trim(strings.TrimSpace(info.prefix), "/"),
		Commit: strings.TrimSpace(commit), Tree: tree, Repo: info.repo, Metadata: metadata}, nil
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
	c, err := PlanCandidate(snap, managedRoot)
	if err != nil {
		return Candidate{}, err
	}
	prepared, err := MaterializeCandidate(ctx, snap, c)
	if err != nil {
		_ = RemoveCandidate(context.WithoutCancel(ctx), snap, c)
		return Candidate{}, err
	}
	return prepared, nil
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
	release, err := lockApply(ctx, source)
	if err != nil {
		return err
	}
	defer release()
	current, err := workingTree(ctx, source)
	if err != nil {
		return err
	}
	if current != snap.Tree {
		return ErrWorkspaceMoved
	}
	return applyPaths(ctx, snap, tree, changes)
}

// RemoveCandidate deletes a candidate worktree and its administrative entry.
func RemoveCandidate(ctx context.Context, snap Snapshot, c Candidate) error {
	source, err := snap.repo(ctx)
	if err != nil {
		return err
	}
	registered, err := candidateRegistered(ctx, source, c.WorktreeRoot)
	if err != nil {
		return err
	}
	if !registered {
		if _, err := os.Lstat(c.WorktreeRoot); os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("unregistered candidate retained: %s", c.WorktreeRoot)
	}
	if _, stderr, err := runGit(ctx, source, "worktree", "remove", "--force", c.WorktreeRoot); err != nil {
		return fmt.Errorf("remove candidate worktree: %w%s", err, stderrSuffix(stderr))
	}
	_ = os.Remove(filepath.Dir(c.WorktreeRoot))
	return nil
}

// workingTree writes actual workspace bytes through a private index. Rebuild
// cached entries before add: Git's stat cache is not a content version (a writer
// can preserve size and timestamps). The user's index remains untouched.
func workingTree(ctx context.Context, repo gitcmd.Repo) (string, error) {
	indexPath, _, err := runGit(ctx, repo, "rev-parse", "--git-path", "index")
	if err != nil {
		return "", fmt.Errorf("locate Git index: %w", err)
	}
	dir, err := os.MkdirTemp("", "reasonix-snapshot-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	private := filepath.Join(dir, "index")
	indexPath = strings.TrimSpace(indexPath)
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(repo.Dir, indexPath)
	}
	if err := copyFile(indexPath, private); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("copy Git index: %w", err)
	}
	env := []string{"GIT_INDEX_FILE=" + private}
	stageCtx, cancel := context.WithTimeout(ctx, gitWorktreeAddTimeout)
	defer cancel()
	if _, err := os.Stat(private); err == nil {
		unmerged, stderr, err := runGitEnv(stageCtx, repo, env, "ls-files", "--unmerged", "-z")
		if err != nil {
			return "", fmt.Errorf("read private index: %w%s", err, stderrSuffix(stderr))
		}
		if unmerged != "" {
			if err := repo.StageAll(stageCtx, env...); err != nil {
				return "", fmt.Errorf("stage conflicted workspace snapshot: %w", err)
			}
		}
		indexTree, stderr, err := runGitEnv(stageCtx, repo, env, "write-tree")
		if err != nil {
			return "", fmt.Errorf("record private index: %w%s", err, stderrSuffix(stderr))
		}
		if err := os.Remove(private); err != nil {
			return "", err
		}
		if _, stderr, err := runGitEnv(stageCtx, repo, env, "read-tree", strings.TrimSpace(indexTree)); err != nil {
			return "", fmt.Errorf("rebuild private index: %w%s", err, stderrSuffix(stderr))
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
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
