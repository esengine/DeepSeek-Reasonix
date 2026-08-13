package control

// Plan-todo seeding: turning an approved plan's markdown list into the same
// todo_write-shaped state the model maintains itself.

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

type seedTodo struct {
	Content string `json:"content"`
	Status  string `json:"status"`
	Level   int    `json:"level,omitempty"`
	StepID  string `json:"step_id,omitempty"`
}

// seedPlanTodos turns an approved plan into a starter task list and emits it as a
// synthetic todo_write event, so the live task panel populates the instant the
// user approves — a structural guarantee, not a prompt the model might ignore.
// The model still flips item status as it works (only it knows its own
// progress); this just makes the list exist. No-op when the plan has no list.
func (c *Controller) seedPlanTodos(plan string) string {
	args := PlanTodosJSON(plan)
	if args == "" {
		return ""
	}
	t := event.Tool{ID: "plan-seed", Name: "todo_write", Args: args, ReadOnly: true}
	c.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: t})
	t.Output = "task list seeded from the approved plan"
	c.sink.Emit(event.Event{Kind: event.ToolResult, Tool: t})
	c.seedAgentTodoState(args)
	return args
}

func (c *Controller) seedAgentTodoState(args string) {
	if c.executor == nil {
		return
	}
	todos := agentTodoStateFromArgs(args)
	if len(todos) == 0 {
		return
	}
	c.executor.SeedTodoState(todos)
}

func (c *Controller) completePlanTodos(args string) {
	if args == "" {
		return
	}
	done := completedPlanTodosJSON(args)
	if done == "" {
		return
	}
	t := event.Tool{ID: "plan-seed", Name: "todo_write", Args: done, ReadOnly: true}
	c.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: t})
	t.Output = "approved plan finished"
	c.sink.Emit(event.Event{Kind: event.ToolResult, Tool: t})
	c.replaceAgentTodoState(done)
}

func (c *Controller) replaceAgentTodoState(args string) {
	if c.executor == nil {
		return
	}
	todos := agentTodoStateFromArgs(args)
	if len(todos) == 0 {
		return
	}
	c.executor.ReplaceTodoState(todos)
}

func agentTodoStateFromArgs(args string) []evidence.TodoItem {
	var payload struct {
		Todos []evidence.TodoItem `json:"todos"`
	}
	if err := json.Unmarshal([]byte(args), &payload); err != nil {
		return nil
	}
	return payload.Todos
}

// PlanTodosJSON parses an approved plan's markdown into todo_write-shaped args
// JSON ({"todos":[...]}), or "" when the plan has no list items. The exit_plan_mode
// path seeds via seedPlanTodos (an event); a frontend whose own approval flow
// bypasses exit_plan_mode (the chat TUI's text-plan approval) calls this directly
// to render the same starter checklist. Shared parsing keeps the two consistent.
func PlanTodosJSON(plan string) string {
	items := parsePlanTodos(plan)
	if len(items) == 0 {
		return ""
	}
	// Host-generated state must obey the same contract as a model todo_write.
	// Returning no seed is safer than showing a list the agent cannot advance.
	if err := evidence.ValidateSerialTodos(seedTodoEvidenceState(items)); err != nil {
		return ""
	}
	b, err := json.Marshal(map[string]any{"todos": items})
	if err != nil {
		return ""
	}
	return string(b)
}

