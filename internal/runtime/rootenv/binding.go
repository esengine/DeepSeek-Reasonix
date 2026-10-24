package rootenv

import (
	"context"
	"errors"
	"maps"

	"reasonix/internal/base/filelock"

	"reasonix/internal/state/sessionstore"
)

func (e *Environment) Validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrStale
	}
	e.owner.mu.Lock()
	defer e.owner.mu.Unlock()
	r, err := e.owner.store.load(e.owner.record.ID)
	if err != nil {
		return err
	}
	if r.Generation != e.owner.record.Generation || r.RunGeneration != e.owner.record.RunGeneration {
		return ErrStale
	}
	if r.State != "ready" {
		return ErrRecovery
	}
	return e.owner.store.verifyCandidate(r)
}

func (e *Environment) BindSession(ctx context.Context, path string, authority *sessionstore.SessionWriteAuthority) error {
	if err := e.Validate(ctx); err != nil {
		return err
	}
	r := e.Record()
	if !ownsAttachment(r, sessionstore.CanonicalSessionPath(path)) {
		return ErrOwner
	}
	if !r.Bound {
		if err := e.updatePublication(ctx, r, func(r *Record) error { r.Bound = true; return nil }); err != nil {
			return err
		}
		r = e.Record()
	}
	return sessionstore.BindExecutionEnvironment(path, sessionstore.ExecutionEnvironmentBinding{Version: 1,
		EnvironmentID: r.ID, Generation: r.Generation, SourceWorkspace: r.SourceWorkspace,
		WorkspaceRoot: r.Candidate.WorkspaceRoot, WorkingCopyRoot: r.Candidate.WorktreeRoot}, authority)
}

func (e *Environment) CheckSession(ctx context.Context, path string) error {
	if err := e.Validate(ctx); err != nil {
		return err
	}
	r := e.Record()
	binding, bound, err := sessionstore.LoadExecutionEnvironment(path)
	if err != nil {
		return err
	}
	if !bound || binding.EnvironmentID != r.ID || binding.Generation != r.Generation ||
		!ownsAttachment(r, binding.SessionPath) || binding.SourceWorkspace != r.SourceWorkspace ||
		binding.WorkspaceRoot != r.Candidate.WorkspaceRoot || binding.WorkingCopyRoot != r.Candidate.WorktreeRoot {
		return ErrOwner
	}
	return nil
}

// Resolve checks a session binding before any runtime or extension starts.
// Execution ownership still has to be acquired with Open before publication.
func (s *Store) Resolve(ctx context.Context, binding sessionstore.ExecutionEnvironmentBinding) (Record, error) {
	if binding.ArchiveOnly {
		return Record{}, ErrOwner
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	r, err := s.load(binding.EnvironmentID)
	if err != nil {
		return Record{}, err
	}
	if r.Generation != binding.Generation || !ownsAttachment(r, binding.SessionPath) || r.SourceWorkspace != binding.SourceWorkspace ||
		r.Candidate.WorkspaceRoot != binding.WorkspaceRoot || r.Candidate.WorktreeRoot != binding.WorkingCopyRoot {
		return Record{}, ErrOwner
	}
	if r.State != "ready" {
		return Record{}, ErrRecovery
	}
	if err := s.verifyCandidate(r); err != nil {
		return Record{}, errors.Join(ErrRecovery, err)
	}
	return r, nil
}

// AttachSession records an immutable transcript attachment to the same live
// scene. Creation identity stays unchanged; a new attachment requires both the
// current environment owner and the target transcript's write authority.
func (e *Environment) AttachSession(ctx context.Context, parent, path string, authority *sessionstore.SessionWriteAuthority) error {
	return sessionstore.WithSessionPublication(path, func(p *sessionstore.SessionPublication) error {
		return e.AttachSessionPublication(ctx, parent, path, authority, p)
	})
}

// AttachSessionPublication reuses the recovery writer's session lock boundary.
func (e *Environment) AttachSessionPublication(ctx context.Context, parent, path string, authority *sessionstore.SessionWriteAuthority, publication *sessionstore.SessionPublication) error {
	if err := e.CheckSession(ctx, parent); err != nil {
		return err
	}
	end, err := authority.BeginSave(path)
	if err != nil {
		return err
	}
	defer end()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrStale
	}
	e.owner.mu.Lock()
	defer e.owner.mu.Unlock()
	s := e.owner.store
	unlock, err := filelock.Acquire(ctx, s.recordPath(e.owner.record.ID)+".lock")
	if err != nil {
		return err
	}
	defer unlock()
	r, err := s.load(e.owner.record.ID)
	if err != nil {
		return err
	}
	if r.RunGeneration != e.owner.record.RunGeneration || r.Generation != e.owner.record.Generation {
		return ErrStale
	}
	parent, path = sessionstore.CanonicalSessionPath(parent), sessionstore.CanonicalSessionPath(path)
	if !ownsAttachment(r, parent) {
		return ErrOwner
	}
	if !ownsAttachment(r, path) {
		r.Attachments = maps.Clone(r.Attachments)
		if r.Attachments == nil {
			r.Attachments = map[string]string{}
		}
		r.Attachments[path] = parent
		if err := s.save(r); err != nil {
			return err
		}
	}
	// The record comes first: interruption may leave an unused attachment, but
	// never a visible transcript that can fall back to its source workspace.
	e.owner.record = r
	return publication.BindExecutionEnvironment(sessionstore.ExecutionEnvironmentBinding{Version: 1,
		EnvironmentID: r.ID, Generation: r.Generation, SourceWorkspace: r.SourceWorkspace,
		WorkspaceRoot: r.Candidate.WorkspaceRoot, WorkingCopyRoot: r.Candidate.WorktreeRoot}, authority)
}

func ownsAttachment(r Record, path string) bool {
	if path == r.SessionID {
		return true
	}
	seen := map[string]bool{}
	for path != "" && !seen[path] {
		seen[path] = true
		path = r.Attachments[path]
		if path == r.SessionID {
			return true
		}
	}
	return false
}
