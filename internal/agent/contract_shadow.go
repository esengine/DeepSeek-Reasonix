package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/completion"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/plancontract"
	"reasonix/internal/taskcontract"
	"reasonix/internal/taskintent"
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
	case taskintent.Classify(input) == taskintent.Mutation,
		taskintent.Classify(input) == taskintent.PersistentAction:
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
	// Todo titles restate the plan's steps, so they only become requirements
	// when no plan supplied the real acceptance criteria.
	if plan == nil {
		for i, todo := range todos {
			c.AddRequirement(fmt.Sprintf("t%d", i+1), todo.Content, true)
		}
	}
	for _, r := range receipts {
		c.Observe(r)
		resolveCitedCriteria(c, r)
	}
	for i, todo := range todos {
		if todo.Status == "completed" {
			c.Resolve(fmt.Sprintf("t%d", i+1), taskcontract.Satisfied)
		}
	}
	return c
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
		Intent:                intentName(c.Intent),
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

func intentName(i taskintent.Intent) string {
	switch i {
	case taskintent.Advisory:
		return "advisory"
	case taskintent.ObservableRead:
		return "observable_read"
	case taskintent.Mutation:
		return "mutation"
	case taskintent.PersistentAction:
		return "persistent_action"
	default:
		return "conversation"
	}
}

// LiveContract is the contract as it stands right now: the same pure replay the
// turn ends with, run against the receipts recorded so far. Rebuilding beats
// keeping incremental state because one code path serves the per-round view and
// the end-of-turn record, so the two can never disagree.
func (a *Agent) LiveContract() *taskcontract.Contract {
	if a == nil || a.evidence == nil {
		return nil
	}
	return buildShadowContract(a.turnInput, a.evidence.Receipts(), a.planContractSnapshot())
}

// observeContractRound records the contract after one tool round, so a
// trajectory carries how the evidence graph filled in rather than only where it
// landed. It observes; it decides nothing.
func (a *Agent) observeContractRound() {
	c := a.LiveContract()
	if c == nil || (len(c.Requirements) == 0 && len(c.Checks) == 0) {
		return
	}
	event.RecordContractShadow(a.sink, contractShadowAudit(c))
}

// emitTurnShadows records the end-of-turn shadow observations: the contract's
// state, and the completion report derived from it. Both observe; neither
// decides.
func (a *Agent) emitTurnShadows(input string) {
	if a.evidence == nil {
		return
	}
	c := buildShadowContract(input, a.evidence.Receipts(), a.planContractSnapshot())
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
	event.RecordContractShadow(a.sink, contractShadowAudit(c))
	rep := completion.Build(c, a.evidence)
	a.completion = &rep
	event.RecordCompletionReport(a.sink, completionReportAudit(rep))
	a.emitCompletionSummary(c)
}

// CompletionReceipt returns the turn's completion record for the host to
// deliver, or nil when the turn had nothing to judge. The host renders it; the
// agent never writes the user-facing text, which is the whole point.
func (a *Agent) CompletionReceipt() *event.CompletionReceipt {
	if a == nil || a.completion == nil {
		return nil
	}
	return completionReceipt(*a.completion)
}
