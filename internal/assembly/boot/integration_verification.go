package boot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"reasonix/internal/contract/event"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/instruction"
)

func verifyAndIntegrate(ctx context.Context, parent Options, store *isolation.Store, posture *isolationPosture, source string, spec delegation.ProfileExecSpec, entry *isolation.Entry, sink event.Sink, authorize isolation.SourceAuthorizer, checks []instruction.VerifyCheck) (result string, resultErr error) {
	if !parent.frozenConfig.AutoIntegrationEnabled() {
		return fmt.Sprintf("Result %s retained for manual application; automatic integration is disabled for this runtime.", entry.ID), nil
	}
	defer func() {
		if p := recover(); p != nil {
			resultErr = fmt.Errorf("integration verification panicked; original result retained: %v", p)
		}
	}()
	ctx = isolation.WithPreparationCapacities(ctx, parent.DelegationScheduler.PhaseCapacity().Preparation)
	attempt, err := store.PrepareIntegration(ctx, entry.ID, entry.ResumeOwner)
	if err != nil {
		return "", err
	}
	defer attempt.Close()
	retained := fmt.Sprintf("Integration attempt %s retains the current-source candidate separately from result %s.", attempt.ID(), entry.ID)
	opts, err := unattendedOptions(parent, isolatedUsage{sink}, posture.mode(), source, spec.Worker.Model)
	if err != nil {
		return retained, err
	}
	task, mapped, closeRuntime, err := buildIntegrationRuntime(ctx, parent, opts, store, source, spec, attempt)
	if err != nil {
		return retained, err
	}
	defer closeRuntime()
	contracts := make([]isolation.CheckContract, 0, len(checks))
	for _, check := range checks {
		raw, _ := json.Marshal(check)
		digest := sha256.Sum256(raw)
		contracts = append(contracts, isolation.CheckContract{Identity: hex.EncodeToString(digest[:]), CommandDigest: evidence.CheckCommandDigest(check.Command)})
	}
	if err := attempt.BeginChecks(ctx, contracts); err != nil {
		return retained, err
	}
	results, runErr := task.RunHostChecks(ctx, mapped, checks, sink, attempt.Status().Candidate.WorktreeRoot)
	observations := make([]isolation.CheckEvidence, 0, len(results))
	for index, result := range results {
		observations = append(observations, isolation.CheckEvidence{ExecutionID: result.ExecutionID,
			Contract: contracts[index].Identity, CommandDigest: contracts[index].CommandDigest, Success: result.VerificationSucceeded(), Inputs: result.Inputs})
	}
	if err := attempt.FinishChecks(ctx, observations); err != nil {
		return retained, err
	}
	if err := errors.Join(runErr, ctx.Err()); err != nil {
		return retained, err
	}
	if status := attempt.Status(); status.Verification != "valid" {
		return retained + " Verification is " + status.Verification + ": " + status.Reason + ". Source files were not changed.", nil
	}
	if err := attempt.Commit(ctx, authorize); err != nil {
		return retained, err
	}
	return fmt.Sprintf("Integration attempt %s applied result %s; verification belongs to that exact integrated snapshot.", attempt.ID(), entry.ID), nil
}

func buildIntegrationRuntime(ctx context.Context, parent, opts Options, store *isolation.Store, source string, spec delegation.ProfileExecSpec, attempt *isolation.Integration) (*delegation.TaskTool, delegation.ProfileExecSpec, func(), error) {
	return buildVerificationRuntime(ctx, parent, opts, store, source, spec, attempt.Status().Candidate, attempt.ID())
}

func buildVerificationRuntime(ctx context.Context, parent, opts Options, store *isolation.Store, source string, spec delegation.ProfileExecSpec, candidate worktree.Candidate, execution string) (*delegation.TaskTool, delegation.ProfileExecSpec, func(), error) {
	release, err := store.AdmitRuntimePreparation(ctx, execution)
	if err != nil {
		return nil, spec, nil, err
	}
	defer release()
	return buildIsolatedCandidate(ctx, parent, opts, source, candidate, spec)
}
