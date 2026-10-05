package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"reasonix/internal/safety/evidence"
)

// AwaitUserTool declares that an open task list cannot advance without the
// user. It is the only signal separating that from a turn that stopped with
// work undone. Readiness waives the open list for a turn that records one, and
// a Goal pauses on it.
type AwaitUserTool struct{}

// awaitUserNeedLimit bounds `need`: the host shows it as a one-line status, and
// a need that carries the options is a choice ask would have put on a card.
const awaitUserNeedLimit = 200

func NewAwaitUserTool() *AwaitUserTool { return &AwaitUserTool{} }

func (*AwaitUserTool) Name() string { return "await_user" }

func (*AwaitUserTool) Description() string {
	return "Hand control back to the user with the task list still open, when what the current item needs next is something only they can give: their view on what you just showed them, the material they said they would add, the judgement the list exists to collect. The list is kept as it stands and the turn ends; their next message resumes it, and the host will not continue the list on its own in the meantime. This is not `ask`, which offers options you wrote and answers inside the same turn, and not `conclude_blocked`, which says the work cannot be done at all. When what you wait for is a pick among choices you can name — which way to proceed, which option to take — call `ask` instead: the user answers it with one click and you carry on in this turn. Name the waiting item with `step_id`, and put in `need` one short line saying what you are waiting for, addressed to them. Say the substance — the item you are presenting, the question you want answered — in your own reply; this call records the wait, it does not speak for you."
}

func (*AwaitUserTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "step_id":{"type":"string","description":"The task-list item that is waiting. Defaults to the list's current in_progress item."},
  "need":{"type":"string","description":"One short line: what you need from the user before this item can go on, written to them. Not the options and not the substance — those go in your reply, or in ask when they are choices."}
},
"required":["need"]
}`)
}

// DecisionBarrier is true: calls queued behind this one were written on the
// assumption it withdraws, so the round ends here.
func (*AwaitUserTool) DecisionBarrier() bool { return true }

// ReadOnly is true: the call records a declaration and has no host effect.
func (*AwaitUserTool) ReadOnly() bool { return true }

func (*AwaitUserTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var payload struct {
		StepID string `json:"step_id"`
		Need   string `json:"need"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	need := strings.TrimSpace(payload.Need)
	if need == "" {
		return "", fmt.Errorf("await_user needs a `need`: say what you are waiting for the user to give you")
	}
	if n := utf8.RuneCountInString(need); n > awaitUserNeedLimit {
		return "", fmt.Errorf("`need` is %d characters and is shown to the user as a one-line status, so it holds at most %d; put the substance in your reply, and if you are offering choices call ask with them as options instead", n, awaitUserNeedLimit)
	}
	if _, planning := planSubmissionFromContext(ctx); planning {
		return "", fmt.Errorf("await_user hands back an execution turn, not a planning one; a plan that needs the user's decision states the unknown, or calls ask")
	}
	// No asker means no answer can arrive. The error records no gate, so the
	// turn's readiness is unchanged.
	if _, _, asker, ok := CallContext(ctx); !ok || asker == nil {
		return "", fmt.Errorf("await_user needs an interactive user and this run has none, so there is nobody whose answer would arrive; do whatever the list allows without them, then call conclude_blocked naming the input that is missing")
	}
	todos := awaitUserTodos(ctx)
	if len(todos) == 0 {
		return "", fmt.Errorf("await_user parks an open task list and this turn has none; end your reply with what you want from the user instead — a turn that left no list open is not continued")
	}
	if len(evidence.IncompleteTodos(todos)) == 0 {
		return "", fmt.Errorf("every item on the current task list is completed, so nothing is waiting; end your reply with what you want from the user instead")
	}
	match, err := awaitUserStep(payload.StepID, todos)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Recorded: waiting on the user at %s. The turn ends here with the list untouched, and the host will not continue it — their next message does. Put what you are waiting for to them in your reply.",
		evidence.TodoCitation(match.StepID, match.Index, match.Content)), nil
}

// awaitUserTodos returns the list being gated. The ledger carries one only for
// a turn that wrote it; a list worked one item per turn lives in the canonical
// todo state.
func awaitUserTodos(ctx context.Context) []evidence.TodoItem {
	if ledger, ok := evidence.FromContext(ctx); ok {
		if todos, ok := ledger.LatestTodos(); ok && len(todos) > 0 {
			return todos
		}
	}
	todos, _ := evidence.TodoStateFromContext(ctx)
	return todos
}

// awaitUserStep resolves the waiting item. A step_id is identity; without one
// the serial state machine's single in_progress item is the only other answer
// readable off structure.
func awaitUserStep(stepID string, todos []evidence.TodoItem) (evidence.TodoStepMatch, error) {
	if id := strings.TrimSpace(stepID); id != "" {
		match, ok := evidence.MatchStepID(id, todos)
		if !ok {
			if ids := evidence.TodoStepIDs(todos); len(ids) > 0 {
				return match, fmt.Errorf("step_id %q names no item on the current task list; cite one of: %s", id, strings.Join(ids, ", "))
			}
			return match, fmt.Errorf("step_id %q names no item on the current task list, and the list carries no ids at all; leave step_id out and the current item is taken", id)
		}
		if strings.TrimSpace(match.Status) == "completed" {
			return match, fmt.Errorf("todo %d %q is already completed, so nothing about it waits on the user; name the item that does, or leave step_id out", match.Index, match.Content)
		}
		return match, nil
	}
	if match, ok := evidence.InProgressTodo(todos); ok {
		return match, nil
	}
	if ids := evidence.TodoStepIDs(todos); len(ids) > 0 {
		return evidence.TodoStepMatch{}, fmt.Errorf("no item on the list is in_progress, so there is no current one to name; give step_id — available ids: %s", strings.Join(ids, ", "))
	}
	return evidence.TodoStepMatch{}, fmt.Errorf("no item on the list is in_progress, so there is no current one to name; mark the item you are waiting on with todo_write first")
}

// UserGate reports the gate recorded this turn, for hosts deciding what runs
// after it.
func (a *Agent) UserGate() (evidence.UserGate, bool) {
	if a == nil || a.task.ledger == nil {
		return evidence.UserGate{}, false
	}
	return a.task.ledger.UserGateThisTurn()
}
