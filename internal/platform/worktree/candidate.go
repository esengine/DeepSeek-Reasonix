package worktree

import (
	"errors"

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
