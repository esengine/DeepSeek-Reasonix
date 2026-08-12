package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/ablation"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/taskpolicy"
)

// Final readiness: whether a turn has earned the right to stop. It reads the
// evidence ledger, the delivery profile, and the approved plan's contract, and
// says what is missing rather than merely that something is.

type finalReadinessCheck struct {
	applies                   bool
	reason                    string
	missingProjectChecks      int
	incompleteTodos           int
	missingAcceptanceCriteria int
	missingVerification       int
	missingReview             int
	missingSignoff            int
	missingActionEvidence     int
	missingMutation           int
	missingCapabilities       int
}

func (c finalReadinessCheck) progressSignature() string {
	return fmt.Sprintf("%d/%d/%d/%d/%d/%d/%d/%d/%d/%d\x00%s",
		c.missingProjectChecks,
		c.incompleteTodos,
		c.missingAcceptanceCriteria,
		c.missingVerification,
		c.missingReview,
		c.missingSignoff,
		c.missingActionEvidence,
		c.missingMutation,
		c.missingCapabilities,
		boolInt(c.applies),
		c.reason,
	)
}

func (c finalReadinessCheck) missingIDs() []string {
	missing := make([]string, 0, 9)
	add := func(id string, count int) {
		if count > 0 {
			missing = append(missing, id)
		}
	}
	add("project_check", c.missingProjectChecks)
	add("todo", c.incompleteTodos)
	add("criteria", c.missingAcceptanceCriteria)
	add("verification", c.missingVerification)
	add("review", c.missingReview)
	add("signoff", c.missingSignoff)
	add("action", c.missingActionEvidence)
	add("mutation", c.missingMutation)
	add("capability", c.missingCapabilities)
	return missing
}

func (c finalReadinessCheck) audit(result evidence.ReadinessAuditResult, recovered bool) evidence.ReadinessAudit {
	return evidence.ReadinessAudit{
		Result:                    result,
		Recovered:                 recovered,
		MissingProjectChecks:      c.missingProjectChecks,
		IncompleteTodos:           c.incompleteTodos,
		CommandMismatchMissing:    c.missingProjectChecks,
		MissingAcceptanceCriteria: c.missingAcceptanceCriteria,
		MissingVerification:       c.missingVerification,
		MissingReview:             c.missingReview,
		MissingSignoff:            c.missingSignoff,
		MissingActionEvidence:     c.missingActionEvidence,
		MissingMutation:           c.missingMutation,
		MissingCapabilities:       c.missingCapabilities,
	}
}

