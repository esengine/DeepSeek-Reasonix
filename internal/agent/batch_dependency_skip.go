package agent

import (
	"encoding/json"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func (a *Agent) markDependencySkipped(calls []provider.ToolCall, outcomes []toolOutcome, results []string, durations []int64, start int, cause *mutationBarrierCause) {
	if cause != nil {
		a.mutationDependencyBarrier.CompareAndSwap(nil, cause)
	}
	cause = a.mutationDependencyBarrier.Load()
	for j := start; j < len(calls); j++ {
		if results[j] != "" {
			continue
		}
		// Pre-classify when statically certain. Proxies and ambiguous
		// targets fall through to run() so executeOne can resolve the real
		// target and re-apply the barrier before Commit/Execute.
		if !batchCallStaticallySkippable(a, calls[j]) {
			continue
		}
		isVerification := calls[j].Name == "bash" && evidence.IsVerificationCommand(bashCommandFromArgs(json.RawMessage(calls[j].Arguments)))
		msg := cause.message()
		var ex *tool.ShellExecution
		if calls[j].Name == "bash" {
			ex = &tool.ShellExecution{
				Kind:         "shell",
				State:        tool.ShellStateNotRun,
				FailurePhase: tool.ShellPhaseDependency,
				MutationRisk: tool.ShellMutationNotStarted,
				Verification: tool.ShellVerificationNotVerification,
			}
			if isVerification {
				ex.Verification = tool.ShellVerificationNotRun
			}
			if t, _, amb := a.svc.tools.ResolveCall(calls[j].Name); t != nil && len(amb) == 0 {
				if bt, ok := t.(tool.DetailedExecutor); ok {
					if desc := bt.ExecutionDescriptor(json.RawMessage(calls[j].Arguments)); desc != nil {
						ex.Shell = desc.Shell
						ex.ShellVersion = desc.ShellVersion
						ex.Platform = desc.Platform
						ex.SupportsAndAnd = desc.SupportsAndAnd
					}
				}
			}
		}
		results[j] = msg
		outcomes[j] = toolOutcome{
			output:    msg,
			blocked:   true,
			errMsg:    firstLine(msg),
			execution: ex,
		}
		durations[j] = 0
	}
}
