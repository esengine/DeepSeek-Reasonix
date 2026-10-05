package agent

// Re-projection of the canonical task list's identities: a fold can take the
// step ids out of view while the host still holds them, and complete_step goes
// on asking for one. Derived per request, stored nowhere.

import (
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/evidence"
)

// todoIdentityNote renders the ids a sign-off must cite. It states whose list
// this is: an unattributed task list reads as something the model itself sent.
func todoIdentityNote(todos []evidence.TodoItem) string {
	var b strings.Builder
	b.WriteString("Host task state. This list is the host's, not a message you sent; cite these step ids in complete_step:")
	for i, t := range todos {
		b.WriteString("\n  - " + todoStateLine(i+1, t))
	}
	return b.String()
}

// todoStateLine is the single rendering of one item, shared by the note and the
// freshness check so the two cannot diverge.
func todoStateLine(index int, t evidence.TodoItem) string {
	return fmt.Sprintf("%s (%s)", evidence.TodoCitation(t.StepID, index, t.Content), t.Status)
}

// withTodoIdentityTail appends the host's task state when the request cannot
// already read it. Owed is recomputed per request from canonical state, never
// from a record of what an earlier request carried. A list with every item
// complete owes nothing: the tail exists so a sign-off can cite an id, and that
// plan has none left — carried into the next task it reads as work outstanding.
func (a *Agent) withTodoIdentityTail(visible []provider.Message) []provider.Message {
	todos := a.CanonicalTodoState()
	if len(evidence.TodoStepIDs(todos)) == 0 || len(evidence.IncompleteTodos(todos)) == 0 || todoStateVisible(visible, todos) {
		return visible
	}
	return append(visible, provider.Message{Role: provider.RoleUser, Content: todoIdentityNote(todos), Derived: true})
}

// todoStateVisible reports whether the view already carries the host's current
// reading of every item. Ids alone do not answer it: they persist in the model's
// own todo_write after complete_step has moved the status under them.
func todoStateVisible(msgs []provider.Message, todos []evidence.TodoItem) bool {
	for i, t := range todos {
		if !messagesMention(msgs, todoStateLine(i+1, t)) {
			return false
		}
	}
	return true
}

func messagesMention(msgs []provider.Message, needle string) bool {
	for _, msg := range slices.Backward(msgs) {
		if strings.Contains(msg.Content, needle) {
			return true
		}
		for _, call := range msg.ToolCalls {
			if strings.Contains(call.Arguments, needle) {
				return true
			}
		}
	}
	return false
}
