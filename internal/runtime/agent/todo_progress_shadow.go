package agent

import (
	"reasonix/internal/contract/event"
	"reasonix/internal/safety/evidence"
)

// observeTodoTransition records what one task-list write did to the plan. It
// decides nothing: no gate reads these counters and no prompt byte moves with
// them. What it separates is the distinction the host never held — a plan that
// was rewritten from a plan that advanced.
func (a *Agent) observeTodoTransition(before, after []evidence.TodoItem) {
	if kind := evidence.ClassifyTodoTransition(before, after); kind != evidence.TodoUnchanged {
		a.observeTodoKind(kind, after)
	}
}

// observeTodoKind records a verdict the caller already holds. A renewal is one:
// the list did not move, so no comparison of it could have found the sign-off.
func (a *Agent) observeTodoKind(kind evidence.TodoTransition, after []evidence.TodoItem) {
	switch kind {
	case evidence.TodoUnchanged, evidence.TodoRenewal:
	case evidence.TodoReplan:
		a.task.todoRevs.plan++
		a.task.todoRevs.content++
	case evidence.TodoAdvance, evidence.TodoTerminal:
		a.task.todoRevs.progress++
		a.task.todoRevs.content++
	default:
		a.task.todoRevs.content++
	}
	completed := 0
	for _, todo := range after {
		if canonicalTodoStatus(todo.Status) == "completed" {
			completed++
		}
	}
	a.svc.sink.Emit(event.Event{Kind: event.TodoProgressEvent, TodoProgress: &event.TodoProgress{
		Kind: string(kind), Steps: len(after), Completed: completed,
		ContentRevision: a.task.todoRevs.content, PlanRevision: a.task.todoRevs.plan,
		ProgressRevision: a.task.todoRevs.progress,
	}})
}
