package isolation

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
)

// IntegrationAttempt is separate from the immutable original result. Its
// candidate starts from the current source, then receives the original delta.
// Unknown evidence never grants automatic publication authority.
type IntegrationAttempt struct {
	Version          int                          `json:"version"`
	ID               string                       `json:"id"`
	ResultID         string                       `json:"result_id"`
	ResultGeneration string                       `json:"result_generation"`
	ResultTree       string                       `json:"result_tree"`
	Owner            string                       `json:"owner"`
	State            State                        `json:"state"`
	Source           worktree.Snapshot            `json:"source"`
	SourceContent    evidence.ContentSnapshot     `json:"source_content"`
	SourceGit        worktree.GitInputs           `json:"source_git"`
	Candidate        worktree.Candidate           `json:"candidate"`
	Tree             string                       `json:"tree"`
	Changes          []worktree.Change            `json:"changes"`
	Content          evidence.ContentSnapshot     `json:"content"`
	Checks           []CheckEvidence              `json:"checks"`
	CheckContracts   []CheckContract              `json:"check_contracts"`
	Verification     string                       `json:"verification"`
	Reason           string                       `json:"reason"`
	CleanupComplete  bool                         `json:"cleanup_complete"`
	Environment      *worktree.EnvironmentReceipt `json:"prepared_environment,omitempty"`
	DiscardContent   *evidence.ContentSnapshot    `json:"discard_content,omitempty"`
	DiscardApproval  string                       `json:"discard_approval,omitempty"`
}

// Integration owns one attempt's OS lock. It holds no source-file lease while
// checks run. Close retains all records and candidate changes for recovery.
type Integration struct {
	store   *Store
	attempt IntegrationAttempt
	release func()
	mu      sync.Mutex
}

func (i *Integration) Close() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.release != nil {
		i.release()
		i.release = nil
	}
}

func (i *Integration) WorkspaceRoot() string { return i.attempt.Candidate.WorkspaceRoot }
func (i *Integration) ID() string            { return i.attempt.ID }

// PrepareIntegration takes a coordinated source snapshot and applies the
// original base-to-result delta only in the new candidate. A conflict never
// writes source files and never rewrites the original sealed manifest.
func (s *Store) PrepareIntegration(ctx context.Context, resultID, owner string) (prepared *Integration, resultErr error) {
	if s == nil || s.mutations == nil {
		return nil, errUnavailable
	}
	original, err := s.operationSnapshot(ctx, resultID)
	if err != nil {
		return nil, err
	}
	if !settleable(original.State) || original.legacy || original.ResumeOwner != owner {
		return nil, tool.Refusal{Code: CodeOwnerMismatch, Message: "integration requires an owned, sealed isolated result"}
	}
	snap, content, gitInputs, err := s.integrationSnapshot(ctx, original)
	if err != nil {
		return nil, err
	}
	candidate, err := worktree.PlanCandidate(snap, s.integrationRoot())
	if err != nil {
		return nil, err
	}
	id := "int_" + rand.Text()
	release, err := filelock.TryAcquire(filepath.Join(s.integrationRoot(), id+".lock"))
	if err != nil {
		return nil, err
	}
	i := &Integration{store: s, release: release, attempt: IntegrationAttempt{Version: 1, ID: id,
		ResultID: original.ID, ResultGeneration: original.RunGeneration, ResultTree: original.Tree,
		Owner: owner, State: Preparing, Source: snap, SourceContent: content, SourceGit: gitInputs, Candidate: candidate, Verification: "unknown"}}
	registered := false
	defer func() {
		if p := recover(); p != nil {
			resultErr = fmt.Errorf("integration preparation panicked: %v", p)
		}
		if prepared == nil {
			defer i.Close()
			if resultErr == nil {
				resultErr = errors.New("integration preparation did not transfer execution ownership")
			}
			if !registered {
				return
			}
			i.attempt.State, i.attempt.Reason = RecoveryRequired, resultErr.Error()
			var conflict *worktree.ConflictError
			if errors.As(resultErr, &conflict) {
				i.attempt.State = Conflict
			}
			resultErr = errors.Join(resultErr, i.save())
			resultErr = fmt.Errorf("integration attempt %s retained: %w", id, resultErr)
		}
	}()
	if s.integrationRegistrationProbe != nil {
		s.integrationRegistrationProbe(original)
	}
	if err := s.registerIntegration(ctx, original, i); err != nil {
		return nil, err
	}
	registered = true
	lease, err := s.mutations.Acquire(mutation.WithoutLease(ctx), mutation.Intent{Effect: mutation.NamedPaths,
		Paths: []string{candidate.WorktreeRoot, snap.Repo.CommonDir, snap.RepoRoot}, ExecutionID: id, CallID: "isolation.integration.prepare",
		Capacities: preparationCapacities(ctx)})
	if err != nil {
		return nil, err
	}
	defer lease.End(mutation.Unknown)
	ctx = lease.Context(ctx)
	if !sameContent(evidence.CaptureContentSnapshot(ctx, snap.RepoRoot, 50_000), content) {
		return nil, tool.Refusal{Code: CodeStaleExecution, Message: "source changed before integration environment preparation"}
	}
	i.attempt.Candidate, err = worktree.MaterializeCandidate(ctx, snap, candidate)
	if err != nil {
		return nil, err
	}
	i.attempt.Environment, err = worktree.PrepareEnvironment(ctx, snap, i.attempt.Candidate, s.environmentPaths, s.environmentDenied...)
	if err != nil {
		return nil, err
	}
	// The baseline is the original result's base, but the destination is the
	// fresh candidate. Conservative path comparison handles idempotent paths.
	base := original.Snap
	base.Repo, base.RepoRoot = i.attempt.Candidate.Repo, i.attempt.Candidate.WorktreeRoot
	if original.Origin != nil {
		if err := worktree.ValidatePublicationSourceModes(ctx, base, original.Origin.Materialization, original.Changes); err != nil {
			return nil, err
		}
	}
	if err := worktree.ApplyPaths(ctx, base, original.Tree, original.Changes); err != nil {
		return nil, err
	}
	i.attempt.Tree, i.attempt.Changes, err = worktree.CandidateTree(ctx, snap, i.attempt.Candidate)
	if err != nil {
		return nil, err
	}
	i.attempt.Content = evidence.CaptureContentSnapshot(ctx, i.attempt.Candidate.WorktreeRoot, 50_000)
	i.attempt.State = Pending
	if err := i.save(); err != nil {
		return nil, err
	}
	return i, nil
}

