package isolation

import (
	"context"
	"os"
	"path/filepath"

	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
)

// Preparation binds candidate creation to one Store and captured Git snapshot.
// It is process-local and does not publish or retain a running result.
type Preparation struct {
	store    *Store
	snap     worktree.Snapshot
	identity preparationIdentity
}

type preparationIdentity struct{ source, workspace, common, git os.FileInfo }

// Prepare captures one Git snapshot for subsequent candidates from this Store.
// It does not take arbitrary snapshot, source or candidate-path inputs.
func (s *Store) Prepare(ctx context.Context) (*Preparation, error) {
	if s == nil {
		return nil, errUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snap, err := s.captureSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	identity, err := preparationWorkspace(s, snap)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Preparation{store: s, snap: snap, identity: identity}, nil
}

// Begin creates a distinct result candidate from the captured snapshot.
// The Store owns its source workspace, managed root and result identity.
func (p *Preparation) Begin(ctx context.Context) (*Entry, error) {
	if p == nil || p.store == nil {
		return nil, errUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := preparationWorkspace(p.store, p.snap)
	if err != nil {
		return nil, err
	}
	if !samePreparationIdentity(p.identity, current) {
		return nil, errUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return p.store.beginSnapshot(ctx, p.store.workspace, p.snap)
}

func preparationWorkspace(store *Store, snap worktree.Snapshot) (preparationIdentity, error) {
	source, err := openRecordDirectory(snap.RepoRoot)
	if err != nil {
		return preparationIdentity{}, errUnavailable
	}
	defer source.root.Close()
	prefix := filepath.FromSlash(snap.Prefix)
	if prefix == "" {
		prefix = "."
	}
	if !filepath.IsLocal(prefix) || !source.same(store.workspace, prefix) {
		return preparationIdentity{}, tool.Refusal{Code: CodeUnavailable, Message: "the Store workspace no longer matches its prepared source"}
	}
	common, err := openRecordDirectory(snap.Repo.CommonDir)
	if err != nil {
		return preparationIdentity{}, errUnavailable
	}
	defer common.root.Close()
	gitDir, ok := common.local(snap.Repo.GitDir)
	if !ok {
		return preparationIdentity{}, errUnavailable
	}
	var identity preparationIdentity
	for _, item := range []struct {
		root *os.Root
		path string
		info *os.FileInfo
	}{{source.root, ".", &identity.source}, {source.root, prefix, &identity.workspace},
		{common.root, ".", &identity.common}, {common.root, gitDir, &identity.git}} {
		info, err := item.root.Stat(item.path)
		if err != nil || !info.IsDir() {
			return preparationIdentity{}, errUnavailable
		}
		*item.info = info
	}
	return identity, nil
}

func samePreparationIdentity(a, b preparationIdentity) bool {
	for _, pair := range [][2]os.FileInfo{{a.source, b.source}, {a.workspace, b.workspace}, {a.common, b.common}, {a.git, b.git}} {
		if pair[0] == nil || pair[1] == nil || !os.SameFile(pair[0], pair[1]) {
			return false
		}
	}
	return true
}

func (s *Store) captureSnapshot(ctx context.Context) (worktree.Snapshot, error) {
	snap, err := worktree.TakeSnapshot(ctx, s.repo)
	if err != nil {
		return worktree.Snapshot{}, tool.Refusal{Code: CodeNotGit, Message: "worktree isolation needs a Git workspace with a commit: " + err.Error()}
	}
	return snap, nil
}

func (s *Store) beginSnapshot(ctx context.Context, workspaceRoot string, snap worktree.Snapshot) (*Entry, error) {
	cand, err := worktree.CreateCandidate(ctx, snap, s.managedRoot)
	if err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		_ = worktree.RemoveCandidate(context.WithoutCancel(ctx), snap, cand)
		return nil, err
	}
	return &Entry{ID: id, Workspace: workspaceRoot, Snap: snap, Cand: cand}, nil
}
