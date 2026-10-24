package boot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"reasonix/internal/runtime/checkenv"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/safety/evidence"
)

func (b *rootPublisher) checkRootExport(ctx context.Context, store *isolation.Store, parent Options, spec delegation.ProfileExecSpec, entry *isolation.Entry, mode string) ([]isolation.CheckEvidence, bool, error) {
	source := b.environment.Record().SourceWorkspace
	opts, err := unattendedOptions(parent, isolatedUsage{b.sink}, mode, source, spec.Worker.Model)
	if err != nil {
		return nil, false, err
	}
	task, mapped, closeRuntime, err := buildVerificationRuntime(ctx, parent, opts, store, source, spec, entry.Cand, entry.ID)
	if err != nil {
		return nil, false, err
	}
	defer closeRuntime()
	results, runErr := task.RunHostChecks(ctx, mapped, b.prompt.projectChecks, b.sink, entry.Cand.WorktreeRoot)
	checks := make([]isolation.CheckEvidence, 0, len(results))
	valid := runErr == nil && len(results) == len(b.prompt.projectChecks)
	for index, result := range results {
		raw, _ := json.Marshal(b.prompt.projectChecks[index])
		digest := sha256.Sum256(raw)
		checks = append(checks, isolation.CheckEvidence{ExecutionID: result.ExecutionID, Contract: hex.EncodeToString(digest[:]),
			CommandDigest: evidence.CheckCommandDigest(b.prompt.projectChecks[index].Command), Success: result.VerificationSucceeded(), Inputs: result.Inputs})
		if !result.VerificationSucceeded() || len(result.Inputs) != 1 {
			valid = false
			continue
		}
		input := result.Inputs[0]
		if !checkenv.Verify(input, result.ExecutionID) || !input.Completed || !input.Stable || len(input.Gaps) != 0 ||
			entry.Evidence == nil || entry.Evidence.FinishedContent == nil || !input.Before.Complete || !input.After.Complete ||
			input.Before.Digest != entry.Evidence.FinishedContent.Digest || input.After.Digest != entry.Evidence.FinishedContent.Digest {
			valid = false
		}
	}
	return checks, valid, runErr
}
