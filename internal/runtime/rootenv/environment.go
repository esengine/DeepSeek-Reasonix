// Package rootenv owns persistent session worktrees independently of results.
// Closing a runtime releases execution ownership without removing its files.
package rootenv

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/mutation"
)

var (
	ErrProtocol = errors.New("unsupported root environment; original retained")
	ErrOwner    = errors.New("root environment belongs to another session")
	ErrRecovery = errors.New("root environment preparation was interrupted; files retained")
	ErrStale    = errors.New("root environment execution no longer owns this generation")
)

type Record struct {
	Version         int                          `json:"version"`
	ID              string                       `json:"id"`
	Generation      string                       `json:"generation"`
	RunGeneration   string                       `json:"run_generation"`
	SessionID       string                       `json:"session_id"`
	Attachments     map[string]string            `json:"attachments,omitempty"`
	Bound           bool                         `json:"bound,omitempty"` // set before any transcript binding is written
	SourceWorkspace string                       `json:"source_workspace"`
	Source          gitcmd.Repo                  `json:"source"`
	State           string                       `json:"state"`
	Reason          string                       `json:"reason,omitempty"`
	Baseline        worktree.Snapshot            `json:"baseline"`
	Cursor          worktree.Snapshot            `json:"cursor"`
	CursorResultID  string                       `json:"cursor_result_id,omitempty"`
	Candidate       worktree.Candidate           `json:"candidate"`
	Environment     *worktree.EnvironmentReceipt `json:"prepared_environment,omitempty"`
	Materialization *worktree.Snapshot           `json:"materialization,omitempty"`
	ParentID        string                       `json:"parent_environment_id,omitempty"`
	Publication     *Publication                 `json:"publication"`
	CheckContract   *CheckContract               `json:"check_contract,omitempty"`
}

type Store struct {
	dir              string
	workspace        string
	repo             gitcmd.Repo
	mutations        *mutation.Coordinator
	preparation      []mutation.Capacity
	preparationProbe func(Record) error
	checks           *CheckContract
}

func (s *Store) WithPreparationCapacities(capacities []mutation.Capacity) *Store {
	s.preparation = slices.Clone(capacities)
	return s
}

func New(managedRoot, sourceWorkspace string, repo gitcmd.Repo, coordinator *mutation.Coordinator) (*Store, error) {
	if !filepath.IsAbs(managedRoot) || !filepath.IsAbs(sourceWorkspace) || coordinator == nil {
		return nil, ErrProtocol
	}
	if err := repo.Verify(); err != nil {
		return nil, err
	}
	workspace, err := fileutil.CanonicalWritePath(sourceWorkspace)
	if err != nil {
		return nil, err
	}
	repoDir, err := fileutil.CanonicalWritePath(repo.Dir)
	if err != nil {
		return nil, err
	}
	if workspace != repoDir {
		return nil, ErrProtocol
	}
	repo.Dir = repoDir
	managed, err := fileutil.CanonicalWritePath(managedRoot)
	if err != nil {
		return nil, err
	}
	worktreeRoot, err := fileutil.CanonicalWritePath(repo.WorkTree)
	if err != nil {
		return nil, err
	}
	if rel, err := filepath.Rel(worktreeRoot, managed); err != nil || filepath.IsLocal(rel) {
		return nil, errors.Join(ErrProtocol, err)
	}
	dir := filepath.Join(managed, "root-environments-v1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{dir: dir, workspace: workspace, repo: repo, mutations: coordinator}, nil
}

// Prepare retains a durable record before snapshotting or changing Git metadata.
// A failed preparation cannot silently execute at source; ReclaimAbandoned removes it.
func (s *Store) Prepare(ctx context.Context, sessionID string, copies, denied []string) (*Environment, error) {
	return s.prepareOwned(ctx, sessionID, copies, denied, nil)
}

func (s *Store) prepareOwned(ctx context.Context, sessionID string, copies, denied []string, parent *Record) (*Environment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, ErrOwner
	}
	r := Record{Version: 1, ID: "renv_" + rand.Text(), Generation: rand.Text(), RunGeneration: rand.Text(),
		SessionID: sessionID, SourceWorkspace: s.workspace, Source: s.repo, State: "preparing"}
	r.CheckContract = s.checks.clone()
	if parent != nil {
		r.ParentID = parent.ID
		r.CheckContract = parent.CheckContract.clone()
	}
	if r.CheckContract != nil {
		r.Version = 2
	}
	if err := os.Mkdir(s.environmentDir(r.ID), 0700); err != nil {
		return nil, err
	}
	release, err := filelock.TryAcquire(s.livePath(r.ID))
	if err != nil {
		return nil, err
	}
	e := newEnvironment(s, r, release)
	delivered := false
	defer func() {
		if !delivered {
			e.Close()
		}
	}()
	if err := s.save(r); err != nil {
		e.Close()
		return nil, err
	}
	err = s.prepare(ctx, e, copies, denied, parent)
	if err != nil {
		e.owner.record.State, e.owner.record.Reason = "recovery_required", err.Error()
		err = errors.Join(err, s.save(e.owner.record), e.Close())
	}
	delivered = err == nil
	return e, err
}

