package isolation

import (
	"context"
	"errors"
	"fmt"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
)

// RecheckImportedResult observes an immutable export under a separate run
// owner. Checks hold no shared-file transaction across their execution.
func (s *Store) RecheckImportedResult(ctx context.Context, request SnapshotImport, run func(context.Context, *Entry) ([]CheckEvidence, error)) (*Entry, error) {
	before, err := s.operationSnapshot(ctx, request.ID)
	if err != nil {
		return nil, err
	}
	if !matchesSnapshotImport(before, request) || run == nil {
		return nil, tool.Refusal{Code: CodeOwnerMismatch, Message: "recheck does not own this exported root snapshot"}
	}
	release, err := filelock.TryAcquire(s.runLock(before.ID))
	if err != nil {
		return nil, err
	}
	defer release()
	e, err := s.beginImportedChecks(ctx, before)
	if err != nil {
		return nil, err
	}
	checks, runErr := runSnapshotChecks(ctx, e, run)
	return e, errors.Join(runErr, s.finishImportedChecks(context.WithoutCancel(ctx), e, checks))
}

func runSnapshotChecks(ctx context.Context, e *Entry, run func(context.Context, *Entry) ([]CheckEvidence, error)) (checks []CheckEvidence, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("snapshot checks panicked; result retained: %v", p)
		}
	}()
	return run(ctx, e)
}

func (s *Store) beginImportedChecks(ctx context.Context, before *Entry) (*Entry, error) {
	lease, err := s.holdCandidate(ctx, before, "isolation.import.check_inputs")
	if err != nil {
		return nil, err
	}
	defer lease.End(mutation.NotExecuted)
	unlock, err := s.lockEntry(ctx, before.ID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	d, err := s.managedResultMetadata(before.ID)
	if err != nil {
		return nil, err
	}
	e := d.entry()
	if err := s.checkImportedPublication(e); err != nil {
		return nil, err
	}
	if e.RunGeneration != before.RunGeneration || !matchesSnapshotImport(e, SnapshotImport{ID: before.ID, Owner: before.ResumeOwner, Baseline: before.Snap, Origin: *before.Origin}) ||
		(!settleable(e.State) && e.State != Interrupted && e.State != Running) || e.Tree == "" {
		return nil, tool.Refusal{Code: CodeStaleExecution, Message: "export changed or is not available for checks"}
	}
	if !sealedContentUnchanged(ctx, e) {
		e.State = RecoveryRequired
		return nil, errors.Join(tool.Refusal{Code: CodeRecoveryRequired, Message: "immutable export changed; sealed manifest retained"}, persist(e))
	}
	e.State = Running
	if err := persist(e); err != nil {
		return nil, err
	}
	return e, nil
}

func (s *Store) finishImportedChecks(ctx context.Context, e *Entry, checks []CheckEvidence) error {
	lease, err := s.holdCandidate(ctx, e, "isolation.import.check_receipt")
	if err != nil {
		return err
	}
	defer lease.End(mutation.NotExecuted)
	unlock, err := s.lockEntry(ctx, e.ID)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.checkRunGeneration(e); err != nil {
		return err
	}
	e.State = Pending
	if !sealedContentUnchanged(ctx, e) {
		e.State = RecoveryRequired
		return errors.Join(tool.Refusal{Code: CodeRecoveryRequired, Message: "checks changed the immutable export; original manifest retained"}, persist(e))
	}
	if e.Evidence == nil {
		e.Evidence = CaptureCandidateEvidence(e, evidence.ChildEvidenceSummary{})
	}
	e.Evidence.Checks, e.Evidence.Verification = checks, "unknown"
	return persist(e)
}
