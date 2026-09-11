package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/i18n"
	"reasonix/internal/taskcontract"
	"reasonix/internal/tool"
)

// maxCitableReceipts bounds how many host receipt IDs a rejection lists. The
// point is to let the model pick one, not to replay the turn.
const maxCitableReceipts = 6

// operations is the turn's operation lifecycle, or nil when no ledger is
// active (sub-agents and read-only probes run without one).
func (a *Agent) operations() *evidence.OperationLedger {
	if a == nil || a.task.ledger == nil {
		return nil
	}
	return a.task.ledger.Operations()
}

// operationID is the host's stable identity for what this call intends to do.
// It is derived from the real target and its arguments, never from the
// provider's per-round call ID, so a retry of the same edit is the same
// operation and repeated failure is detectable at all.
func (p *toolCallPlan) operationID() string {
	if p == nil {
		return ""
	}
	if p.evidenceName != "" {
		return evidence.OperationID(p.evidenceName, p.evidenceArgs)
	}
	return evidence.OperationID(p.call.Name, json.RawMessage(p.call.Arguments))
}

// auditOperation reports one lifecycle counter. It carries host identifiers
// only — the sink must never learn a path, an argument, or a command.
func (a *Agent) auditOperation(metric string, op evidence.Operation) {
	event.RecordOperationAudit(a.svc.sink, evidence.OperationAudit{
		Metric:          metric,
		OperationID:     op.ID,
		Tool:            op.Tool,
		State:           op.State,
		FailureCode:     op.FailureCode,
		RecoveryAttempt: op.RecoveryCount,
	})
}

// applyOperationGate refuses to run an operation the host already stopped
// automating. Without it the model can reissue the same rejected call under a
// new call ID forever; the guards downstream only notice after the repetition
// has already cost another provider round.
func (a *Agent) applyOperationGate(plan *toolCallPlan) (toolOutcome, bool) {
	ops := a.operations()
	if ops == nil || plan == nil {
		return toolOutcome{}, false
	}
	id := plan.operationID()
	op, ok := ops.Get(id)
	if !ok || op.State != evidence.OperationNeedsUser {
		return toolOutcome{}, false
	}
	d := &tool.OperationDiagnostic{
		Code:            tool.OperationNeedsUser,
		OperationID:     id,
		State:           string(op.State),
		Recovery:        "this operation is paused for the user; do not resubmit it",
		AllowedRecovery: []string{tool.RecoveryAbandonEdit},
	}
	if len(op.TargetPaths) > 0 {
		d.Path = op.TargetPaths[0]
	}
	a.auditOperation(evidence.MetricOperationDuplicateBlock, op)
	msg := fmt.Sprintf("blocked: [operation paused] %s already failed the same way twice (%s); the host will not resubmit it. Continue with other work or report it to the user.",
		id, op.FailureCode)
	if recovery := d.ModelFacing(); recovery != "" {
		msg += "\n" + recovery
	}
	return toolOutcome{output: msg, blocked: true, errMsg: firstLine(msg), diagnostic: d}, true
}

// noteOperationFailure records one rejection against the operation and fills
// the diagnostic with the closed set of recoveries the host will accept. The
// model chooses an action instead of guessing another wording of the same call.
func (a *Agent) noteOperationFailure(operationID, code string, d *tool.OperationDiagnostic) evidence.RecoveryDecision {
	ops := a.operations()
	if ops == nil || operationID == "" || code == "" {
		return evidence.RecoveryDecision{Retryable: true}
	}
	decision := ops.Fail(operationID, code)
	op, _ := ops.Get(operationID)
	metric := evidence.MetricOperationRecoveryAttempt
	if decision.State == evidence.OperationNeedsUser {
		metric = evidence.MetricOperationNeedsUser
	}
	a.auditOperation(metric, op)
	if d == nil {
		return decision
	}
	d.OperationID = operationID
	d.State = string(decision.State)
	d.Retryable = decision.Retryable
	d.RetryBudget = decision.Budget
	if len(d.AllowedRecovery) == 0 {
		d.AllowedRecovery = allowedRecoveryFor(code, decision)
	}
	if len(d.AvailableReceipts) == 0 {
		d.AvailableReceipts = a.citableReceiptIDs()
	}
	return decision
}

