package agent

// Re-projection of the canonical task list's identities: a fold can take the
// step ids out of view while the host still holds them, and complete_step goes
// on asking for one. Derived per request, stored nowhere.

import (
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

// todoIdentityNote renders the ids a sign-off must cite. It states whose list
// this is: an unattributed task list reads as something the model itself sent.
func todoIdentityNote(todos []evidence.TodoItem) string {
	var b strings.Builder
	b.WriteString("Host task state. This list is the host's, not a message you sent; cite these step ids in complete_step:")
	for i, t := range todos {
		fmt.Fprintf(&b, "\n  - %s (%s)", evidence.TodoCitation(t.StepID, i+1, t.Content), t.Status)
	}
	return b.String()
}

// withTodoIdentityTail appends the host's step ids to a request when that
// request cannot already read them. Owed is recomputed from the ids the host
// holds now against the history this request actually carries — a remembered
// "already shown" answers for some earlier request, not this one, and a fold
// between the two is exactly what takes the ids away.
func (a *Agent) withTodoIdentityTail(visible []provider.Message) []provider.Message {
	todos := a.CanonicalTodoState()
	ids := evidence.TodoStepIDs(todos)
	if len(ids) == 0 || todoIdentitiesVisible(visible, ids) {
		return visible
	}
	return append(visible, provider.Message{Role: provider.RoleUser, Content: todoIdentityNote(todos)})
}

// todoIdentitiesVisible reports whether every id can still be read in the view
// the model is about to be sent. Bounded by the projection, which is the part
// compaction just made small.
func todoIdentitiesVisible(msgs []provider.Message, ids []string) bool {
	for _, id := range ids {
		if !messagesMentionID(msgs, id) {
			return false
		}
	}
	return true
}

func messagesMentionID(msgs []provider.Message, id string) bool {
	for _, msg := range slices.Backward(msgs) {
		if strings.Contains(msg.Content, id) {
			return true
		}
		for _, call := range msg.ToolCalls {
			if strings.Contains(call.Arguments, id) {
				return true
			}
		}
	}
	return false
}
