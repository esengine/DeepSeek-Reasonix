package agent

import (
	"context"

	"reasonix/internal/safety/evidence"
)

// applyShellShapeGates holds the deterministic command-shape contract: shapes a
// later segment could use to hide an earlier failure, and the shape a declared
// check needs for its exit status to mean anything. Ordinary mode blocks only
// the hiding shapes; Delivery blocks the wider class. What the host merely
// cannot audit is not blocked at all — it is recorded unproven and owed.
func (a *Agent) applyShellShapeGates(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	// PowerShell 5.1 &&/|| is enforced inside the bash tool itself, so descriptor
	// and error text stay shell-accurate; only command shape is decided here.
	if plan.evidenceName == "bash" {
		// A declared check is read off its exit status, so something has to
		// answer for it: the whole line, or the pipe-status probe the mixed-shape
		// gate below also takes. Neither records a verdict the shell never gave.
		if declared := bashDeclaredCheck(plan.evidenceArgs); declared != "" &&
			!evidence.WholeCommandExitConclusive(bashCommandFromArgs(plan.evidenceArgs)) &&
			!hostReadsCheckThroughPipeStatus(plan.evidenceArgs) {
			return toolOutcome{
				output:    declaredCheckShapeMessage(bashCommandFromArgs(plan.evidenceArgs)),
				blocked:   true,
				errMsg:    "blocked: declared check has an inconclusive exit status",
				execution: shellPreflightExecution(plan, true),
			}, true
		}
		if evidence.BashToolCallMasksVerificationExit(plan.evidenceArgs) {
			msg := evidence.ShellContractPreflightMessage("mask_exit")
			if a.deliveryProfile {
				msg = "blocked: the trailing echo/printf of $? masks the verifier's exit status, so this command would look successful even when the check failed. Run the verifier or read-only extraction pipeline by itself and let its exit status be the tool result; for example: tail ... | head ... | node --check -"
			}
			return toolOutcome{
				output:    msg,
				blocked:   true,
				errMsg:    "blocked: verification exit status masked",
				execution: shellPreflightExecution(plan, true),
			}, true
		}
		mixedShape := evidence.BashToolCallMixesMutationAndVerification(plan.evidenceArgs)
		if !a.deliveryProfile {
			mixedShape = evidence.BashToolCallMixesMutationAndMaskableVerification(plan.evidenceArgs) &&
				!hostReadsCheckThroughPipeStatus(plan.evidenceArgs)
		}
		if mixedShape && !a.mixedShapeClearedByEscalation(ctx, plan) {
			msg := evidence.ShellContractMixedMessage(plan.evidenceArgs)
			if a.deliveryProfile {
				msg = evidence.ShellContractMixedDeliveryMessage(plan.evidenceArgs)
			}
			return toolOutcome{
				output:    msg,
				blocked:   true,
				errMsg:    "blocked: mixed mutation and verification command",
				execution: shellPreflightExecution(plan, true),
			}, true
		}
		if evidence.BashToolCallUsesNonTerminalInlineInterpreter(plan.evidenceArgs) {
			msg := evidence.ShellContractInlineNonTerminalMessage(plan.evidenceArgs)
			return toolOutcome{
				output:    msg,
				blocked:   true,
				errMsg:    "blocked: non-terminal inline interpreter command",
				execution: shellPreflightExecution(plan, false),
			}, true
		}
	}
	return toolOutcome{}, false
}

// declaredCheckShapeMessage names the shape that actually stopped the call. A
// message that guesses at a ';' the command never had leaves the model editing
// the wrong thing, which one run spent four rounds doing.
func declaredCheckShapeMessage(command string) string {
	if !evidence.CommandExitShapeReadable(command) {
		return "blocked: this call declares a check through `verifies`, but the host cannot statically read what its exit status proves — a subshell, a here-doc, a background job, or a negation puts the verdict out of reach. " +
			"Run the check itself as its own call, with the setup it needs done in an earlier call, or drop `verifies` and run it as an ordinary command."
	}
	return "blocked: this call declares a check through `verifies`, but its exit status would not answer for the whole command — a ';', a pipe, or '||' hands the status to a later stage. " +
		"Run the check as one command or an '&&' chain so a zero exit proves every segment, or drop `verifies` and run it as an ordinary command."
}