func completedPlanTodosJSON(args string) string {
	var p struct {
		Todos []seedTodo `json:"todos"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil || len(p.Todos) == 0 {
		return ""
	}
	for i := range p.Todos {
		p.Todos[i].Status = "completed"
	}
	b, err := json.Marshal(map[string]any{"todos": p.Todos})
	if err != nil {
		return ""
	}
	return string(b)
}

// parsePlanTodos extracts a starter task list from an approved plan's markdown
// list items (bulleted or numbered), capped so a long plan can't flood the panel.
// A flat list starts its first item; a layered list starts the first sub-step and
// keeps its phase pending until every child is complete, matching todo_write's
// serial state machine. It understands ONLY markdown lists
// — an unambiguous, standard structure — and deliberately does not guess at prose,
// tables, or arrow sequences (those need brittle, language-specific heuristics).
// The plan-mode marker steers the model to present its plan as a list, so this
// catches the normal case; anything it misses is covered by the model's own
// todo_write calls as it executes.
func parsePlanTodos(plan string) []seedTodo {
	var todos []seedTodo
	for raw := range strings.SplitSeq(plan, "\n") {
		item, level, ok := listItem(raw)
		if !ok {
			continue
		}
		todos = append(todos, seedTodo{Content: item, Status: "pending", Level: level})
		if len(todos) >= 20 {
			break
		}
	}
	if len(todos) == 0 {
		return nil
	}
	// Be tolerant of a model that emits an indented bullet before a phase
	// heading: promote the first item so the host never seeds an orphan L1.
	if todos[0].Level == 1 {
		todos[0].Level = 0
	}
	// The markdown has no identity of its own, so the host mints one per item.
	// Seeded ids are what let a later rewrite retitle or insert steps without
	// detaching the completions already signed off against them.
	for i := range todos {
		todos[i].StepID = fmt.Sprintf("plan_step_%02d", i+1)
	}
	normalized := evidence.NormalizeSerialTodos(seedTodoEvidenceState(todos))
	for i := range todos {
		todos[i].Status = normalized[i].Status
		todos[i].Level = normalized[i].Level
	}
	return todos
}

func seedTodoEvidenceState(todos []seedTodo) []evidence.TodoItem {
	state := make([]evidence.TodoItem, len(todos))
	for i, todo := range todos {
		state[i] = evidence.TodoItem{
			Content: todo.Content,
			Status:  todo.Status,
			Level:   todo.Level,
			StepID:  todo.StepID,
		}
	}
	return state
}

// hasTodoUpdateSince reports whether the model emitted its own todo_write after
// index start, so the seeded plan todos aren't auto-completed over the model's
// own bookkeeping.
func (c *Controller) hasTodoUpdateSince(start int) bool {
	if c.executor == nil {
		return false
	}
	msgs := c.executor.Session().Messages
	if start < 0 || start > len(msgs) {
		start = len(msgs)
	}
	_, ok := latestTodoArgsSince(msgs, start)
	return ok
}

func latestTodoArgsSince(msgs []provider.Message, start int) (string, bool) {
	for i := len(msgs) - 1; i >= start; i-- {
		for _, v := range slices.Backward(msgs[i].ToolCalls) {
			tc := v
			if tc.Name == "todo_write" {
				return tc.Arguments, true
			}
		}
	}
	return "", false
}

// listItem parses a markdown list line ("- x", "* x", "1. x", "2) x") into its
// task text and a nesting level derived from leading indentation (0 for a
// top-level item, 1 for an indented sub-step — capped at 1 since the plan is
// two-level). ok is false when the line isn't a list item. Light inline-markdown
// stripping keeps the checklist readable.
func listItem(line string) (content string, level int, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return "", 0, false
	}
	indent := 0
	for _, c := range line[:len(line)-len(trimmed)] {
		if c == '\t' {
			indent += 4
		} else {
			indent++
		}
	}
	s := trimmed
	// A numbered markdown heading ("### 1. Add the loader") is how models often
	// write a phase even when asked for a list; strip the heading marker and
	// treat it as a top-level phase. A heading without a number (a section
	// title like "## Plan") falls through and is ignored.
	heading := false
	if h := strings.TrimLeft(s, "#"); h != s && strings.HasPrefix(h, " ") {
		heading = true
		s = strings.TrimSpace(h)
	}
	switch {
	case strings.HasPrefix(s, "- "), strings.HasPrefix(s, "* "), strings.HasPrefix(s, "+ "):
		s = s[2:]
	default:
		// numbered: leading digits, then "." or ")", then a space
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 || i+1 >= len(s) || (s[i] != '.' && s[i] != ')') || s[i+1] != ' ' {
			return "", 0, false
		}
		s = s[i+2:]
	}
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[ ] ")
	s = strings.TrimPrefix(s, "[x] ")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, false
	}
	if heading {
		return s, 0, true // a heading is always a top-level phase
	}
	if indent >= 2 {
		return s, 1, true
	}
	return s, 0, true
}