func (a *Agent) finalReadinessCheckFor() finalReadinessCheck {
	if a.task.ledger == nil || a.ablation.Off(ablation.Evidence) {
		return finalReadinessCheck{}
	}
	var missing []string
	out := finalReadinessCheck{}
	// Planning returns a proposal; the controller owns approval and starts a
	// fresh execution turn, which is where delivery requirements belong. A
	// workflow boundary only — tool calls still take the usual permission path.
	if a.planMode.Load() {
		return out
	}
	{
		incomplete, hasTodos := a.task.ledger.IncompleteLatestTodos()
		if !hasTodos && a.task.ledger.HasAnySuccessfulReceipt() {
			incomplete, hasTodos = a.incompleteCanonicalTodos()
		}
		if hasTodos && len(incomplete) > 0 && a.task.ledger.HasSuccessfulTodoProgressReceipt() {
			out.applies = true
			out.incompleteTodos = len(incomplete)
			missing = append(missing, finalReadinessIncompleteTodos(incomplete))
		}
	}
	writer, hasWriter := a.task.ledger.LatestSuccessfulWriterIndex()
	deliveryMutation := false
	deliveryVerificationOnly := false
	checkpoint := a.task.checkpoint
	checkpointApplies := a.turn.deliveryScopeActive && checkpoint.ScopeID == a.task.scopeID
	if a.deliveryProfile {
		if mutation, ok := a.task.ledger.LatestSuccessfulMutationIndex(); ok {
			writer, hasWriter = mutation, true
			deliveryMutation = true
		} else if checkpointApplies && checkpoint.PendingMutation {
			// The mutation happened before a controller rebuild/restart. Treat it as
			// the baseline so this run can satisfy verification/review/sign-off
			// without manufacturing another write.
			writer, hasWriter = -1, true
			deliveryMutation = true
		} else if checkpointApplies && checkpoint.MutationObserved {
			deliveryMutation = true
		}
		workObserved := a.task.ledger.HasSuccessfulWorkReceipt() || (checkpointApplies && checkpoint.WorkObserved)
		if a.turn.deliveryTaskExpected && !a.turn.deliveryPersistentExpected && !workObserved {
			out.missingActionEvidence++
			missing = append(missing, "perform host-observable work for this technical task before answering")
		}
		if a.turn.deliveryPersistentExpected && !a.task.ledger.HasSuccessfulToolReceipt("remember") {
			out.missingMutation++
			missing = append(missing, "save the requested durable memory with the remember tool before answering")
		}
		if a.turn.deliveryMutationExpected && !deliveryMutation {
			out.missingMutation++
			missing = append(missing, "the request requires a state change, but no successful mutation was observed")
		}
		if !hasWriter && a.task.ledger.HasSuccessfulVerificationCommand() {
			writer, hasWriter = -1, true
			deliveryVerificationOnly = true
		}
		// Required/preferred capability gates apply before the no-writer fast
		// path below: a user-required Skill/MCP must not be skippable by
		// answering from ordinary reads alone.
		if msg := a.capabilityGateFailure(); msg != "" {
			out.applies = true
			out.missingCapabilities++
			missing = append(missing, msg)
		}
		if a.turn.deliveryPersistentExpected && !a.turn.deliveryMutationExpected && !a.task.ledger.HasSuccessfulMutationOtherThan("remember") {
			// A durable-memory-only request has its own concrete receipt contract.
			// It must not inherit code-delivery todo/test/diff/review ceremonies;
			// any unrelated mutation falls through to the full contract below.
			out.applies = true
			if len(missing) > 0 {
				out.reason = strings.Join(missing, "; ")
			}
			return out
		}
	}
	if !hasWriter {
		if len(missing) > 0 {
			if a.loopGuardAllowsFinal() {
				return out
			}
			out.reason = strings.Join(missing, "; ")
		}
		return out
	}
	if !a.deliveryProfile && a.turn.policySet && a.turn.policy.Verification >= taskpolicy.VerifyTargeted &&
		a.turn.policy.AllowsTests() && toolPresent(a.tools, "bash") &&
		!a.task.ledger.HasSuccessfulVerificationCommandAfter(writer) {
		out.applies = true
		out.missingVerification++
		missing = append(missing, "run a relevant verification command after the latest write for the current role setting")
	}
	hasProjectChecks := len(a.projectChecks) > 0
	hasTodoReceipt := a.task.ledger.HasSuccessfulTodoWrite()
	if !a.deliveryProfile && !hasProjectChecks && !hasTodoReceipt && len(missing) == 0 {
		return finalReadinessCheck{}
	}
	out.applies = true
	if a.deliveryProfile {
		a.emitTurnPhase(event.TurnPhaseVerifying)
		criteriaEstablished := a.turn.deliveryCriteriaEstablished || (checkpointApplies && checkpoint.CriteriaEstablished)
		if !criteriaEstablished {
			out.missingAcceptanceCriteria++
			missing = append(missing, "establish concrete acceptance criteria with todo_write before changing state")
		}
		hasCompleteStep := a.task.ledger.HasSuccessfulCompleteStepAfter(writer)
		if !hasCompleteStep {
			out.missingSignoff++
			missing = append(missing, "call complete_step after the latest mutation")
		}
		if !a.task.ledger.HasSuccessfulDeliverySignoffAfter(writer) {
			out.missingVerification++
			missing = append(missing, "run relevant verification after the latest mutation and cite that successful command in complete_step")
		}
		if deliveryMutation && !a.task.ledger.HasSuccessfulReviewAfter(writer) {
			out.missingReview++
			missing = append(missing, "inspect the changed result after the latest mutation (read the touched file or run git diff/status)")
		}
		if msg := a.deliveryReviewGateFailure(); msg != "" {
			out.missingReview++
			missing = append(missing, msg)
		}
		// The capability gate already ran before the no-writer fast path above.
	}
	for _, check := range a.projectChecks {
		if deliveryVerificationOnly {
			break
		}
		command := strings.TrimSpace(check.Command)
		if command == "" {
			continue
		}
		if !a.task.ledger.HasSuccessfulCommandAfter(command, writer) {
			out.missingProjectChecks++
			missing = append(missing, fmt.Sprintf("run %q from %s after the latest write", command, finalReadinessCheckSource(check)))
		}
	}

	// Before the loop-guard escape on purpose: a criterion the model cannot
	// prove must still be able to stop asking.
	outstanding := a.outstandingPlanCriteria()
	out.missingAcceptanceCriteria += len(outstanding)
	missing = append(missing, outstanding...)
	if len(missing) == 0 {
		return out
	}
	if a.loopGuardAllowsFinal() {
		return out
	}
	out.reason = strings.Join(missing, "; ")
	return out
}

func finalReadinessCheckSource(check instruction.VerifyCheck) string {
	source := strings.TrimSpace(check.SourcePath)
	if source == "" {
		source = "project memory"
	}
	if check.Line > 0 {
		return fmt.Sprintf("%s:%d", source, check.Line)
	}
	return source
}

func finalReadinessIncompleteTodos(items []evidence.TodoStepMatch) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		label := strings.TrimSpace(item.Content)
		if label == "" {
			label = fmt.Sprintf("todo %d", item.Index)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", label, item.Status))
	}
	return "latest successful todo_write still has incomplete items: " + strings.Join(parts, ", ")
}
