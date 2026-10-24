package isolation

import (
	"reflect"
	"slices"

	"reasonix/internal/contract/tool"
)

func checkPersistentTransition(before durableEntry, after *Entry) (bool, error) {
	if !supportedResultVersion(before) || !supportedLifecycle(before) {
		return false, tool.Refusal{Code: CodeUnsupportedState, Message: "isolated result was written by a newer protocol"}
	}
	pinning := preparationPinsIdentity(before, after)
	if before.State != Preparing && (!reflect.DeepEqual(before.Environment, after.Environment) || !reflect.DeepEqual(before.Baseline, after.Baseline)) {
		return false, tool.Refusal{Code: CodeUnsupportedState, Message: "prepared environment receipt changed; record retained"}
	}
	if before.ID != after.ID || before.Workspace != after.Workspace || before.Snap != after.Snap ||
		(before.Cand != after.Cand && !pinning) || before.ResumeOwner != after.ResumeOwner || before.DiscardPolicy != after.DiscardPolicy || !reflect.DeepEqual(before.Origin, after.Origin) {
		return false, tool.Refusal{Code: CodeUnsupportedState, Message: "isolated result identity changed; record retained"}
	}
	if (settleable(before.State) || terminalState(before.State)) && (before.Tree != after.Tree || !slices.Equal(before.Changes, after.Changes)) {
		return false, tool.Refusal{Code: CodeRecoveryRequired, Message: "sealed isolated manifest changed; original record retained"}
	}
	if terminalState(before.State) && (before.State != after.State || before.RunGeneration != after.RunGeneration ||
		(before.CleanupComplete && (!after.CleanupComplete || before.DiscardApproval != after.DiscardApproval || !reflect.DeepEqual(before.DiscardContent, after.DiscardContent))) || !slices.Equal(before.Stats, after.Stats) || !reflect.DeepEqual(before.Evidence, after.Evidence)) {
		return false, tool.Refusal{Code: CodeAlreadySettled, Message: "settled isolated identity cannot be rewritten"}
	}
	return pinning, nil
}
