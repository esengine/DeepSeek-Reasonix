package evidence

// Plan mutation and plan advancement are different facts: only the execution
// position moving says the plan went somewhere. One number for both let 1065
// task-list writes stand in for progress across twelve hours that changed no file.

import "strings"

// TodoTransition is what one task-list write did to the plan, read from the
// list's structure: which steps exist, and where execution stands among them.
type TodoTransition string

const (
	// TodoUnchanged: the same steps in the same state.
	TodoUnchanged TodoTransition = "unchanged"
	// TodoRewrite: the same steps in the same state, said differently.
	TodoRewrite TodoTransition = "rewrite"
	// TodoReplan: the set of steps itself changed — added, dropped, split.
	TodoReplan TodoTransition = "replan"
	// TodoAdvance: a step completed and execution moved on.
	TodoAdvance TodoTransition = "advance"
	// TodoTerminal: the advance that completed the last step.
	TodoTerminal TodoTransition = "terminal"
	// TodoRegression: work that was complete is open again.
	TodoRegression TodoTransition = "regression"
	// TodoRenewal: a sign-off landing on a step already complete. The list does
	// not move, so no structural comparison can find it — the caller that knows
	// a sign-off was attempted names it.
	TodoRenewal TodoTransition = "renewal"
)

// Advances reports whether the transition moved execution forward. Replan is
// deliberately not one: changing the plan is a strategy change, and counting it
// as advancement lets a stalled turn renew itself by rewriting its own list.
func (t TodoTransition) Advances() bool { return t == TodoAdvance || t == TodoTerminal }

// stepIdentity is what makes two entries the same step across a write: a
// stable id where the list carries one, and the normalized text where it does
// not. Position is not identity — an insertion would rename every step after it.
func stepIdentity(todo TodoItem) string {
	if id := strings.TrimSpace(todo.StepID); id != "" {
		return "id:" + id
	}
	return "text:" + normalizeStepText(todo.Content)
}

func stepIdentities(todos []TodoItem) map[string]bool {
	out := make(map[string]bool, len(todos))
	for _, todo := range todos {
		out[stepIdentity(todo)] = true
	}
	return out
}

func completedIdentities(todos []TodoItem) map[string]bool {
	out := map[string]bool{}
	for _, todo := range todos {
		if todoStatus(todo.Status) == "completed" {
			out[stepIdentity(todo)] = true
		}
	}
	return out
}

func sameIdentitySet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func containsAll(outer, inner map[string]bool) bool {
	for k := range inner {
		if !outer[k] {
			return false
		}
	}
	return true
}

func sameWording(a, b []TodoItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Content != b[i].Content || a[i].ActiveForm != b[i].ActiveForm || a[i].Status != b[i].Status {
			return false
		}
	}
	return true
}

// ClassifyTodoTransition reads what changed between two canonical task lists.
// It answers from the lists alone — no wording is interpreted, and no caller's
// intent is consulted — so the same write is classified the same way whether
// the model or the host produced it.
func ClassifyTodoTransition(before, after []TodoItem) TodoTransition {
	before, after = NormalizeSerialTodos(before), NormalizeSerialTodos(after)
	if len(before) == 0 {
		if len(after) == 0 {
			return TodoUnchanged
		}
		return TodoReplan
	}
	if !sameIdentitySet(stepIdentities(before), stepIdentities(after)) {
		return TodoReplan
	}
	doneBefore, doneAfter := completedIdentities(before), completedIdentities(after)
	switch {
	case !containsAll(doneAfter, doneBefore):
		return TodoRegression
	case len(doneAfter) > len(doneBefore):
		if len(doneAfter) == len(stepIdentities(after)) {
			return TodoTerminal
		}
		return TodoAdvance
	case sameWording(before, after):
		return TodoUnchanged
	}
	return TodoRewrite
}
