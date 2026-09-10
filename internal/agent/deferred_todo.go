package agent

import (
	"sort"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

// deferredTodoCompletion is keyed by TodoIdentityKey rather than by position.
// Level is retained separately so a stale completion cannot cross a hierarchy
// change before it is consumed.
type deferredTodoCompletion struct {
	level int
}

type todoStateTransition struct {
	todos    []evidence.TodoItem
	deferred []string
	added    []string
	consumed []string
}

// DeferredTodoCompletions returns the stable ids whose completion facts are
// recorded but not yet reflected in the canonical serial list. It is sorted so
// callers and tests never observe map iteration order.
func (a *Agent) DeferredTodoCompletions() []string {
	if a == nil {
		return nil
	}
	a.sess.todoMu.Lock()
	defer a.sess.todoMu.Unlock()
	return deferredTodoIDsLocked(a.sess.deferredTodoCompletions)
}

func deferredTodoIDsLocked(state map[string]deferredTodoCompletion) []string {
	ids := make([]string, 0, len(state))
	for id := range state {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func cloneDeferredTodoState(state *provider.DeferredTodoCompletionState) *provider.DeferredTodoCompletionState {
	if state == nil {
		return nil
	}
	return &provider.DeferredTodoCompletionState{Items: append([]provider.DeferredTodoCompletion(nil), state.Items...)}
}

func hostTodoStateLocked(todos []evidence.TodoItem, deferred map[string]deferredTodoCompletion) *provider.HostTodoState {
	hostTodos := make([]provider.HostTodoItem, len(todos))
	for i, todo := range todos {
		hostTodos[i] = provider.HostTodoItem{
			Content:    todo.Content,
			Status:     todo.Status,
			ActiveForm: todo.ActiveForm,
			Level:      todo.Level,
			StepID:     todo.StepID,
		}
	}
	hostDeferred := make([]provider.DeferredTodoCompletion, 0, len(deferred))
	for _, id := range deferredTodoIDsLocked(deferred) {
		hostDeferred = append(hostDeferred, provider.DeferredTodoCompletion{ID: id, Level: deferred[id].level})
	}
	return &provider.HostTodoState{Todos: hostTodos, Deferred: hostDeferred}
}

func cloneHostTodoState(state *provider.HostTodoState) *provider.HostTodoState {
	if state == nil {
		return nil
	}
	clone := &provider.HostTodoState{
		Todos:    make([]provider.HostTodoItem, len(state.Todos)),
		Deferred: make([]provider.DeferredTodoCompletion, len(state.Deferred)),
	}
	copy(clone.Todos, state.Todos)
	copy(clone.Deferred, state.Deferred)
	return clone
}

func deferredTodoStateFromHost(state *provider.HostTodoState) *provider.DeferredTodoCompletionState {
	if state == nil {
		return nil
	}
	items := make([]provider.DeferredTodoCompletion, len(state.Deferred))
	copy(items, state.Deferred)
	return &provider.DeferredTodoCompletionState{Items: items}
}

func (a *Agent) hostTodoStateSnapshot(force bool) *provider.HostTodoState {
	if a == nil {
		return nil
	}
	a.sess.todoMu.Lock()
	defer a.sess.todoMu.Unlock()
	if !force && len(a.sess.todoState) == 0 && len(a.sess.deferredTodoCompletions) == 0 {
		return nil
	}
	return hostTodoStateLocked(a.sess.todoState, a.sess.deferredTodoCompletions)
}

func todoIdentityIndex(todos []evidence.TodoItem) (map[string]int, map[string]bool) {
	index := make(map[string]int, len(todos))
	duplicates := make(map[string]bool)
	for i, todo := range todos {
		key, ok := evidence.TodoIdentityKey(todo)
		if !ok {
			continue
		}
		if _, exists := index[key]; exists {
			duplicates[key] = true
			delete(index, key)
			continue
		}
		if duplicates[key] {
			continue
		}
		index[key] = i
	}
	return index, duplicates
}

// pruneDeferredTodoCompletionsLocked drops facts that can no longer be
// attached safely to the current list. A completed item consumed the fact;
// deletion, duplicate identities, and level changes invalidate it.
func (a *Agent) pruneDeferredTodoCompletionsLocked() {
	if len(a.sess.deferredTodoCompletions) == 0 {
		return
	}
	index, duplicates := todoIdentityIndex(a.sess.todoState)
	for id, deferred := range a.sess.deferredTodoCompletions {
		i, ok := index[id]
		if !ok || duplicates[id] || strings.TrimSpace(a.sess.todoState[i].Status) == "completed" || a.sess.todoState[i].Level != deferred.level {
			delete(a.sess.deferredTodoCompletions, id)
		}
	}
	if len(a.sess.deferredTodoCompletions) == 0 {
		a.sess.deferredTodoCompletions = nil
	}
}

func replaceCanonicalTodoState(a *Agent, todos []evidence.TodoItem) {
	if a == nil {
		return
	}
	a.sess.todoMu.Lock()
	a.sess.todoState = evidence.NormalizeSerialTodos(todos)
	a.sess.deferredTodoCompletions = nil
	a.sess.todoMu.Unlock()
}

func hostTodoItemsToEvidence(items []provider.HostTodoItem) []evidence.TodoItem {
	todos := make([]evidence.TodoItem, len(items))
	for i, item := range items {
		todos[i] = evidence.TodoItem{
			Content:    item.Content,
			Status:     item.Status,
			ActiveForm: item.ActiveForm,
			Level:      item.Level,
			StepID:     item.StepID,
		}
	}
	return todos
}

func (a *Agent) restoreTodoState(state *provider.HostTodoState) {
	if a == nil {
		return
	}
	a.sess.todoMu.Lock()
	if state == nil {
		a.sess.todoState = nil
	} else {
		// A host snapshot is already canonical. Keep it byte-for-byte equivalent
		// to the in-memory state; recovery must not replay old tool calls or run a
		// second normalization/consumption transition.
		a.sess.todoState = hostTodoItemsToEvidence(state.Todos)
	}
	a.sess.deferredTodoCompletions = nil
	if state != nil {
		for _, item := range state.Deferred {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			if a.sess.deferredTodoCompletions == nil {
				a.sess.deferredTodoCompletions = make(map[string]deferredTodoCompletion)
			}
			if _, exists := a.sess.deferredTodoCompletions[item.ID]; !exists {
				a.sess.deferredTodoCompletions[item.ID] = deferredTodoCompletion{level: item.Level}
			}
		}
	}
	a.pruneDeferredTodoCompletionsLocked()
	a.sess.todoMu.Unlock()
}

func (a *Agent) restoreLegacyTodoState(todos []evidence.TodoItem, persisted *provider.DeferredTodoCompletionState) {
	if a == nil {
		return
	}
	a.sess.todoMu.Lock()
	a.sess.todoState = evidence.NormalizeSerialTodos(todos)
	a.sess.deferredTodoCompletions = nil
	if persisted != nil {
		for _, item := range persisted.Items {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			if a.sess.deferredTodoCompletions == nil {
				a.sess.deferredTodoCompletions = make(map[string]deferredTodoCompletion)
			}
			if _, exists := a.sess.deferredTodoCompletions[item.ID]; !exists {
				a.sess.deferredTodoCompletions[item.ID] = deferredTodoCompletion{level: item.Level}
			}
		}
	}
	a.pruneDeferredTodoCompletionsLocked()
	a.consumeDeferredCompletionsLocked()
	a.pruneDeferredTodoCompletionsLocked()
	a.sess.todoMu.Unlock()
}

// acceptTodoUpdate merges one successful todo_write into the canonical state,
// records safe out-of-order completions idempotently, and applies only the
// contiguous deferred prefix. The whole transition is serialized by todoMu so
// late or concurrent results cannot overwrite a newer canonical state.
func (a *Agent) acceptTodoUpdate(next []evidence.TodoItem, deferred []evidence.TodoItem, planReplacement bool) todoStateTransition {
	if a == nil {
		return todoStateTransition{}
	}
	a.sess.todoMu.Lock()
	defer a.sess.todoMu.Unlock()

	if planReplacement || len(a.sess.todoState) == 0 {
		a.sess.todoState = evidence.NormalizeSerialTodos(next)
		a.sess.deferredTodoCompletions = nil
	} else {
		a.sess.todoState = mergeCanonicalTodoState(a.sess.todoState, next)
	}

	added := make([]string, 0, len(deferred))
	for _, candidate := range deferred {
		key, ok := evidence.TodoIdentityKey(candidate)
		if !ok {
			continue
		}
		index, duplicates := todoIdentityIndex(a.sess.todoState)
		i, found := index[key]
		if !found || duplicates[key] {
			continue
		}
		current := a.sess.todoState[i]
		switch strings.TrimSpace(current.Status) {
		case "completed":
			delete(a.sess.deferredTodoCompletions, key)
		case "pending":
			if a.sess.deferredTodoCompletions == nil {
				a.sess.deferredTodoCompletions = make(map[string]deferredTodoCompletion)
			}
			if _, exists := a.sess.deferredTodoCompletions[key]; !exists {
				a.sess.deferredTodoCompletions[key] = deferredTodoCompletion{level: current.Level}
				added = append(added, key)
			}
		}
	}

	a.pruneDeferredTodoCompletionsLocked()
	consumed := a.consumeDeferredCompletionsLocked()
	a.pruneDeferredTodoCompletionsLocked()
	return todoStateTransition{
		todos:    append([]evidence.TodoItem(nil), a.sess.todoState...),
		deferred: deferredTodoIDsLocked(a.sess.deferredTodoCompletions),
		added:    added,
		consumed: consumed,
	}
}

// mergeCanonicalTodoState preserves progress already observed by a newer
// result while accepting retitles and appended pending work from a valid
// update. A stale result that no longer matches the current ordered identity
// layout is ignored, leaving the latest canonical state intact.
func mergeCanonicalTodoState(current, next []evidence.TodoItem) []evidence.TodoItem {
	if len(current) == 0 {
		return evidence.NormalizeSerialTodos(next)
	}
	if len(next) < len(current) {
		return append([]evidence.TodoItem(nil), current...)
	}
	merged := append([]evidence.TodoItem(nil), next...)
	for i, prior := range current {
		candidate := merged[i]
		if prior.Level != candidate.Level {
			return append([]evidence.TodoItem(nil), current...)
		}
		match, found := evidence.MatchTodoIdentity(prior, merged)
		if !found || match.Index != i+1 {
			return append([]evidence.TodoItem(nil), current...)
		}
		if strings.TrimSpace(prior.Status) == "completed" {
			merged[i].Status = "completed"
		} else if strings.TrimSpace(prior.Status) == "in_progress" && strings.TrimSpace(merged[i].Status) == "pending" {
			merged[i].Status = "in_progress"
		}
	}
	merged = evidence.NormalizeSerialTodos(merged)
	if err := evidence.ValidateSerialTodos(merged); err != nil {
		return append([]evidence.TodoItem(nil), current...)
	}
	return merged
}

// consumeDeferredCompletionsLocked advances only the current serial item when
// its identity is deferred. AdvanceSerialTodo owns phase/sub-step promotion;
// the loop stops at the first hole, so a later deferred item can never skip an
// unfinished predecessor.
func (a *Agent) consumeDeferredCompletionsLocked() []string {
	if len(a.sess.todoState) == 0 || len(a.sess.deferredTodoCompletions) == 0 {
		return nil
	}
	working := append([]evidence.TodoItem(nil), a.sess.todoState...)
	deferred := make(map[string]deferredTodoCompletion, len(a.sess.deferredTodoCompletions))
	for id, item := range a.sess.deferredTodoCompletions {
		deferred[id] = item
	}
	consumed := make([]string, 0)
	for range working {
		index := nextSerialAdvanceIndex(working)
		if index < 0 {
			break
		}
		id, ok := evidence.TodoIdentityKey(working[index])
		item, exists := deferred[id]
		if !ok || !exists || item.level != working[index].Level {
			break
		}
		before := append([]evidence.TodoItem(nil), working...)
		if !evidence.AdvanceSerialTodo(working, index) || evidence.ValidateSerialTodos(working) != nil {
			working = before
			break
		}
		delete(deferred, id)
		consumed = append(consumed, id)
	}
	a.sess.todoState = working
	if len(deferred) == 0 {
		a.sess.deferredTodoCompletions = nil
	} else {
		a.sess.deferredTodoCompletions = deferred
	}
	return consumed
}

func nextSerialAdvanceIndex(todos []evidence.TodoItem) int {
	for i, todo := range todos {
		if strings.TrimSpace(todo.Status) != "in_progress" {
			continue
		}
		if sub, ok := evidence.FirstUnfinishedSubStep(todos, i); ok && sub >= 0 {
			return sub
		}
		return i
	}
	return -1
}

func todoNamesForKeys(ids []string, todos []evidence.TodoItem) string {
	if len(ids) == 0 {
		return ""
	}
	index, duplicates := todoIdentityIndex(todos)
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		i, ok := index[id]
		if !ok || duplicates[id] {
			continue
		}
		name := strings.TrimSpace(todos[i].Content)
		if name == "" {
			name = id
		}
		if todos[i].StepID != "" {
			name += " (" + todos[i].StepID + ")"
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}
