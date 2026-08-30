package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/ablation"
	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/runtimepolicy"
	"reasonix/internal/taskcontract"
)

// Final readiness: whether the fact contract and ledger allow the turn to stop.

type finalReadinessCheck struct {
	applies                    bool
	reason                     string
	continuationGeneric        bool
	continuationHighConfidence bool
	continuationUnsafe         bool
	missingProjectChecks       int
	incompleteTodos            int
	missingAcceptanceCriteria  int
	missingVerification        int
	missingReview              int
	missingSignoff             int
	missingActionEvidence      int
	missingMutation            int
	missingCapabilities        int
	incompleteTodoItems        []evidence.TodoStepMatch
}

func (c finalReadinessCheck) continuationClass() ReadinessContinuationClass {
	if c.reason == "" || c.continuationUnsafe || c.missingActionEvidence > 0 || c.missingMutation > 0 ||
		c.missingCapabilities > 0 {
		return ReadinessContinuationNone
	}
	if c.continuationHighConfidence {
		return ReadinessContinuationHighConfidence
	}
	if c.continuationGeneric {
		return ReadinessContinuationGeneric
	}
	return ReadinessContinuationNone
}

func (c *finalReadinessCheck) observeObligation(o taskcontract.Obligation) {
	if o.Enforcement != taskcontract.EnforcementAdvisory {
		switch o.Kind {
		case taskcontract.ObligationActionReceipt:
			// Repeating an external/destructive action to manufacture a
			// receipt is never an automatic readiness operation.
			c.continuationUnsafe = true
		case taskcontract.ObligationTodo, taskcontract.ObligationCriteria,
			taskcontract.ObligationFullVerify, taskcontract.ObligationIndependentReview,
			taskcontract.ObligationSecurityReview, taskcontract.ObligationSignoff:
			switch {
			case o.Kind == taskcontract.ObligationTodo || o.Kind == taskcontract.ObligationCriteria:
				c.continuationHighConfidence = true
			case o.Enforcement == taskcontract.EnforcementStrict:
				c.continuationHighConfidence = true
			case o.Enforcement == taskcontract.EnforcementRecoverable:
				c.continuationGeneric = true
			}
		case taskcontract.ObligationTargetedVerify, taskcontract.ObligationDiffReview:
			switch o.Enforcement {
			case taskcontract.EnforcementStrict:
				c.continuationHighConfidence = true
			case taskcontract.EnforcementRecoverable:
				c.continuationGeneric = true
			}
		default:
			c.continuationUnsafe = true
		}
	}
	switch o.Kind {
	case taskcontract.ObligationTargetedVerify, taskcontract.ObligationFullVerify:
		c.missingVerification++
	case taskcontract.ObligationDiffReview, taskcontract.ObligationIndependentReview, taskcontract.ObligationSecurityReview:
		c.missingReview++
	case taskcontract.ObligationSignoff:
		c.missingSignoff++
	case taskcontract.ObligationActionReceipt:
		c.missingActionEvidence++
	case taskcontract.ObligationCriteria:
		c.missingAcceptanceCriteria++
	}
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
	missing := make([]string, 0, 10)
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
	// Absorb wait/child receipts that arrived after the last commit. Do not
	// rebuild the whole contract here — that would re-impose an unused plan.
	if a.turn.engine != nil && a.task.ledger != nil {
		a.turn.engine.SyncReceipts(a.task.ledger.Receipts(), a.writeWorkspaceRoot, a.turn.constraints.ForbidTests)
	}
	var missing []string
	out := finalReadinessCheck{continuationUnsafe: a.turn.constraints.ForbidTests}
	if a.planMode.Load() {
		return out
	}
	incomplete, hasTodos := a.task.ledger.IncompleteLatestTodos()
	if a.closedLoopActive() && !hasTodos && a.task.ledger.HasAnySuccessfulReceipt() {
		incomplete, hasTodos = a.incompleteCanonicalTodos()
	}
	if msg := a.capabilityGateFailure(); msg != "" {
		out.applies = true
		out.continuationUnsafe = true
		out.missingCapabilities++
		missing = append(missing, msg)
	}
	writer, hasWriter := a.task.ledger.LatestSuccessfulWriterIndex()
	if mutation, ok := a.task.ledger.LatestSuccessfulMutationIndex(); ok {
		writer, hasWriter = mutation, true
	}
	if a.closedLoopActive() && hasWriter && hasTodos && len(incomplete) > 0 {
		out.applies = true
		out.continuationHighConfidence = true
		out.incompleteTodos = len(incomplete)
		out.incompleteTodoItems = append([]evidence.TodoStepMatch(nil), incomplete...)
		missing = append(missing, finalReadinessIncompleteTodos(incomplete))
	}
	for _, check := range a.projectChecks {
		if !hasWriter {
			break
		}
		command := strings.TrimSpace(check.Command)
		if command == "" {
			continue
		}
		if !a.task.ledger.HasSuccessfulCommandAfter(command, writer) {
			out.continuationHighConfidence = true
			out.missingProjectChecks++
			missing = append(missing, fmt.Sprintf("run %q from %s after the latest write", command, finalReadinessCheckSource(check)))
		}
	}
	if hasWriter {
		outstanding := a.outstandingPlanCriteria()
		if len(outstanding) > 0 {
			out.continuationHighConfidence = true
		}
		out.missingAcceptanceCriteria += len(outstanding)
		missing = append(missing, outstanding...)
	}

	stop := runtimepolicy.StopDecision{Disposition: taskcontract.StopReady}
	if a.turn.engine != nil {
		stop = a.turn.engine.BeforeStop(runtimepolicy.StopContext{
			GoalActive:     a.turn.deliveryScopeActive,
			ApprovedPlan:   a.planContractSnapshot() != nil,
			IncompleteTodo: a.closedLoopActive() && hasTodos && len(incomplete) > 0,
			Opts: taskcontract.StopOptions{
				LoopGuard:        a.loopGuardAllowsFinal(),
				EnvUnavailable:   a.turn.constraints.ForbidTests,
				PermissionDenied: false,
			},
		})
		for _, o := range a.turn.engine.Snapshot().Unsatisfied() {
			missing = append(missing, obligationGap(o))
			out.observeObligation(o)
		}
	}
	if a.loopGuardAllowsFinal() {
		return finalReadinessCheck{applies: true}
	}
	if out.incompleteTodos > 0 && stop.Disposition == taskcontract.StopReady {
		// Incomplete todos are a fact contradiction, not an advisory gap.
		stop.Disposition = taskcontract.StopContinue
	}
	if len(missing) == 0 && stop.Disposition == taskcontract.StopReady {
		return out
	}
	out.applies = true
	switch stop.Disposition {
	case taskcontract.StopReady:
		if a.loopGuardAllowsFinal() {
			return out
		}
		if out.incompleteTodos == 0 && a.turn.engine != nil && len(a.turn.engine.Snapshot().AdvisoryGaps()) > 0 {
			return out
		}
	case taskcontract.StopPartial:
		out.reason = strings.Join(missing, "; ")
		return a.applyPartialCheckWaiver(out)
	case taskcontract.StopBlocked:
		out.continuationUnsafe = true
		out.reason = strings.Join(missing, "; ")
		return out
	case taskcontract.StopContinue:
		if a.loopGuardAllowsFinal() {
			return out
		}
		if a.turn.engine != nil {
			a.turn.engine.NoteRecoveryAttempt()
		}
		out.reason = strings.Join(missing, "; ")
		if !a.closedLoopActive() {
			return a.applyPartialCheckWaiver(out)
		}
		return out
	}
	if a.loopGuardAllowsFinal() {
		return out
	}
	out.reason = strings.Join(missing, "; ")
	return a.applyPartialCheckWaiver(out)
}

func obligationGap(o taskcontract.Obligation) string {
	switch o.Kind {
	case taskcontract.ObligationTargetedVerify, taskcontract.ObligationFullVerify:
		return "run relevant verification after the latest mutation"
	case taskcontract.ObligationDiffReview:
		return "inspect the changed result after the latest mutation (read the touched file or run git diff/status)"
	case taskcontract.ObligationIndependentReview:
		return "run an independent review after the latest mutation"
	case taskcontract.ObligationSecurityReview:
		return "run a security review after the latest mutation"
	case taskcontract.ObligationSignoff:
		return "call complete_step after the latest mutation"
	case taskcontract.ObligationActionReceipt:
		return "record a successful action receipt for this operation"
	case taskcontract.ObligationCriteria:
		return "establish concrete acceptance criteria before changing state"
	case taskcontract.ObligationTodo:
		return "keep an in-progress todo for this write"
	default:
		return string(o.Kind)
	}
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
