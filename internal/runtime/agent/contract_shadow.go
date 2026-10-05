package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/completion"
	"reasonix/internal/runtime/plancontract"
	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/safety/evidence"
)

// buildShadowContract replays a finished turn's receipts into a task
// contract that observed everything and decided nothing. An approved plan is
// the contract's source of truth when there is one — its acceptance criteria
// are what the work agreed to, and todo titles are only a restatement of the
// steps. Without a plan the todo list stands in, as it always did.
func buildShadowContract(input string, receipts []evidence.Receipt, plan *plancontract.Plan) *taskcontract.Contract {
	var c *taskcontract.Contract
	switch {
	case plan != nil:
		c = taskcontract.FromPlan(input, planFacts(*plan))
	case receiptsCarryMutation(receipts):
		// No plan stated the terms, but the turn changed something, so the ask
		// itself is the requirement and the change is what answers for it. The
		// trigger is the receipt: a turn that changed nothing owes no such claim.
		c = taskcontract.Atomic(input)
	default:
		c = taskcontract.New(input)
	}
	var todos []evidence.TodoItem
	for _, r := range receipts {
		if len(r.Todos) > 0 {
			todos = r.Todos
		}
	}
	// A todo is the model's own breakdown of the work, not a criterion anyone
	// accepted, so it is recorded without deciding completion, and marking it
	// done proves nothing. Unfinished todos are the readiness gate's to hold.
	if plan == nil {
		for i, todo := range todos {
			c.AddRequirement(fmt.Sprintf("t%d", i+1), todo.Content, false)
		}
	}
	for _, r := range receipts {
		c.Observe(r)
		resolveCitedCriteria(c, r)
		resolveBlockedCriteria(c, r)
	}
	return c
}

// receiptsCarryMutation reports whether the turn actually changed something.
func receiptsCarryMutation(receipts []evidence.Receipt) bool {
	for _, r := range receipts {
		if r.Success && (r.Mutation || r.Write) {
			return true
		}
	}
	return false
}

// resolveCitedCriteria satisfies the criteria a successful complete_step named.
// The tool verified each proof against the ledger before succeeding, so what the
// citation adds is the binding: "the command ran" and "the criterion holds" are
// different claims, and only the model knows which proof was for which.
func resolveCitedCriteria(c *taskcontract.Contract, r evidence.Receipt) {
	if r.ToolName != "complete_step" || !r.Success || len(r.Args) == 0 {
		return
	}
	var payload struct {
		Evidence []struct {
			Kind        string `json:"kind"`
			CriterionID string `json:"criterion_id"`
		} `json:"evidence"`
	}
	if json.Unmarshal(r.Args, &payload) != nil {
		return
	}
	for _, e := range payload.Evidence {
		id := strings.TrimSpace(e.CriterionID)
		if id == "" {
			continue
		}
		c.Resolve(id, taskcontract.Satisfied, taskcontract.EvidenceRef{
			Kind:          criterionEvidenceKind(e.Kind),
			MutationEpoch: c.Epoch(),
			Source:        "complete_step",
			Success:       true,
		})
	}
}

// resolveBlockedCriteria is the other half of resolveCitedCriteria: a criterion
// the model established cannot be met resolves Failed, which is what a contract
// already means by Blocked. The tool checked the claim against the ledger before
// succeeding, so what the citation adds is which criterion it was about.
func resolveBlockedCriteria(c *taskcontract.Contract, r evidence.Receipt) {
	if r.ToolName != "conclude_blocked" || !r.Success || len(r.Args) == 0 {
		return
	}
	var payload struct {
		CriterionID string `json:"criterion_id"`
	}
	if json.Unmarshal(r.Args, &payload) != nil {
		return
	}
	c.MarkBlocked()
	id := strings.TrimSpace(payload.CriterionID)
	if id == "" {
		return
	}
	c.Resolve(id, taskcontract.Failed, taskcontract.EvidenceRef{
		Kind:          taskcontract.EvidenceRead,
		MutationEpoch: c.Epoch(),
		Source:        "conclude_blocked",
		Success:       true,
	})
}

// criterionEvidenceKind mirrors the ledger's own classification so staleness
// behaves identically: a mutation proves it happened and never stales, while a
// verification, review, or manual check must be re-proven after later changes.
func criterionEvidenceKind(kind string) taskcontract.EvidenceKind {
	switch kind {
	case "verification":
		return taskcontract.EvidenceVerification
	case "review":
		return taskcontract.EvidenceReview
	case "diff", "files":
		return taskcontract.EvidenceMutation
	default:
		return taskcontract.EvidenceRead
	}
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
		Intent:                c.Kind.String(),
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
	return buildShadowContract(a.turn.turnInput, a.task.ledger.Receipts(), a.PlanContract())
}

// emitTurnShadows records the end-of-turn shadow observations: the contract's
// state, and the completion report derived from it. Both observe; neither
// decides. blocked says the host stopped this turn at the readiness gate, so
// the summary reports the turn that happened rather than the one the model
// believed it had finished.
func (a *Agent) emitTurnShadows(input string, blocked bool) {
	if a.task.ledger == nil {
		return
	}
	c := buildShadowContract(input, a.task.ledger.Receipts(), a.PlanContract())
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
	rep := completion.Build(c, a.task.ledger, a.pathInWorkspace)
	a.turn.completion = &rep
	event.RecordCompletionReport(a.svc.sink, completionReportAudit(rep))
	a.sealShadowBundle(input, c, rep, a.task.ledger.Receipts(), blocked)
	a.emitCompletionSummary(c, rep, blocked)
}

// CompletionReceipt returns the turn's completion record for the host to
// deliver, or nil when the turn had nothing to judge. The host renders it; the
// agent never writes the user-facing text, which is the whole point.
func (a *Agent) CompletionReceipt() *event.CompletionReceipt {
	if a == nil || a.turn.completion == nil {
		return nil
	}
	rep := *a.turn.completion
	rep.Gaps = a.planCriterionGaps(rep.Gaps)
	return completionReceipt(rep)
}