func (s *Store) integrationSnapshot(ctx context.Context, original *Entry) (snap worktree.Snapshot, content evidence.ContentSnapshot, gitInputs worktree.GitInputs, err error) {
	lease, err := s.mutations.Acquire(mutation.WithoutLease(ctx), mutation.Intent{Effect: mutation.NamedPaths,
		Paths: append(s.candidateResources(original), s.repo.WorkTree), ExecutionID: original.ID,
		CallID: "isolation.integration.snapshot", Capacities: preparationCapacities(ctx)})
	if err != nil {
		return snap, content, gitInputs, err
	}
	defer lease.End(mutation.NotExecuted)
	current, unlock, err := s.lockCurrent(lease.Context(ctx), original)
	if err != nil {
		return snap, content, gitInputs, err
	}
	defer unlock()
	if err := s.checkImportedPublication(current); err != nil {
		return snap, content, gitInputs, err
	}
	if !settleable(current.State) || current.Tree != original.Tree {
		return snap, content, gitInputs, tool.Refusal{Code: CodeStaleExecution, Message: "original result changed before integration preparation"}
	}
	if !onlyPublishableChanges(ctx, current) {
		return snap, content, gitInputs, tool.Refusal{Code: CodeRecoveryRequired, Message: "original result retains environment changes outside its Git manifest; reconcile or explicitly discard them before integration"}
	}
	before := evidence.CaptureContentSnapshot(ctx, s.repo.WorkTree, 50_000)
	gitBefore := worktree.CaptureGitInputs(ctx, s.repo)
	snap, err = s.gitSnapshot(ctx)
	if err != nil {
		return snap, before, gitBefore, err
	}
	after := evidence.CaptureContentSnapshot(ctx, s.repo.WorkTree, 50_000)
	gitInputs = worktree.CaptureGitInputs(ctx, s.repo)
	if !before.Complete || !after.Complete || before.Digest != after.Digest || !gitBefore.Complete || !gitInputs.Complete || gitBefore.Digest != gitInputs.Digest {
		return snap, after, gitInputs, tool.Refusal{Code: CodeStaleExecution, Message: "cannot establish stable complete source and Git input snapshots"}
	}
	return snap, after, gitInputs, nil
}

func (i *Integration) requireOpen() error {
	if i.release == nil {
		return fmt.Errorf("integration attempt %s is closed", i.attempt.ID)
	}
	return nil
}
