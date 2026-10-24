package isolation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
)

// IntegrationInfo includes unreadable records as diagnostics; a listing must
// not make retained results disappear merely because a version is unknown.
type IntegrationInfo struct {
	ID       string              `json:"id"`
	Attempt  *IntegrationAttempt `json:"attempt,omitempty"`
	Active   bool                `json:"active"`
	Recovery string              `json:"recovery,omitempty"`
}

func (s *Store) IntegrationAttempts() []IntegrationInfo {
	result := []IntegrationInfo{}
	if s == nil {
		return result
	}
	paths, _ := filepath.Glob(filepath.Join(s.integrationRoot(), "int_*.json"))
	for _, path := range paths {
		id := strings.TrimSuffix(filepath.Base(path), ".json")
		info := IntegrationInfo{ID: id}
		attempt, _, err := s.readIntegration(id)
		if err != nil {
			info.Recovery = err.Error()
		} else {
			if attempt.State != Integrated && attempt.Verification == "valid" && !s.validIntegrationChecks(attempt) {
				attempt.Verification, attempt.Reason = "unknown", "verification environment ownership is unavailable; recheck required"
			}
			info.Attempt = &attempt
		}
		release, lockErr := filelock.TryAcquire(filepath.Join(s.integrationRoot(), id+".lock"))
		if lockErr == nil {
			release()
		} else if errors.Is(lockErr, filelock.ErrHeld) {
			info.Active = true
		} else {
			info.Recovery = lockErr.Error()
		}
		result = append(result, info)
	}
	return result
}

// Discard is an explicit decision about this attempt, never about the original
// result. A source journal must be settled before its candidate may be removed.
func (i *Integration) Discard(ctx context.Context) error {
	return i.discard(ctx, "")
}

func (i *Integration) discard(ctx context.Context, revision string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.requireOpen(); err != nil {
		return err
	}
	if i.attempt.State == Integrated {
		return tool.Refusal{Code: CodeAlreadySettled, Message: "integration attempt is already integrated"}
	}
	if i.attempt.State == Applying || i.attempt.State == RecoveryRequired {
		original, err := i.store.operationSnapshot(ctx, i.attempt.ResultID)
		if err != nil {
			return err
		}
		if original.State == Applying {
			return tool.Refusal{Code: CodeRecoveryRequired, Message: "settle the source integration journal and original result before discarding this attempt"}
		}
	}
	if i.attempt.State == Discarded && i.attempt.CleanupComplete {
		return nil
	}
	lease, err := i.store.mutations.Acquire(mutation.WithoutLease(ctx), mutation.Intent{Effect: mutation.NamedPaths,
		Paths: []string{i.attempt.Candidate.WorktreeRoot, i.attempt.Source.Repo.CommonDir}, ExecutionID: i.attempt.ID, CallID: "isolation.integration.discard"})
	if err != nil {
		return err
	}
	defer lease.End(mutation.Unknown)
	if revision != "" && (i.attempt.State != Discarded || i.attempt.DiscardApproval != revision) {
		preview, content, err := i.inspectDiscard(ctx)
		if err != nil {
			return err
		}
		if preview.Revision != revision {
			return tool.Refusal{Code: CodeStaleDiscard, Message: "integration candidate changed after discard preview; files retained"}
		}
		i.attempt.DiscardContent, i.attempt.DiscardApproval = &content, revision
	} else if i.attempt.State != Discarded {
		_, content, err := i.inspectDiscard(ctx)
		if err != nil {
			return err
		}
		i.attempt.DiscardContent = &content
	}
	i.attempt.State = Discarded
	if err := i.save(); err != nil {
		return err
	}
	current := evidence.CaptureContentSnapshot(ctx, i.attempt.Candidate.WorktreeRoot, 50_000)
	if i.attempt.DiscardContent == nil || !sameContent(*i.attempt.DiscardContent, current) {
		if _, err := os.Lstat(i.attempt.Candidate.WorktreeRoot); !errors.Is(err, os.ErrNotExist) {
			return tool.Refusal{Code: CodeStaleDiscard, Message: "integration candidate changed after discard decision; obtain a new preview"}
		}
	}
	if err := worktree.RemoveCandidate(lease.Context(ctx), i.attempt.Source, i.attempt.Candidate); err != nil {
		return err
	}
	i.attempt.CleanupComplete = true
	return i.save()
}
