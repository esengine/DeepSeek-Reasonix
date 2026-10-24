package isolation

import (
	"context"
	"errors"
	"reflect"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
)

func (s *Store) operationSnapshot(ctx context.Context, id string) (*Entry, error) {
	unlock, err := s.lockEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := s.recoverChildReservationsLocked(id); err != nil {
		return nil, err
	}
	return s.operationManifest(id)
}

func (s *Store) operationManifest(id string) (*Entry, error) {
	d, err := s.scopedResultMetadata(id)
	if err != nil {
		return nil, err
	}
	if d.Version == 0 {
		return s.lookup(id)
	}
	if !supportedResultVersion(d) || !supportedLifecycle(d) {
		return nil, tool.Refusal{Code: CodeUnsupportedState, Message: "isolated result was written by a newer protocol"}
	}
	if err := s.requireSettledChildren(d.entry()); err != nil {
		return nil, err
	}
	release, err := filelock.TryAcquire(s.runLock(id))
	if errors.Is(err, filelock.ErrHeld) {
		return nil, tool.Refusal{Code: CodeBusy, Message: "isolated task is still running"}
	}
	if err != nil {
		return nil, err
	}
	release()
	if settleable(d.State) && d.Tree == "" {
		return nil, tool.Refusal{Code: CodeRecoveryRequired, Message: "isolated result has no sealed manifest; original record retained"}
	}
	return d.entry(), nil
}

// Resources precede this lock. Generation and environment identity are checked
// again; recover classifies candidate content drift while resources are held.
func (s *Store) lockCurrent(ctx context.Context, before *Entry) (*Entry, func(), error) {
	return s.lockCurrentUsing(ctx, before, s.lookup)
}

func (s *Store) lockCurrentUsing(ctx context.Context, before *Entry, lookup func(string) (*Entry, error)) (*Entry, func(), error) {
	unlock, err := s.lockEntry(ctx, before.ID)
	if err != nil {
		return nil, nil, err
	}
	e, err := lookup(before.ID)
	if err == nil && (e.RunGeneration != before.RunGeneration || e.Snap != before.Snap || e.Cand != before.Cand || e.ResumeOwner != before.ResumeOwner || e.Workspace != before.Workspace || e.DiscardPolicy != before.DiscardPolicy || !reflect.DeepEqual(e.Origin, before.Origin)) {
		err = tool.Refusal{Code: CodeStaleExecution, Message: "isolated result changed while waiting; review and retry"}
	}
	if err != nil {
		unlock()
		return nil, nil, err
	}
	return e, unlock, nil
}

func (s *Store) failedFinish(ctx context.Context, e *Entry, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	unlock, err := s.lockEntry(ctx, e.ID)
	if err != nil {
		return errors.Join(cause, err)
	}
	defer unlock()
	if err := s.checkRunGeneration(e); err != nil {
		return errors.Join(cause, err)
	}
	e.State = RecoveryRequired
	return errors.Join(cause, persist(e))
}

func interruptFinishedParent(e *Entry, cause error) error {
	e.State = Interrupted
	if e.Evidence != nil {
		e.Evidence.Verification = "unknown"
		e.Evidence.Checks = nil
		e.Evidence.FinishedContent = nil
	}
	return errors.Join(cause, persist(e))
}
