package evidence

// Todo identity: how a citation resolves to a task-list item. A stable step id
// is the authority; the text predicates below are the fallback for lists that
// never carried one, where wording and position are all there is to go on.

import (
	"context"
	"slices"
	"strconv"
	"strings"
)

// SameTodos reports whether two task lists say the same thing. Both are
// normalized first, so a list restated with a status the state machine would
// have rewritten anyway counts as unchanged rather than as a new plan.
func SameTodos(a, b []TodoItem) bool {
	return slices.Equal(NormalizeSerialTodos(a), NormalizeSerialTodos(b))
}

// todoMatchAt is the one place a positive match is built, so every path reports
// the matched item's stable id alongside its position.
func todoMatchAt(index int, todo TodoItem) TodoStepMatch {
	return TodoStepMatch{
		Found:      true,
		Index:      index,
		Content:    todo.Content,
		Status:     todo.Status,
		ActiveForm: todo.ActiveForm,
		StepID:     todo.StepID,
	}
}

// MatchStepID resolves a stable step id against the current list. It is the
// exact path complete_step should prefer: unlike a title or a 1-based number, an
// id survives the retitles and insertions a replan introduces.
func MatchStepID(stepID string, todos []TodoItem) (TodoStepMatch, bool) {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return TodoStepMatch{}, false
	}
	for i, todo := range todos {
		if todo.StepID == stepID {
			return todoMatchAt(i+1, todo), true
		}
	}
	return TodoStepMatch{}, false
}

// TodoStepIDs lists the stable ids present in the list, for error messages that
// tell a model exactly which identities it may cite.
func TodoStepIDs(todos []TodoItem) []string {
	out := make([]string, 0, len(todos))
	for _, todo := range todos {
		if todo.StepID != "" {
			out = append(out, todo.StepID)
		}
	}
	return out
}

// TodoCitation is how one task-list item names itself wherever the model is
// shown the list. complete_step asks for the stable id, so the id has to be
// what the model saw; an ordinal is the fallback for a list that never carried
// one, and it goes stale the moment a step is inserted above it.
func TodoCitation(stepID string, index int, content string) string {
	if id := strings.TrimSpace(stepID); id != "" {
		return "[" + id + "] " + content
	}
	return strconv.Itoa(index) + ") " + content
}

// InProgressTodo returns the one item a sign-off may currently name.
func InProgressTodo(todos []TodoItem) (TodoStepMatch, bool) {
	for i, todo := range todos {
		if strings.TrimSpace(todo.Status) == "in_progress" {
			return todoMatchAt(i+1, todo), true
		}
	}
	return TodoStepMatch{}, false
}

// sameTodoIdentity answers by stable id whenever both items carry one: an id is
// identity, so a retitled step still matches and two steps that happen to share
// wording no longer collide. Text is the fallback for freehand lists.
func sameTodoIdentity(a, b TodoItem) bool {
	if a.StepID != "" && b.StepID != "" {
		return a.StepID == b.StepID
	}
	return sameStepText(a.Content, b.Content) || sameStepText(a.ActiveForm, b.ActiveForm)
}

func sameTodoMatch(todo TodoItem, match TodoStepMatch) bool {
	return sameStepText(todo.Content, match.Content) || sameStepText(todo.ActiveForm, match.ActiveForm)
}

// todoContentRelates reports whether a todo item's preferred text has a
// recognisable semantic relationship (substring overlap) with the step match
// that was stored against a previous todo_write list.  It returns true when
// the model has rephrased the same task, not swapped it for a different one.
func todoContentRelates(todo TodoItem, match TodoStepMatch) bool {
	return textOverlaps(todo.Content, match.Content) ||
		textOverlaps(todo.ActiveForm, match.ActiveForm)
}

func textOverlaps(a, b string) bool {
	return stepTextContains(normalizeStepText(a), normalizeStepText(b))
}

// WithAcceptanceCriteria carries the ids of the approved plan's criteria into a
// tool call, so a proof citing one can be checked against the plan the user
// approved instead of resolving into nothing.
func WithAcceptanceCriteria(ctx context.Context, ids []string) context.Context {
	if len(ids) == 0 {
		return ctx
	}
	return context.WithValue(ctx, acceptanceCriteriaKey{}, append([]string(nil), ids...))
}

// AcceptanceCriteriaFromContext returns the approved plan's criterion ids.
func AcceptanceCriteriaFromContext(ctx context.Context) ([]string, bool) {
	ids, ok := ctx.Value(acceptanceCriteriaKey{}).([]string)
	return ids, ok && len(ids) > 0
}

type acceptanceCriteriaKey struct{}
