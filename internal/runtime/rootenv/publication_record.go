package rootenv

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"

	"reasonix/internal/base/filelock"
)

func (e *Environment) updatePublication(ctx context.Context, before Record, update func(*Record) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrStale
	}
	e.owner.mu.Lock()
	defer e.owner.mu.Unlock()
	s := e.owner.store
	unlock, err := filelock.Acquire(ctx, s.recordPath(before.ID)+".lock")
	if err != nil {
		return err
	}
	defer unlock()
	r, err := s.load(before.ID)
	if err != nil {
		return err
	}
	if r.Generation != before.Generation || r.RunGeneration != before.RunGeneration || r.State != "ready" ||
		r.Cursor != before.Cursor || !reflect.DeepEqual(r.Publication, before.Publication) {
		return ErrStale
	}
	if err := update(&r); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.save(r); err != nil {
		return err
	}
	e.owner.record = r
	return nil
}

func validPublication(r Record) bool {
	p := r.Publication
	if p == nil {
		return true
	}
	if p.Version != 1 || p.ID == "" || p.ResultID == "" || p.EnvironmentID != r.ID || p.EnvironmentGeneration != r.Generation ||
		!ownsAttachment(r, p.SessionID) || p.From.Repo != r.Source.Top() || p.Target.Repo.CommonDir != r.Source.CommonDir ||
		p.Target.Commit == "" || p.Target.Tree == "" || p.Target.Prefix != r.Baseline.Prefix {
		return false
	}
	if !strings.HasPrefix(p.ID, "pub_") || !validID("renv_"+strings.TrimPrefix(p.ID, "pub_")) {
		return false
	}
	if p.Environment != nil && (p.Environment.Version != 1 || p.Environment.Receipt.Version != 1 ||
		p.Environment.Root != filepath.Join(filepath.Dir(filepath.Dir(r.Candidate.WorktreeRoot)), p.ID+"-inputs")) {
		return false
	}
	if p.State == "pending" || p.State == "preparing" {
		return r.Cursor == p.From
	}
	expected := p.Target
	expected.Repo, expected.RepoRoot, expected.Prefix = r.Source.Top(), r.Source.WorkTree, r.Baseline.Prefix
	return p.State == "acknowledged" && r.Cursor == expected && r.CursorResultID == p.ResultID
}