func (s *Store) prepare(ctx context.Context, e *Environment, copies, denied []string, parent *Record) error {
	r := &e.owner.record
	paths := []string{s.repo.WorkTree, s.repo.CommonDir, s.environmentDir(r.ID)}
	inputRepo := s.repo
	if parent != nil {
		paths = append(paths, parent.Candidate.WorktreeRoot)
		inputRepo = parent.Candidate.Repo
		inputRepo.Dir = parent.Candidate.WorkspaceRoot
	}
	lease, err := s.mutations.Acquire(mutation.WithoutLease(ctx), mutation.Intent{Effect: mutation.NamedPaths,
		Paths: paths, ExecutionID: r.ID, CallID: "root_environment.prepare", Capacities: s.preparationCapacities(ctx)})
	if err != nil {
		return err
	}
	defer lease.End(mutation.Unknown)
	ctx = lease.Context(ctx)
	if parent != nil {
		current, err := s.load(parent.ID)
		if err != nil {
			return err
		}
		if current.Generation != parent.Generation || current.RunGeneration != parent.RunGeneration || current.Cursor != parent.Cursor {
			return ErrStale
		}
	}
	snap, err := worktree.TakeSnapshot(ctx, inputRepo)
	if err != nil {
		return err
	}
	r.Baseline, r.Cursor = snap, snap
	if parent != nil {
		r.Baseline, r.Cursor = parent.Cursor, parent.Cursor
		r.Materialization = &snap
		if err := s.pin(ctx, r.ID+"-materialization", snap.Commit); err != nil {
			return err
		}
	}
	if err := s.pin(ctx, r.ID, r.Baseline.Commit); err != nil {
		return err
	}
	candidate, err := worktree.PlanCandidate(snap, s.environmentDir(r.ID))
	if err != nil {
		return err
	}
	r.Candidate, r.State = candidate, "materializing"
	if err := s.save(*r); err != nil {
		return err
	}
	if s.preparationProbe != nil {
		if err := s.preparationProbe(*r); err != nil {
			return err
		}
	}
	r.Candidate, err = worktree.MaterializeCandidate(ctx, snap, candidate)
	if err != nil {
		return err
	}
	if len(copies) > 0 {
		if err := worktree.EnvironmentSourceMatches(ctx, snap); err != nil {
			return err
		}
		r.Environment, err = worktree.PrepareEnvironment(ctx, snap, r.Candidate, copies, denied...)
		if err != nil {
			return err
		}
	}
	r.State = "ready"
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.save(*r)
}

func (s *Store) pin(ctx context.Context, id, commit string) error {
	cmd := s.repo.Command(ctx, "update-ref", "refs/reasonix/root-environments/"+id, commit)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("anchor persistent environment: %w: %s", err, out)
	}
	return nil
}

// Open restores the same live worktree. It never repopulates it from a snapshot.
func (s *Store) Open(ctx context.Context, id, sessionID string) (*Environment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	before, err := s.load(id)
	if err != nil {
		return nil, err
	}
	if !ownsAttachment(before, sessionID) {
		return nil, ErrOwner
	}
	release, err := filelock.TryAcquire(s.livePath(id))
	if err != nil {
		return nil, err
	}
	e := newEnvironment(s, before, release)
	ok := false
	defer func() {
		if !ok {
			e.Close()
		}
	}()
	unlock, err := filelock.Acquire(ctx, s.recordPath(id)+".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()
	r, err := s.load(id)
	if err != nil {
		return nil, err
	}
	if r.Generation != before.Generation || !ownsAttachment(r, sessionID) {
		return nil, ErrStale
	}
	if r.State != "ready" {
		return nil, ErrRecovery
	}
	if err := s.verifyCandidate(r); err != nil {
		return nil, errors.Join(ErrRecovery, err)
	}
	r.RunGeneration = rand.Text()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.save(r); err != nil {
		return nil, err
	}
	e.owner.record, ok = r, true
	return e, nil
}
