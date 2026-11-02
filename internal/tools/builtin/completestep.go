package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

func init() { tool.RegisterBuiltin(completeStep{}) }

type completeStep struct{}

func (completeStep) Name() string { return "complete_step" }

func (completeStep) Description() string {
	return "Mark ONE step of an approved plan as done. Call it as you finish each step. The host advances the list for you: it marks this step completed and moves the next to in_progress, so a todo_write that marks such an item completed on its own is REJECTED. Sign off one step per tool-call ROUND, not one per call: the sign-off promotes the next item, and a second sign-off sent beside it signs against the list as it stood before."
}

func (completeStep) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "step_id":{"type":"string","description":"PREFERRED: the stable step_id of the task-list item this completes, e.g. \"plan_step_02\". Unlike a title or a number it survives retitles, insertions, and reordering, so cite it whenever the item has one."},
  "step":{"type":"string","description":"Which plan step this completes — its title or number, matching the task list. Use only when the item has no step_id."},
  "step_index":{"type":"integer","minimum":1,"description":"Optional 1-based task-list item number. Use only when the item has no step_id; an index goes stale the moment a step is inserted above it."},
  "result":{"type":"string","description":"What is now true or changed as a result of finishing this step."},
  "notes":{"type":"string","description":"Optional caveats, follow-ups, or anything deferred."}
},
"required":["result"]
}`)
}

func (completeStep) ReadOnly() bool { return true }

// Sequential is true even though ReadOnly is: signing a step off advances the
// task list, so two sign-offs in one reply have to land in the order sent.
func (completeStep) Sequential(context.Context, json.RawMessage) bool { return true }

// complete_step signs off execution work and is unavailable during planning.
// The host Plan gate remains authoritative for stale or hallucinated calls.
func (completeStep) ProviderVisible(ctx context.Context) bool {
	return !planmode.Active(ctx)
}

// Plan mode being active is what ProviderVisible reads, so that is what the
// code names — not "unapproved", which is a conclusion about a plan this tool
// never sees.
var planModeActive = tool.Refusal{
	Code:    "plan.mode_active",
	Message: "blocked: complete_step is only available after plan approval. While planning, keep task state with todo_write and present the plan for user approval.",
}

func (completeStep) Unavailable(context.Context) tool.Refusal { return planModeActive }

// PlanModeSafe reports false: although complete_step is read-only, it signs off a
// completed execution step, which is meaningful only after plan approval — not
// during planning. This explicit phase opt-out is the Plan gate's enforced
// exception to the ordinary Permissions/Sandbox path.
func (completeStep) PlanModeSafe() bool { return false }

func (completeStep) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		StepID    string `json:"step_id"`
		Step      string `json:"step"`
		StepIndex int    `json:"step_index"`
		Result    string `json:"result"`
		Notes     string `json:"notes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	step := completeStepIdentity(p.StepID, p.Step, p.StepIndex)
	if step == "" {
		return "", fmt.Errorf("step_id, step, or step_index is required — cite the task-list item you are completing, preferring its stable step_id")
	}
	if p.StepIndex < 0 {
		return "", fmt.Errorf("step_index must be a positive 1-based task-list number")
	}
	if strings.TrimSpace(p.Result) == "" {
		return "", fmt.Errorf("result is required — state what is now true after finishing this step")
	}
	todoMatch, hasTodo, err := verifyTodoStep(ctx, step)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Step %q done.%s", step, planAdvanceStatus(ctx, todoMatch, hasTodo)), nil
}

// planAdvanceStatus says what the sign-off left behind. Whether a step remains
// is a fact the task list holds, and telling the model to continue past the
// last one is what keeps a finished plan executing (#8816).
func planAdvanceStatus(ctx context.Context, match evidence.TodoStepMatch, hasTodo bool) string {
	if !hasTodo {
		return " The host recorded the sign-off; there is no task list to advance."
	}
	if terminal, next, ok := planAdvanceAfter(ctx, match); ok {
		switch {
		case terminal:
			return " Every step in the task list is now complete. Do not open new work under this plan."
		case next.Content != "":
			return fmt.Sprintf(" The host advanced the task list; the next step is %d %q.", next.Index, next.Content)
		}
	}
	if match.Status == "completed" {
		return " The matched todo was already completed; the task list is unchanged."
	}
	return " The host advanced the task list; continue with the next step."
}