// allowedRecoveryFor maps a failure code onto the actions the host accepts. An
// exhausted budget offers only abandonment: the operation belongs to the user.
func allowedRecoveryFor(code string, decision evidence.RecoveryDecision) []string {
	if !decision.Retryable {
		return []string{tool.RecoveryAbandonEdit}
	}
	switch code {
	case tool.WriteEvidenceStale, tool.WriteEvidenceMissing, tool.ReadSourceChanged:
		return []string{tool.RecoveryRereadTarget, tool.RecoveryAbandonEdit}
	case tool.VerificationReceiptMissing, tool.VerificationReceiptMismatch:
		return []string{tool.RecoveryRunVerifier, tool.RecoveryMarkManual}
	default:
		return []string{tool.RecoveryAbandonEdit}
	}
}

func (a *Agent) citableReceiptIDs() []string {
	if a == nil || a.task.ledger == nil {
		return nil
	}
	refs := a.task.ledger.CitableReceipts(maxCitableReceipts)
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.ID)
	}
	return out
}

// recordOperationOutcome moves the operation the way the real tool result
// moved the world. Nothing here reads a model claim: a mutation that succeeded
// is applied, a recognized verifier that passed settles the change it covers,
// and an error is a bounded failure against this exact operation.
func (a *Agent) recordOperationOutcome(plan *toolCallPlan, rec evidence.Receipt, err error) {
	ops := a.operations()
	if ops == nil || plan == nil || rec.OperationID == "" {
		return
	}
	ops.Open(rec.OperationID, rec.ToolName, rec.Paths)
	if err != nil {
		// Only a host rejection spends recovery budget. A tool that ran and
		// reported a real failure — a test that fails twice while the fix is
		// still in progress — is information, not a loop, and stays with the
		// repeat-failure and storm guards.
		var operationErr *tool.OperationError
		if errors.As(err, &operationErr) {
			a.noteOperationFailure(rec.OperationID, operationErr.Diagnostic.Code, nil)
		}
		return
	}
	ref := rec.Ref()
	switch {
	case rec.Mutation || rec.Write:
		ops.Apply(rec.OperationID, ref)
		// Ordinary work settles on the real result. Only the Delivery floor
		// holds a change open for verification and review, so a routine edit
		// never becomes a bookkeeping task the model has to clear.
		if a.turn.constraints.PolicyFloor != taskcontract.PolicyFloorDelivery {
			a.auditOperation(evidence.MetricOperationSettled, ops.Settle(rec.OperationID))
		}
		a.advanceTodoForOperation(rec)
	case ref.Kind == evidence.ReceiptKindVerification || ref.Kind == evidence.ReceiptKindReview:
		if covered, attached := ops.AttachLatestVerification(ref); attached {
			a.auditOperation(evidence.MetricVerificationAutoAttached, covered)
		} else {
			a.auditOperation(evidence.MetricOperationSettled, ops.Settle(rec.OperationID))
		}
	case ref.Kind == evidence.ReceiptKindCommand:
		// A successful command the host does not recognize as a verifier. It
		// settles like any other real result; the counter is how often the
		// classifier is simply not the one deciding.
		a.auditOperation(evidence.MetricVerificationUnclassified, ops.Settle(rec.OperationID))
	default:
		a.auditOperation(evidence.MetricOperationSettled, ops.Settle(rec.OperationID))
	}
	if rec.ToolName == "complete_step" && !rec.StepProof {
		a.auditOperation(evidence.MetricCompleteStepOptionalCall, evidence.Operation{ID: rec.OperationID, Tool: rec.ToolName})
	}
}

