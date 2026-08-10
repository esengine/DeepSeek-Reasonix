package plancontract

import "reasonix/internal/evidence"

// ProjectTodos renders the plan as the serial task list a host seeds on
// approval: phases at level 0, sub-steps at level 1, first unfinished item made
// current, each carrying its step's id so a completion survives the retitles and
// insertions a replan brings. It returns nil when the plan has no steps or the
// result would break todo_write's state machine, which host state obeys too.
func ProjectTodos(p Plan) []evidence.TodoItem {
	ordered := p.Normalize().Ordered()
	if len(ordered) == 0 {
		return nil
	}
	todos := make([]evidence.TodoItem, 0, len(ordered))
	for _, step := range ordered {
		level := 0
		if step.ParentID != "" {
			level = 1
		}
		todos = append(todos, evidence.TodoItem{
			Content: step.Title,
			Status:  "pending",
			Level:   level,
			StepID:  step.ID,
		})
	}
	todos = evidence.NormalizeSerialTodos(todos)
	if err := evidence.ValidateSerialTodos(todos); err != nil {
		return nil
	}
	return todos
}
