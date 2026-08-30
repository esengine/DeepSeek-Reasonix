package agent

import (
	"strings"

	"reasonix/internal/completion"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/plancontract"
	"reasonix/internal/taskcontract"
)

// buildShadowContract replays a finished turn's receipts into a task
// contract that observed everything and decided nothing. An approved plan is
// the contract's source of truth when there is one — its acceptance criteria
// are what the work agreed to, and todo titles are only a restatement of the
// steps. Without a plan the todo list stands in, as it always did.
func buildShadowContract(input string, receipts []evidence.Receipt, plan *plancontract.Plan, projectChecks ...instruction.VerifyCheck) *taskcontract.Contract {
	return buildShadowContractWithPolicy(input, receipts, plan, false, false, false, "", projectChecks...)
}

func buildShadowContractWithPolicy(
	input string,
	receipts []evidence.Receipt,
	plan *plancontract.Plan,
	goalActive bool,
	testsForbidden bool,
	requireFullVerification bool,
	workspaceRoot string,
	projectChecks ...instruction.VerifyCheck,
) *taskcontract.Contract {
	_ = input
	var todos []evidence.TodoItem
	for _, r := range receipts {
		if len(r.Todos) > 0 {
			todos = r.Todos
		}
	}
	var checks []string
	for _, check := range projectChecks {
		if command := strings.TrimSpace(check.Command); command != "" {
			checks = append(checks, command)
		}
	}
	var planPtr *taskcontract.PlanFacts
	if plan != nil {
		facts := planFacts(*plan)
		planPtr = &facts
	}
	return taskcontract.Rebuild(taskcontract.RebuildFacts{
		Plan:                    planPtr,
		Todos:                   todos,
		ProjectChecks:           checks,
		Receipts:                receipts,
		TestsForbidden:          testsForbidden,
		RequireFullVerification: requireFullVerification,
		WorkspaceRoot:           workspaceRoot,
		HasApprovedPlan:         plan != nil,
		HasActiveGoal:           goalActive,
	})
}

func contractShadowAudit(c *taskcontract.Contract) event.ContractShadowAudit {
	reqDone := 0
	for _, req := range c.Requirements {
		if req.Status == taskcontract.Satisfied {
			reqDone++
		}
	}
	checksDone := 0
	for _, check := range c.Checks {
		if check.Status == taskcontract.Satisfied {
			checksDone++
		}
	}
	return event.ContractShadowAudit{
		Intent:                "",
		Requirements:          len(c.Requirements),
		RequirementsSatisfied: reqDone,
		Checks:                len(c.Checks),
		ChecksSatisfied:       checksDone,
		Epoch:                 c.Epoch(),
		Verdict:               c.GoalVerdict().String(),
		Complete:              c.Complete(),
		ReadyToFinalize:       c.ReadyToFinalize(),
	}
}

// LiveContract is the contract as it stands right now: the same pure replay the
// turn ends with, run against the receipts recorded so far. Rebuilding beats
// keeping incremental state because one code path serves the per-round view and
// the end-of-turn record, so the two can never disagree.
func (a *Agent) LiveContract() *taskcontract.Contract {
	if a == nil || a.task.ledger == nil {
		return nil
	}
	return buildShadowContractWithPolicy(
		a.turn.turnInput,
		a.task.ledger.Receipts(),
		a.planContractSnapshot(),
		a.turn.deliveryScopeActive,
		a.turn.constraints.ForbidTests,
		a.turn.constraints.RequireFullVerification,
		a.writeWorkspaceRoot,
		a.projectChecks...,
	)
}

// observeContractRound records the contract after one tool round, so a
// trajectory carries how the evidence graph filled in rather than only where it
// landed. It observes; it decides nothing.
func (a *Agent) observeContractRound() {
	c := a.LiveContract()
	if c == nil || (len(c.Requirements) == 0 && len(c.Checks) == 0) {
		return
	}
	event.RecordContractShadow(a.svc.sink, contractShadowAudit(c))
}

// emitTurnShadows records the end-of-turn shadow observations: the contract's
// state, and the completion report derived from it. Both observe; neither
// decides.
func (a *Agent) emitTurnShadows(input string) {
	if a.task.ledger == nil {
		return
	}
	c := buildShadowContractWithPolicy(
		input,
		a.task.ledger.Receipts(),
		a.planContractSnapshot(),
		a.turn.deliveryScopeActive,
		a.turn.constraints.ForbidTests,
		a.turn.constraints.RequireFullVerification,
		a.writeWorkspaceRoot,
		a.projectChecks...,
	)
	// Prefer the live contract when present so Suppressed/Partial state is not
	// lost in the pure replay path.
	if live := a.LiveContract(); live != nil && (live.HasSuppressed() || len(live.Requirements) > 0 || len(live.Checks) > 0) {
		// Fold live statuses that the pure replay cannot reconstruct.
		for i := range c.Checks {
			for _, lc := range live.Checks {
				if c.Checks[i].Command == lc.Command && lc.Status == taskcontract.Suppressed {
					c.Checks[i].Status = taskcontract.Suppressed
					c.Checks[i].SuppressReason = lc.SuppressReason
				}
			}
		}
	}
	event.RecordContractShadow(a.svc.sink, contractShadowAudit(c))
	rep := completion.BuildAt(c, a.task.ledger, a.writeWorkspaceRoot, a.scratchRoots())
	a.turn.completion = &rep
	event.RecordCompletionReport(a.svc.sink, completionReportAudit(rep))
	a.emitCompletionSummary(c, rep)
}

// CompletionReceipt returns the turn's completion record for the host to
// deliver, or nil when the turn had nothing to judge. The host renders it; the
// agent never writes the user-facing text, which is the whole point.
func (a *Agent) CompletionReceipt() *event.CompletionReceipt {
	if a == nil || a.turn.completion == nil {
		return nil
	}
	return completionReceipt(*a.turn.completion)
}
