package boot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/instruction"
)

type isolatedVerification struct {
	store     *isolation.Store
	entry     *isolation.Entry
	task      *delegation.TaskTool
	spec      delegation.ProfileExecSpec
	checks    []instruction.VerifyCheck
	ledger    *evidence.Ledger
	sink      event.Sink
	close     func()
	integrate func(context.Context) (string, error)
}

func (v *isolatedVerification) finish(ctx context.Context, answer string, runErr error) (result string, finishErr error) {
	defer v.close()
	unverified := ""
	var checkEvidence []isolation.CheckEvidence
	checksPassed := false
	defer func() {
		if p := recover(); p != nil {
			runErr = errors.Join(runErr, fmt.Errorf("isolated verification panicked: %v", p))
		}
		runErr = errors.Join(runErr, ctx.Err())
		candidateEvidence := isolation.CaptureCandidateEvidence(v.entry, v.ledger.Summary())
		candidateEvidence.Checks = checkEvidence
		result, finishErr = v.store.Complete(ctx, v.entry, isolation.Outcome{Answer: answer, Unverified: unverified,
			Evidence: candidateEvidence}, runErr)
		if runErr == nil && finishErr == nil && checksPassed && len(v.entry.Changes) != 0 && v.integrate != nil {
			integrationResult, integrationErr := v.integrate(ctx)
			if integrationResult != "" {
				result += "\n\n" + integrationResult
			}
			if integrationErr != nil {
				result += "\nAutomatic integration retained its candidate: " + integrationErr.Error()
				finishErr = errors.Join(finishErr, integrationErr)
			}
		}
	}()
	if runErr == nil && !v.spec.Grant.ReadOnly && len(v.checks) != 0 {
		checks, checkErr := v.task.RunHostChecks(ctx, v.spec, v.checks, v.sink, v.entry.Cand.WorktreeRoot)
		for i, check := range checks {
			v.ledger.MergeChild(check.Evidence)
			raw, _ := json.Marshal(v.checks[i])
			digest := sha256.Sum256(raw)
			checkEvidence = append(checkEvidence, isolation.CheckEvidence{ExecutionID: check.ExecutionID,
				Contract: hex.EncodeToString(digest[:]), CommandDigest: evidence.CheckCommandDigest(v.checks[i].Command),
				Success: check.VerificationSucceeded(), Inputs: check.Inputs})
		}
		if checkErr != nil || len(checks) != len(v.checks) || slices.ContainsFunc(checks, func(r agent.HostExecutionResult) bool { return !r.VerificationSucceeded() }) {
			unverified = "Host-selected verification checks did not all complete successfully; the candidate is retained without verification."
			detail := ""
			if checkErr != nil {
				detail = checkErr.Error()
			}
			report(v.sink, event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: unverified, Detail: detail})
		} else {
			checksPassed = true
		}
	}
	return "", nil
}
