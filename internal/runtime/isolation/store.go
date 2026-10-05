// Package isolation runs delegated work in a git worktree of the workspace and
// holds what it produced until the parent applies or discards it. The work
// never touches the workspace while it runs; applying it is an ordinary,
// observed write that is refused path by path where the workspace moved on.
package isolation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/worktree"
)

// Refusal codes an isolated run or its settlement answers with.
const (
	CodeUnavailable   = "isolation.unavailable"
	CodeNotGit        = "isolation.not_git"
	CodeUnknownID     = "isolation.unknown_id"
	CodeApplyConflict = "isolation.apply_conflict"
)

var errUnavailable = tool.Refusal{Code: CodeUnavailable, Message: "worktree isolation is not available in this session"}

// Entry is one finished isolated run whose changes wait on the parent.
type Entry struct {
	ID        string
	Workspace string
	Snap      worktree.Snapshot
	Cand      worktree.Candidate
	Tree      string
	Changes   []worktree.Change
	Stats     []worktree.FileStat
}

// Store indexes the pending isolated results of one workspace. The worktrees
// are the results; the index is rebuilt from them, so a result outlives the
// process that produced it and ends only when it is applied or discarded.
type Store struct {
	managedRoot string
	workspace   string
	repo        gitcmd.Repo
	mu          sync.Mutex
	entries     map[string]*Entry
}

// NewStore keeps worktrees under managedRoot and answers for results taken
// from workspaceRoot, whose git identity repo was resolved when it opened.
func NewStore(managedRoot, workspaceRoot string, repo gitcmd.Repo) *Store {
	return &Store{managedRoot: managedRoot, workspace: workspaceRoot, repo: repo, entries: map[string]*Entry{}}
}

// Begin checks the workspace out into a fresh worktree. A workspace git cannot
// snapshot is refused with CodeNotGit rather than run unisolated.
func (s *Store) Begin(ctx context.Context, workspaceRoot string) (*Entry, error) {
	if s == nil {
		return nil, errUnavailable
	}
	snap, err := worktree.TakeSnapshot(ctx, s.repo)
	if err != nil {
		return nil, tool.Refusal{Code: CodeNotGit, Message: "worktree isolation needs a Git workspace with a commit: " + err.Error()}
	}
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

// Finish records what the run changed. A run that changed nothing leaves no
// entry and no worktree, and reports false.
func (s *Store) Finish(ctx context.Context, e *Entry) (bool, error) {
	tree, changes, err := worktree.CandidateTree(ctx, e.Snap, e.Cand)
	if err == nil && len(changes) > 0 {
		e.Stats, err = worktree.DiffStat(ctx, e.Snap, tree, changes)
	}
	if err != nil || len(changes) == 0 {
		s.remove(ctx, e)
		return false, err
	}
	e.Tree, e.Changes = tree, changes
	if err := persist(e); err != nil {
		s.remove(ctx, e)
		return false, err
	}
	s.mu.Lock()
	s.entries[e.ID] = e
	s.mu.Unlock()
	return true, nil
}

// Abort removes a run's worktree without recording anything.
func (s *Store) Abort(ctx context.Context, e *Entry) { s.remove(ctx, e) }

// Paths is where an entry's changes land in the workspace.
func (s *Store) Paths(id string) ([]string, error) {
	e, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	return worktree.ChangePaths(e.Snap, e.Changes)
}

// Apply writes an entry's changes into the workspace and forgets it. A path
// the workspace changed since the run began refuses the whole apply and keeps
// the entry, so the parent can reconcile and try again or discard it.
func (s *Store) Apply(ctx context.Context, id string) (*Entry, error) {
	e, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	if err := worktree.ApplyPaths(ctx, e.Snap, e.Tree, e.Changes); err != nil {
		var conflict *worktree.ConflictError
		if errors.As(err, &conflict) {
			return nil, tool.Refusal{Code: CodeApplyConflict, Message: fmt.Sprintf(
				"nothing was applied: the workspace changed %s since %s began; reconcile and apply again, or discard it",
				strings.Join(conflict.Paths, ", "), id)}
		}
		return nil, err
	}
	s.drop(ctx, id)
	return e, nil
}

// Discard forgets an entry and removes its worktree.
func (s *Store) Discard(ctx context.Context, id string) (*Entry, error) {
	e, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	s.drop(ctx, id)
	return e, nil
}

// Pending lists this workspace's results still waiting on the parent, by id.
func (s *Store) Pending() []string {
	if s == nil {
		return nil
	}
	ids := s.durableIDs()
	s.mu.Lock()
	for id := range s.entries {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	slices.Sort(ids)
	return ids
}

func (s *Store) lookup(id string) (*Entry, error) {
	if s == nil {
		return nil, errUnavailable
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	e, ok := s.entries[id]
	s.mu.Unlock()
	if !ok {
		e, ok = s.recover(id)
	}
	if !ok {
		pending := s.Pending()
		known := "none are pending"
		if len(pending) > 0 {
			known = "pending: " + strings.Join(pending, ", ")
		}
		return nil, tool.Refusal{Code: CodeUnknownID, Message: fmt.Sprintf("no isolated result %q; %s", id, known)}
	}
	return e, nil
}

func (s *Store) drop(ctx context.Context, id string) {
	s.mu.Lock()
	e := s.entries[id]
	delete(s.entries, id)
	s.mu.Unlock()
	if e != nil {
		s.remove(ctx, e)
	}
}

func (s *Store) remove(ctx context.Context, e *Entry) {
	forget(e)
	_ = worktree.RemoveCandidate(context.WithoutCancel(ctx), e.Snap, e.Cand)
}

func newID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "iso_" + hex.EncodeToString(b[:]), nil
}
