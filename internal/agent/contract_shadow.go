package agent

import (
	"fmt"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/taskcontract"
	"reasonix/internal/taskintent"
)

// buildShadowContract replays a finished turn's receipts into a task
// contract that observed everything and decided nothing: mutation-shaped
// asks get the atomic contract, the latest todo list becomes the
// requirement set, and the ledger drives evidence and epochs. Pure function
// so the shadow is testable without an Agent.
func buildShadowContract(input string, receipts []evidence.Receipt) *taskcontract.Contract {
	var c *taskcontract.Contract
	switch taskintent.Classify(input) {
	case taskintent.Mutation, taskintent.PersistentAction:
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
	for i, todo := range todos {
		c.AddRequirement(fmt.Sprintf("t%d", i+1), todo.Content, true)
	}
	for _, r := range receipts {
		c.Observe(r)
	}
	for i, todo := range todos {
		if todo.Status == "completed" {
			c.Resolve(fmt.Sprintf("t%d", i+1), taskcontract.Satisfied)
		}
	}
	return c
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

// emitContractShadow records the shadow contract's end-of-turn summary; the
// contract observed the turn and decided nothing.
func (a *Agent) emitContractShadow(input string) {
	if a.evidence == nil {
		return
	}
	c := buildShadowContract(input, a.evidence.Receipts())
	event.RecordContractShadow(a.sink, contractShadowAudit(c))
}