// applyOperationBreaker reports operations the host just stopped automating.
// It fires once per operation: repeating the notice every round would be the
// same repetition the breaker exists to end.
func (a *Agent) applyOperationBreaker(receiptMark int) intervention {
	ops := a.operations()
	if ops == nil {
		return intervention{}
	}
	stopped := ops.TakeNewlyNeedsUser()
	if len(stopped) == 0 {
		return intervention{}
	}
	var lines []string
	for _, op := range stopped {
		line := fmt.Sprintf("%s (%s)", op.ID, op.FailureCode)
		if len(op.TargetPaths) > 0 {
			line += " targeting " + strings.Join(slashPaths(op.TargetPaths), ", ")
		}
		lines = append(lines, line)
	}
	guard := fmt.Sprintf(
		"[operation paused] %s failed the same way twice and is now the user's decision. The host will not resubmit it, and re-sending it with reworded arguments will be refused. Continue with unrelated work, or end the turn and report exactly what is unfinished.",
		strings.Join(lines, "; "))
	a.armLoopGuardPass(receiptMark)
	return intervention{
		verdict:  verdictLand,
		guidance: guard,
		notice:   noticeFor(event.NoticeCodeOperationNeedsUser, event.LevelWarn, i18n.M.OperationNeedsUser, "operation breaker: "+strings.Join(lines, "; ")),
	}
}

// appendReceiptCitation hands the model the host's ID for what just happened.
// Without it a later completion has to retype the command it ran and the host
// has to match that text — the exact matching that rejected real work whenever
// a shell prefix, quote style, or working directory differed.
func appendReceiptCitation(result string, rec evidence.Receipt) string {
	if rec.ID == "" || !rec.Success {
		return result
	}
	switch rec.Kind() {
	case evidence.ReceiptKindMutation, evidence.ReceiptKindVerification, evidence.ReceiptKindCommand, evidence.ReceiptKindReview:
	case evidence.ReceiptKindRead:
		// Only a file read produces a versioned window, so only that read has a
		// source token to cite. Other read-shaped tools get nothing: an id no
		// writer can use is pure prompt weight.
		if rec.ToolName != "read_file" {
			return result
		}
		return strings.TrimRight(result, "\n") + "\n[source_token " + rec.ID + "]"
	default:
		return result
	}
	return strings.TrimRight(result, "\n") + "\n[receipt " + rec.ID + "]"
}

// Readiness gap actions. They are identifiers the frontend maps to a control,
// not sentences for the model to interpret.
const (
	readinessActionContinueVerification = "continue_verification"
	readinessActionResolveWithUser      = "resolve_with_user"
)

// readinessOperationGaps reports the changes the host observed but could not
// settle. It is the delivery gap list: one entry per real change, produced
// once for the user to decide on rather than fed back to the model.
func (a *Agent) readinessOperationGaps() []ReadinessOperationGap {
	ops := a.operations()
	if ops == nil {
		return nil
	}
	var out []ReadinessOperationGap
	for _, op := range append(ops.Unsettled(), ops.NeedsUser()...) {
		action := readinessActionContinueVerification
		if op.State == evidence.OperationNeedsUser {
			action = readinessActionResolveWithUser
		}
		out = append(out, ReadinessOperationGap{OperationID: op.ID, Paths: op.TargetPaths, State: string(op.State), Action: action})
	}
	return out
}

// describeReadinessGaps renders the gap list for the one report the user sees.
func describeReadinessGaps(gaps []ReadinessOperationGap) string {
	if len(gaps) == 0 {
		return ""
	}
	parts := make([]string, 0, len(gaps))
	for _, gap := range gaps {
		part := gap.OperationID + " (" + gap.State + " → " + gap.Action + ")"
		if len(gap.Paths) > 0 {
			part += " " + strings.Join(slashPaths(gap.Paths), ", ")
		}
		parts = append(parts, part)
	}
	return "unsettled operations: " + strings.Join(parts, "; ")
}

// slashPaths renders host paths slash-canonically. Display text reaches the
// model and the user, and a backslash-separated path reads as an escape to
// both; the structured fields keep the native separator for opening a file.
func slashPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, filepath.ToSlash(path))
	}
	return out
}
