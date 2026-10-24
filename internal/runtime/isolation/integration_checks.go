package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/checkenv"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
)

const CodeUnverified = "isolation.unverified"

type CheckContract struct {
	Identity      string `json:"identity"`
	CommandDigest string `json:"command_digest"`
}

func (i *Integration) Status() IntegrationAttempt {
	i.mu.Lock()
	defer i.mu.Unlock()
	raw, _ := json.Marshal(i.attempt)
	var result IntegrationAttempt
	_ = json.Unmarshal(raw, &result)
	if result.State != Integrated && result.Verification == "valid" && !i.store.validIntegrationChecks(result) {
		result.Verification, result.Reason = "unknown", "verification environment ownership is unavailable; recheck required"
	}
	return result
}

// BeginChecks freezes the selected check identities before any command runs.
// No mutation lease escapes this method into model inference or a command wait.
func (i *Integration) BeginChecks(ctx context.Context, contracts []CheckContract) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.requireOpen(); err != nil {
		return err
	}
	if (i.attempt.State != Pending && i.attempt.State != Interrupted) || len(contracts) == 0 {
		return tool.Refusal{Code: CodeUnverified, Message: "integration needs a nonempty frozen check contract"}
	}
	lease, err := i.holdCandidate(ctx, "isolation.integration.check_inputs")
	if err != nil {
		return err
	}
	defer lease.End(mutation.NotExecuted)
	content := evidence.CaptureContentSnapshot(ctx, i.attempt.Candidate.WorktreeRoot, 50_000)
	if !sameContent(content, i.attempt.Content) {
		i.attempt.State, i.attempt.Verification, i.attempt.Reason = RecoveryRequired, "invalid", "integration candidate changed before checks"
		return errors.Join(tool.Refusal{Code: CodeStaleExecution, Message: i.attempt.Reason}, i.save())
	}
	i.attempt.State, i.attempt.Verification, i.attempt.Reason = Running, "unknown", "checks running"
	i.attempt.CheckContracts, i.attempt.Checks = slices.Clone(contracts), nil
	return i.save()
}

// FinishChecks derives validity from observed launch receipts and current
// content. Missing launch observations or environment coverage remain unknown.
func (i *Integration) FinishChecks(ctx context.Context, checks []CheckEvidence) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.requireOpen(); err != nil {
		return err
	}
	if i.attempt.State != Running {
		return tool.Refusal{Code: CodeStaleExecution, Message: "integration is not awaiting check completion"}
	}
	raw, _ := json.Marshal(checks)
	_ = json.Unmarshal(raw, &i.attempt.Checks)
	i.attempt.State, i.attempt.Verification, i.attempt.Reason = Pending, "unknown", "missing or incomplete check evidence"
	if err := ctx.Err(); err != nil {
		i.attempt.State, i.attempt.Reason = Cancelled, "checks cancelled; no verification claim retained"
		return errors.Join(err, i.save())
	}
	captureCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	lease, err := i.holdCandidate(captureCtx, "isolation.integration.check_receipt")
	if err != nil {
		i.attempt.Reason = "check input observation unavailable"
		return errors.Join(err, i.save())
	}
	defer lease.End(mutation.NotExecuted)
	content := evidence.CaptureContentSnapshot(captureCtx, i.attempt.Candidate.WorktreeRoot, 50_000)
	if !content.Complete {
		i.attempt.Reason = "check input observation incomplete: " + content.Reason
		return i.save()
	}
	if !sameContent(content, i.attempt.Content) {
		i.attempt.Verification, i.attempt.Reason = "invalid", "integration candidate changed during checks"
		return i.save()
	}
	if i.store.validIntegrationChecks(i.attempt) {
		i.attempt.Verification, i.attempt.Reason = "valid", ""
	}
	return i.save()
}

func (s *Store) validIntegrationChecks(attempt IntegrationAttempt) bool {
	if len(attempt.Checks) == 0 || len(attempt.Checks) != len(attempt.CheckContracts) {
		return false
	}
	for index, check := range attempt.Checks {
		contract := attempt.CheckContracts[index]
		if contract.Identity == "" || contract.CommandDigest == "" || check.Contract != contract.Identity ||
			check.CommandDigest != contract.CommandDigest || check.ExecutionID == "" || !check.Success || len(check.Inputs) != 1 {
			return false
		}
		input := check.Inputs[0]
		verify := checkenv.Verify
		if s.checkVerifier != nil {
			verify = s.checkVerifier
		}
		if !input.Completed || !input.Stable || !input.HasLaunchIdentity(attempt.Candidate.WorkspaceRoot) ||
			!verify(input, check.ExecutionID) ||
			input.CommandDigest != contract.CommandDigest || len(input.Gaps) != 0 ||
			!sameContent(input.Before, attempt.Content) || !sameContent(input.After, attempt.Content) {
			return false
		}
	}
	return true
}

func sameContent(a, b evidence.ContentSnapshot) bool {
	return a.Complete && b.Complete && a.Scope == b.Scope && a.Digest != "" && a.Digest == b.Digest
}

func (i *Integration) holdCandidate(ctx context.Context, call string) (*mutation.Lease, error) {
	return i.store.mutations.Acquire(mutation.WithoutLease(ctx), mutation.Intent{Effect: mutation.NamedPaths,
		Paths:       []string{i.attempt.Candidate.WorktreeRoot, i.attempt.Source.Repo.CommonDir},
		ExecutionID: i.attempt.ID, CallID: call, Capacities: preparationCapacities(ctx)})
}