// planAdvanceAfter replays the host's own advance on a copy of the list, so
// what this result says and what the task list does cannot drift apart: the
// alternative is a second implementation of the same state machine.
func planAdvanceAfter(ctx context.Context, match evidence.TodoStepMatch) (terminal bool, next evidence.TodoStepMatch, ok bool) {
	todos := currentTodos(ctx)
	if len(todos) == 0 || match.Index <= 0 || match.Index > len(todos) {
		return false, evidence.TodoStepMatch{}, false
	}
	advanced := append([]evidence.TodoItem(nil), todos...)
	evidence.AdvanceSerialTodo(advanced, match.Index-1)
	for i, todo := range advanced {
		if strings.TrimSpace(todo.Status) == "in_progress" {
			return false, evidence.TodoStepMatch{Found: true, Index: i + 1, Content: todo.Content,
				Status: todo.Status, ActiveForm: todo.ActiveForm, StepID: todo.StepID}, true
		}
	}
	// AdvanceSerialTodo leaves exactly one item current while any remains, so
	// no current item means none is left.
	return true, evidence.TodoStepMatch{}, true
}

// currentTodos reads the same list verifyTodoStep matched against.
func currentTodos(ctx context.Context) []evidence.TodoItem {
	if ledger, ok := evidence.FromContext(ctx); ok {
		if todos, _ := ledger.LatestTodos(); len(todos) > 0 {
			return todos
		}
	}
	todos, _ := evidence.TodoStateFromContext(ctx)
	return todos
}

// completeStepIdentity picks the citation to resolve against the task list,
// most stable first: an id survives a replan, an index survives a retitle, a
// title survives neither.
func completeStepIdentity(stepID, step string, stepIndex int) string {
	if id := strings.TrimSpace(stepID); id != "" {
		return id
	}
	if stepIndex > 0 {
		return strconv.Itoa(stepIndex)
	}
	return strings.TrimSpace(step)
}

func verifyTodoStep(ctx context.Context, step string) (evidence.TodoStepMatch, bool, error) {
	ledger, ok := evidence.FromContext(ctx)
	var todos []evidence.TodoItem
	if ok {
		todos, _ = ledger.LatestTodos()
	}
	if len(todos) == 0 {
		todos, _ = evidence.TodoStateFromContext(ctx)
	}
	if len(todos) == 0 {
		return evidence.TodoStepMatch{}, false, nil
	}
	match, found := evidence.MatchStep(step, todos)
	if !found {
		allCompleted := true
		for _, todo := range todos {
			if strings.TrimSpace(todo.Status) != "completed" {
				allCompleted = false
				break
			}
		}
		if allCompleted {
			last := len(todos) - 1
			return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q has no matching todo_write item and every current todo is already completed; this is a renewal sign-off, so retry complete_step with step_index %d (the final existing todo %q) — do not invent a new step or rewrite the completed list", step, last+1, todos[last].Content)
		}
		if ids := evidence.TodoStepIDs(todos); len(ids) > 0 {
			return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q has no matching todo_write item in the current task list; cite the item's stable step_id — available ids: %s (list: %s)", step, strings.Join(ids, ", "), todoListInventory(todos))
		}
		return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q has no matching todo_write item in the current task list; cite a todo verbatim or by number: %s", step, todoListInventory(todos))
	}
	switch match.Status {
	case "in_progress":
		if unfinished, ok := evidence.FirstUnfinishedSubStep(todos, match.Index-1); ok && unfinished >= 0 {
			return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q matches phase %d %q whose sub-steps are unfinished; complete sub-step %d %q first, then sign the phase off", step, match.Index, match.Content, unfinished+1, todos[unfinished].Content)
		}
		return match, true, nil
	case "completed":
		return match, true, nil
	case "", "pending":
		current := ""
		for i, todo := range todos {
			if strings.TrimSpace(todo.Status) != "in_progress" {
				continue
			}
			// The deepest in_progress item is the signable end of the current
			// chain: prefer an active sub-step over its phase header.
			current = fmt.Sprintf("; finish todo %d %q first", i+1, todo.Content)
			if todo.Level == 1 {
				break
			}
		}
		return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q matches pending todo %d %q; complete_step only signs the current in_progress item%s", step, match.Index, match.Content, current)
	default:
		return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q matches todo %d (%q) but its status is %q; complete_step requires in_progress or completed", step, match.Index, match.Content, match.Status)
	}
}

func todoListInventory(todos []evidence.TodoItem) string {
	parts := make([]string, 0, len(todos))
	for i, t := range todos {
		content := t.Content
		if r := []rune(content); len(r) > 60 {
			content = string(r[:60]) + "…"
		}
		parts = append(parts, evidence.TodoCitation(t.StepID, i+1, strconv.Quote(content)))
		if len(parts) == 12 && len(todos) > 12 {
			parts = append(parts, fmt.Sprintf("… %d more", len(todos)-12))
			break
		}
	}
	return strings.Join(parts, ", ")
}

// HostRequiredBy says why the tool cannot be removed by name: the loop advances the plan from its receipts and todo_write refuses a completion without one.
func (completeStep) HostRequiredBy() string { return tool.FeaturePlanSteps }
