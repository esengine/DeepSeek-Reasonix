package isolation

import (
	"context"
	"errors"
	"os"

	"reasonix/internal/base/fileutil"

	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
)

// SourceAuthorizer rechecks the live source execution's generation, grant and
// current deny rules. It must be noninteractive; any preview approval precedes
// this operation. Possession of a mutation lease is never write authorization.
type SourceAuthorizer func(context.Context, []string) error

// Commit publishes only a complete verified candidate against unchanged source
// inputs. The same attempt ID is retained across interrupted application. File
// replacement uses the shared conditional-rollback journal, not a cross-file
// atomicity claim.
func (i *Integration) Commit(ctx context.Context, authorize SourceAuthorizer) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.requireOpen(); err != nil {
		return err
	}
	if authorize == nil {
		return tool.Refusal{Code: CodeOwnerMismatch, Message: "integration has no live source authorizer"}
	}
	if i.attempt.State != Integrated && ((i.attempt.State != Pending && i.attempt.State != Applying) || i.attempt.Verification != "valid" || !i.store.validIntegrationChecks(i.attempt)) {
		return tool.Refusal{Code: CodeUnverified, Message: "automatic integration requires complete valid verification evidence"}
	}
	original, err := i.store.operationSnapshot(ctx, i.attempt.ResultID)
	if err != nil {
		return err
	}
	paths, err := integrationPaths(original)
	if err != nil {
		return err
	}
	if err := authorize(ctx, paths); err != nil {
		return err
	}
	resources := append(append([]string(nil), paths...), i.store.candidateResources(original)...)
	resources = append(resources, i.attempt.Source.Repo.WorkTree, i.attempt.Candidate.WorktreeRoot)
	lease, err := i.store.mutations.Acquire(mutation.WithoutLease(ctx), mutation.Intent{Effect: mutation.NamedPaths,
		Paths: resources, ExecutionID: i.attempt.ID, CallID: "isolation.integration.commit", Recovery: true})
	if err != nil {
		return err
	}
	defer lease.End(mutation.Unknown)
	ctx = worktree.WithRecoveryAuthorization(lease.Context(ctx), func(actualPaths []string) error {
		if err := lease.CheckPaths(actualPaths); err != nil {
			return err
		}
		return authorize(ctx, actualPaths)
	})
	if err := authorize(ctx, paths); err != nil {
		return err
	}
	e, unlock, err := i.store.lockCurrent(ctx, original)
	if err != nil {
		return err
	}
	defer unlock()
	return i.commitCurrent(ctx, e)
}

func (i *Integration) commitCurrent(ctx context.Context, e *Entry) error {
	if err := i.store.checkImportedPublication(e); err != nil {
		return err
	}
	if !i.originalMatches(e) {
		return tool.Refusal{Code: CodeStaleExecution, Message: "original result changed or was discarded; integration retained"}
	}
	if i.attempt.State == Integrated {
		if e.State != Integrated {
			return tool.Refusal{Code: CodeRecoveryRequired, Message: "integration decisions disagree; records retained"}
		}
		return i.cleanup(ctx, e)
	}
	if !onlyPublishableChanges(ctx, e) {
		return tool.Refusal{Code: CodeRecoveryRequired, Message: "original candidate has unpublished environment changes; both results retained"}
	}
	if err := worktree.RecoverIntegration(ctx, i.attempt.Source.Repo); err != nil {
		return err
	}
	source := evidence.CaptureContentSnapshot(ctx, i.attempt.Source.Repo.WorkTree, 50_000)
	gitInputs := worktree.CaptureGitInputs(ctx, i.attempt.Source.Repo)
	gitUnchanged := gitInputs.Complete && i.attempt.SourceGit.Complete && gitInputs.Digest == i.attempt.SourceGit.Digest
	// A crash can occur after files were published, before either terminal
	// receipt. Exact target content makes that retry idempotent.
	alreadyApplied := i.attempt.State == Applying && sameContent(source, i.attempt.Content) && gitUnchanged
	if alreadyApplied {
		return i.sealPublication(ctx, e)
	}
	if e.State == Integrated || !sameContent(source, i.attempt.SourceContent) || !gitUnchanged {
		return i.invalidate("source verification inputs changed; prepare a new integration candidate")
	}
	return i.publishCandidate(ctx, e)
}

