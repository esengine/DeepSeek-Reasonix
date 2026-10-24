package rootenv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/runtime/isolation"
)

// RetryPublication is an explicit owner action. It retains both the old result
// and the complete publication record, then permits a fresh result identity.
func (e *Environment) RetryPublication(ctx context.Context, path, publicationID string, results *isolation.Store) error {
	retained, err := e.Retain()
	if err != nil {
		return err
	}
	defer retained.Close()
	if err := retained.CheckSession(ctx, path); err != nil {
		return err
	}
	s, before := retained.owner.store, retained.Record()
	unlock, err := filelock.Acquire(ctx, s.recordPath(before.ID)+".publication.lock")
	if err != nil {
		return err
	}
	defer unlock()
	before = retained.Record()
	p := before.Publication
	if p == nil || p.ID != publicationID || (p.State != "pending" && p.State != "preparing") || results == nil {
		return ErrStale
	}
	return results.RevokeImportedResult(ctx, p.Import(), func() error {
		return retained.updatePublication(ctx, before, func(r *Record) error {
			if err := s.archivePublication(*r); err != nil {
				return err
			}
			r.Publication = nil
			return nil
		})
	})
}

func (s *Store) archivePublication(r Record) error {
	raw, err := os.ReadFile(s.recordPath(r.ID))
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	directory := filepath.Join(s.environmentDir(r.ID), "publication-history-v1")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	path := filepath.Join(directory, r.Publication.ID+".json")
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, fields["publication"]) {
			return nil
		}
		return ErrRecovery
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return fileutil.AtomicCreateFile(path, fields["publication"], 0600)
}