func (i *Integration) originalMatches(e *Entry) bool {
	return e.RunGeneration == i.attempt.ResultGeneration && e.Tree == i.attempt.ResultTree && e.ResumeOwner == i.attempt.Owner && e.State != Discarded
}

func (i *Integration) publishCandidate(ctx context.Context, e *Entry) error {
	candidate := evidence.CaptureContentSnapshot(ctx, i.attempt.Candidate.WorktreeRoot, 50_000)
	if !sameContent(candidate, i.attempt.Content) {
		return i.invalidate("verified integration candidate changed before publication")
	}
	applyCtx := worktree.WithApplyIdentity(ctx, e.ID, i.attempt.ID)
	applyCtx = worktree.WithApplyPostimageValidator(applyCtx, func(ctx context.Context, updates map[string]fileutil.ContentVersion) error {
		projected := evidence.CaptureProjectedContentSnapshot(ctx, i.attempt.Source.Repo.WorkTree, updates, 50_000)
		if !sameContent(projected, i.attempt.Content) {
			return i.invalidate("candidate does not represent the exact publication images; prepare its environment before checking")
		}
		i.attempt.State = Applying
		if err := i.save(); err != nil {
			return err
		}
		e.State = Applying
		return persist(e)
	})
	if err := worktree.ApplyPaths(applyCtx, i.attempt.Source, i.attempt.Tree, i.attempt.Changes); err != nil {
		// Applying is deliberate: cancellation or an external writer can
		// leave a recovery journal. Never discard either candidate here.
		return err
	}
	return i.sealPublication(ctx, e)
}

func (i *Integration) sealPublication(ctx context.Context, e *Entry) error {
	post := evidence.CaptureContentSnapshot(ctx, i.attempt.Source.Repo.WorkTree, 50_000)
	postGit := worktree.CaptureGitInputs(ctx, i.attempt.Source.Repo)
	if !sameContent(post, i.attempt.Content) || !postGit.Complete || postGit.Digest != i.attempt.SourceGit.Digest {
		i.attempt.Verification, i.attempt.Reason = "invalid", "source changed during publication; applied files retained for recovery"
		i.attempt.State = RecoveryRequired
		return errors.Join(tool.Refusal{Code: CodeRecoveryRequired, Message: i.attempt.Reason}, i.save())
	}
	e.State = Integrated
	if err := persist(e); err != nil {
		return err
	}
	i.attempt.State = Integrated
	if err := i.save(); err != nil {
		return err
	}
	return i.cleanup(ctx, e)
}

func (i *Integration) invalidate(reason string) error {
	i.attempt.Verification, i.attempt.Reason = "invalid", reason
	i.attempt.State = Conflict
	return errors.Join(tool.Refusal{Code: CodeStaleExecution, Message: reason}, i.save())
}

func (i *Integration) cleanup(ctx context.Context, original *Entry) error {
	if i.attempt.CleanupComplete {
		return nil
	}
	if _, err := os.Lstat(i.attempt.Candidate.WorktreeRoot); !errors.Is(err, os.ErrNotExist) {
		if !sameContent(evidence.CaptureContentSnapshot(ctx, i.attempt.Candidate.WorktreeRoot, 50_000), i.attempt.Content) {
			return tool.Refusal{Code: CodeRecoveryRequired, Message: "integrated candidate contents changed; cleanup refused and both records retained"}
		}
	}
	if err := i.store.remove(ctx, original); err != nil {
		return err
	}
	if err := worktree.RemoveCandidate(ctx, i.attempt.Source, i.attempt.Candidate); err != nil {
		return err
	}
	i.attempt.CleanupComplete = true
	return i.save()
}
