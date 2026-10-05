package evidence

// The todo list as evidence: which step a receipt is about, what a serial list
// allows next, and which completed items no receipt has verified.

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ValidateSerialTodos enforces the task-list state machine promised by
// todo_write: at most one item in the whole list is in_progress, completed
// work forms a serial prefix, and pending work follows the current item. The
// rule is segment-aware for two-level lists: a level-0 phase owns the level-1
// sub-steps after it, sub-steps complete in order while their phase stays
// pending, and the phase becomes the single in_progress item only after every
// sub-step has completed — the phase signs off last. A fully completed or
// empty list is also valid.
func ValidateSerialTodos(todos []TodoItem) error {
	ipSeen := false
	for i, todo := range todos {
		switch todoStatus(todo.Status) {
		case "completed", "pending":
		case "in_progress":
			if ipSeen {
				return fmt.Errorf("todo %d %q is a second in_progress item; serial task lists allow exactly one current item", i+1, todo.Content)
			}
			ipSeen = true
		default:
			return fmt.Errorf("todo %d %q has invalid status %q", i+1, todo.Content, todo.Status)
		}
	}
	if len(todos) > 0 && todos[0].Level == 1 {
		return fmt.Errorf("todo 1 %q is a level-1 sub-step with no phase above it; add a level-0 phase header or use level 0", todos[0].Content)
	}
	seenCurrent := false
	seenPending := false
	for _, seg := range serialTodoSegments(todos) {
		state, err := validateSerialSegment(todos, seg)
		if err != nil {
			return err
		}
		switch state {
		case "completed":
			if seenCurrent || seenPending {
				return fmt.Errorf("todo %d %q is completed after unfinished work; serial task lists require completed items to form a prefix", seg.head+1, todos[seg.head].Content)
			}
		case "in_progress":
			if seenPending {
				ip := seg.head
				for i := seg.head; i < seg.end; i++ {
					if todoStatus(todos[i].Status) == "in_progress" {
						ip = i
						break
					}
				}
				return fmt.Errorf("todo %d %q is in_progress after pending work; the current item must be the first unfinished item", ip+1, todos[ip].Content)
			}
			seenCurrent = true
		case "pending":
			seenPending = true
		default: // stale: partially completed with no current item
			if seenCurrent {
				first := seg.head
				for i := seg.head; i < seg.end; i++ {
					if todoStatus(todos[i].Status) == "completed" {
						first = i
						break
					}
				}
				return fmt.Errorf("todo %d %q is completed after unfinished work; serial task lists require completed items to form a prefix", first+1, todos[first].Content)
			}
			seenPending = true
		}
	}
	if len(todos) > 0 && seenPending && !seenCurrent {
		return fmt.Errorf("serial task list has pending work but no in_progress item")
	}
	return nil
}

// serialTodoSegments splits a task list into serial units. A level-0 item
// directly followed by level-1 items owns them as one phase segment; every
// other item — including a level-1 item with no preceding phase — is its own
// single-step segment.
func serialTodoSegments(todos []TodoItem) []todoSegment {
	var segs []todoSegment
	for i := 0; i < len(todos); {
		end := i + 1
		if todos[i].Level == 0 {
			for end < len(todos) && todos[end].Level == 1 {
				end++
			}
		}
		segs = append(segs, todoSegment{head: i, end: end})
		i = end
	}
	return segs
}

// validateSerialSegment checks one segment's internal shape and returns its
// serial state: "completed" (every item completed), "in_progress" (the
// segment holds the current item), "pending" (untouched), or "stale"
// (partially completed with no current item). Item statuses and the global
// single-in_progress rule are already validated by the caller.
func validateSerialSegment(todos []TodoItem, seg todoSegment) (string, error) {
	head := todos[seg.head]
	headStatus := todoStatus(head.Status)
	if seg.end == seg.head+1 {
		return headStatus, nil
	}
	seenSubCurrent := false
	seenSubPending := false
	completedSubs := 0
	unfinished := -1
	for i := seg.head + 1; i < seg.end; i++ {
		sub := todos[i]
		switch todoStatus(sub.Status) {
		case "completed":
			if seenSubCurrent || seenSubPending {
				return "", fmt.Errorf("todo %d %q is completed after unfinished work; serial task lists require completed items to form a prefix", i+1, sub.Content)
			}
			completedSubs++
		case "in_progress":
			if seenSubPending {
				return "", fmt.Errorf("todo %d %q is in_progress after pending work; the current item must be the first unfinished item", i+1, sub.Content)
			}
			seenSubCurrent = true
			if unfinished < 0 {
				unfinished = i
			}
		default: // pending
			seenSubPending = true
			if unfinished < 0 {
				unfinished = i
			}
		}
	}
	switch headStatus {
	case "completed":
		if unfinished >= 0 {
			return "", fmt.Errorf("phase %d %q is completed but sub-step %d %q is unfinished; complete every sub-step, then sign the phase off with complete_step", seg.head+1, head.Content, unfinished+1, todos[unfinished].Content)
		}
		return "completed", nil
	case "in_progress":
		if unfinished >= 0 {
			return "", fmt.Errorf("phase %d %q cannot be in_progress while sub-step %d %q is unfinished; keep the phase pending, finish its sub-steps in order, then mark the phase in_progress to sign it off", seg.head+1, head.Content, unfinished+1, todos[unfinished].Content)
		}
		return "in_progress", nil
	default: // pending head: its sub-steps carry the segment's progress
		if seenSubCurrent {
			return "in_progress", nil
		}
		if completedSubs == 0 {
			return "pending", nil
		}
		return "stale", nil
	}
}

// NormalizeSerialTodos repairs legacy host state that predates
// ValidateSerialTodos. It preserves the leading run of fully completed
// segments and makes the first unfinished segment current: its completed
// sub-step prefix is kept and its first unfinished sub-step becomes the
// single in_progress item — or the phase itself when every sub-step is
// already completed. Every later segment returns to pending.
func NormalizeSerialTodos(todos []TodoItem) []TodoItem {
	out := append([]TodoItem(nil), todos...)
	unfinished := false
	for _, seg := range serialTodoSegments(out) {
		if !unfinished && serialSegmentCompleted(out, seg) {
			continue
		}
		if unfinished {
			for i := seg.head; i < seg.end; i++ {
				out[i].Status = "pending"
			}
			continue
		}
		unfinished = true
		if seg.end == seg.head+1 {
			out[seg.head].Status = "in_progress"
			continue
		}
		subUnfinished := false
		for i := seg.head + 1; i < seg.end; i++ {
			if !subUnfinished && todoStatus(out[i].Status) == "completed" {
				continue
			}
			if !subUnfinished {
				out[i].Status = "in_progress"
				subUnfinished = true
				continue
			}
			out[i].Status = "pending"
		}
		if subUnfinished {
			out[seg.head].Status = "pending"
		} else {
			out[seg.head].Status = "in_progress"
		}
	}
	return out
}

func serialSegmentCompleted(todos []TodoItem, seg todoSegment) bool {
	for i := seg.head; i < seg.end; i++ {
		if todoStatus(todos[i].Status) != "completed" {
			return false
		}
	}
	return true
}

// FirstUnfinishedSubStep reports whether todos[index] is a level-0 phase with
// level-1 sub-steps, and if so the 0-based index of its first sub-step that is
// not yet completed. ok is false when index is not a phase header; a phase
// whose sub-steps are all completed returns (-1, true).
func FirstUnfinishedSubStep(todos []TodoItem, index int) (int, bool) {
	if index < 0 || index >= len(todos) || todos[index].Level != 0 {
		return -1, false
	}
	if index+1 >= len(todos) || todos[index+1].Level != 1 {
		return -1, false
	}
	for i := index + 1; i < len(todos) && todos[i].Level == 1; i++ {
		if todoStatus(todos[i].Status) != "completed" {
			return i, true
		}
	}
	return -1, true
}

// AdvanceSerialTodo completes the in_progress item at index (0-based) as a
// signed-off step and promotes the next serial item so exactly one item stays
// current. A phase with unfinished sub-steps does not complete. Completing a
// sub-step promotes its next pending sibling, or returns its phase to
// in_progress for sign-off once every sibling is completed. Completing a
// phase or plain step promotes the next pending unit — a phase's first
// pending sub-step (the phase itself stays pending until its sub-steps
// finish), or the plain step itself. A level-1 item with no phase above it
// advances as a standalone step. It reports whether the item was completed.
func AdvanceSerialTodo(todos []TodoItem, index int) bool {
	if index < 0 || index >= len(todos) {
		return false
	}
	if todoStatus(todos[index].Status) != "in_progress" {
		return false
	}
	if unfinished, ok := FirstUnfinishedSubStep(todos, index); ok && unfinished >= 0 {
		return false
	}
	todos[index].Status = "completed"
	if todos[index].Level == 1 {
		for i := index + 1; i < len(todos) && todos[i].Level == 1; i++ {
			if todoStatus(todos[i].Status) == "pending" {
				todos[i].Status = "in_progress"
				return true
			}
		}
		head := index - 1
		for head >= 0 && todos[head].Level == 1 {
			head--
		}
		if head >= 0 {
			if todoStatus(todos[head].Status) != "completed" {
				todos[head].Status = "in_progress"
			}
			return true
		}
		// No phase above: an orphan sub-step falls through and promotes the
		// next pending unit like a plain step, so the list keeps one current
		// item.
	}
	for i := range todos {
		if todoStatus(todos[i].Status) == "in_progress" {
			return true
		}
	}
	for i := range todos {
		if todoStatus(todos[i].Status) != "pending" {
			continue
		}
		if sub, ok := FirstUnfinishedSubStep(todos, i); ok && sub >= 0 {
			if todoStatus(todos[sub].Status) == "pending" {
				todos[sub].Status = "in_progress"
			}
			return true
		}
		todos[i].Status = "in_progress"
		return true
	}
	return true
}

func (l *Ledger) IncompleteLatestTodos() ([]TodoStepMatch, bool) {
	if l == nil {
		return nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, v := range slices.Backward(l.receipts) {
		r := v
		if !r.Success || r.ToolName != "todo_write" {
			continue
		}
		return IncompleteTodos(r.Todos), true
	}
	return nil, false
}

// IncompleteTodos returns the items of a todo list that are not completed.
func IncompleteTodos(todos []TodoItem) []TodoStepMatch {
	incomplete := make([]TodoStepMatch, 0)
	for j, t := range todos {
		status := todoStatus(t.Status)
		if status == "completed" {
			continue
		}
		incomplete = append(incomplete, TodoStepMatch{
			Found:      true,
			Index:      j + 1,
			Content:    t.Content,
			Status:     status,
			ActiveForm: t.ActiveForm,
		})
	}
	return incomplete
}

// MatchStep resolves a complete_step.step (number, title, or drift-tolerant
// variant) against a todo list, returning the matched item.
func MatchStep(step string, todos []TodoItem) (TodoStepMatch, bool) {
	m := matchTodoStep(step, todos)
	return m, m.Found
}

// MatchTodoIdentity resolves an existing todo against an updated list without
// interpreting numeric content as a 1-based step citation.
func MatchTodoIdentity(todo TodoItem, todos []TodoItem) (TodoStepMatch, bool) {
	for i, candidate := range todos {
		if sameTodoIdentity(todo, candidate) {
			return todoMatchAt(i+1, candidate), true
		}
	}
	found := -1
	for i, candidate := range todos {
		match := TodoStepMatch{Content: candidate.Content, ActiveForm: candidate.ActiveForm}
		if !todoContentRelates(todo, match) {
			continue
		}
		if found >= 0 && found != i {
			return TodoStepMatch{}, false
		}
		found = i
	}
	if found < 0 {
		return TodoStepMatch{}, false
	}
	candidate := todos[found]
	return todoMatchAt(found+1, candidate), true
}

// PreservesCompletedTodoPositions reports whether every previously completed
// item remains completed at the same index in the replacement list. Completed
// sub-steps can sit behind a pending phase header, so this checks every item
// rather than assuming the literal list begins with completed statuses.
func PreservesCompletedTodoPositions(previous, next []TodoItem) bool {
	for i, todo := range previous {
		if todoStatus(todo.Status) != "completed" {
			continue
		}
		if i >= len(next) || todoStatus(next[i].Status) != "completed" {
			return false
		}
		match, found := MatchTodoIdentity(todo, next)
		if !found || match.Index != i+1 {
			return false
		}
	}
	return true
}

func (l *Ledger) MatchLatestTodoStep(step string) (TodoStepMatch, bool) {
	step = strings.TrimSpace(step)
	if l == nil || step == "" {
		return TodoStepMatch{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, v := range slices.Backward(l.receipts) {
		r := v
		if !r.Success || r.ToolName != "todo_write" {
			continue
		}
		return matchTodoStep(step, r.Todos), true
	}
	return TodoStepMatch{}, false
}

// LatestTodos returns the todo list from this turn's latest successful todo_write.
func (l *Ledger) LatestTodos() ([]TodoItem, bool) {
	if l == nil {
		return nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, v := range slices.Backward(l.receipts) {
		r := v
		if r.Success && r.ToolName == "todo_write" {
			return append([]TodoItem(nil), r.Todos...), true
		}
	}
	return nil, false
}

// UnverifiedCompletedTodos reports current completed todos that transitioned
// from the latest prior successful todo_write receipt without a matching
// successful complete_step receipt earlier in the same turn. If this turn has no
// prior todo_write baseline, hasBaseline is false and callers should preserve
// the existing loose validation behavior.
func (l *Ledger) UnverifiedCompletedTodos(current []TodoItem) (missing []TodoStepMatch, hasBaseline bool) {
	current = normalizeTodos(current)
	if l == nil {
		return nil, false
	}

	l.mu.Lock()
	receipts := append([]Receipt(nil), l.receipts...)
	l.mu.Unlock()

	var previous []TodoItem
	baseline := -1
	for i, v := range slices.Backward(receipts) {
		r := v
		if !r.Success || r.ToolName != "todo_write" {
			continue
		}
		previous = r.Todos
		baseline = i
		hasBaseline = true
		break
	}
	if !hasBaseline {
		return nil, false
	}

	for i, t := range current {
		if todoStatus(t.Status) != "completed" {
			continue
		}
		index := i + 1
		if previousTodoCompleted(index, t, previous) {
			continue
		}
		if hasSuccessfulCompleteStepForTodo(receipts, index, current) {
			continue
		}
		if hasFailedCompleteStepRecoveryForTodo(receipts, baseline, index, current) {
			continue
		}
		missing = append(missing, TodoStepMatch{
			Found:      true,
			Index:      index,
			Content:    t.Content,
			Status:     todoStatus(t.Status),
			ActiveForm: t.ActiveForm,
		})
	}
	return missing, true
}

// completeStepIdentity is the citation a receipt records, most stable first: a
// step id survives a replan, an index survives a retitle, the title survives
// neither.
func completeStepIdentity(fields map[string]json.RawMessage) string {
	if id := stringField(fields, "step_id"); id != "" {
		return id
	}
	if n, ok := intField(fields, "step_index"); ok && n > 0 {
		return strconv.Itoa(n)
	}
	return stringField(fields, "step")
}

func todoItemsField(fields map[string]json.RawMessage, key string) []TodoItem {
	raw, ok := fields[key]
	if !ok {
		return nil
	}
	var todos []TodoItem
	if err := json.Unmarshal(raw, &todos); err != nil {
		return nil
	}
	return normalizeTodos(todos)
}

// A failed complete_step can unlock todo recovery only when the payload had the
// same structural proof shape Execute expects before host verification runs.
// completeStepCitedChecks returns the commands a completion named as its
// verification. Whether each one actually ran is the ledger's question, asked
// where it matters rather than here.
func completeStepCitedChecks(fields map[string]json.RawMessage) []string {
	raw, ok := fields["evidence"]
	if !ok {
		return nil
	}
	var items []struct {
		Kind    string `json:"kind"`
		Command string `json:"command"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var out []string
	for _, item := range items {
		command := strings.TrimSpace(item.Command)
		if strings.EqualFold(strings.TrimSpace(item.Kind), "verification") && command != "" {
			out = append(out, command)
		}
	}
	return out
}

func completeStepHasProof(fields map[string]json.RawMessage) bool {
	if strings.TrimSpace(stringField(fields, "result")) == "" {
		return false
	}
	raw, ok := fields["evidence"]
	if !ok {
		return false
	}
	var items []struct {
		Kind    string   `json:"kind"`
		Summary string   `json:"summary"`
		Command string   `json:"command"`
		Paths   []string `json:"paths"`
	}
	if err := json.Unmarshal(raw, &items); err != nil || len(items) == 0 {
		return false
	}
	for _, item := range items {
		kind := strings.TrimSpace(item.Kind)
		if kind == "" || strings.TrimSpace(item.Summary) == "" {
			return false
		}
		switch kind {
		case "verification":
			if strings.TrimSpace(item.Command) == "" {
				return false
			}
		case "diff", "files":
			if len(normalizePaths(item.Paths)) == 0 {
				return false
			}
		case "manual":
			// Summary is enough for manual evidence.
		default:
			return false
		}
	}
	return true
}

func normalizeTodos(todos []TodoItem) []TodoItem {
	out := make([]TodoItem, 0, len(todos))
	for _, t := range todos {
		t.Content = strings.TrimSpace(t.Content)
		t.Status = strings.TrimSpace(t.Status)
		t.ActiveForm = strings.TrimSpace(t.ActiveForm)
		out = append(out, t)
	}
	return out
}

func todoStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return "pending"
	}
	return status
}

func previousTodoCompleted(index int, current TodoItem, previous []TodoItem) bool {
	if index >= 1 && index <= len(previous) {
		p := previous[index-1]
		if todoStatus(p.Status) == "completed" && sameTodoIdentity(current, p) {
			return true
		}
	}
	for _, p := range previous {
		if todoStatus(p.Status) == "completed" && sameTodoIdentity(current, p) {
			return true
		}
	}
	return false
}

func latestTodoStep(step string, receipts []Receipt) TodoStepMatch {
	for _, v := range slices.Backward(receipts) {
		r := v
		if !r.Success || r.ToolName != "todo_write" {
			continue
		}
		return matchTodoStep(step, r.Todos)
	}
	return TodoStepMatch{}
}

// matchTodoStep resolves a citation to a todo. A stable id wins outright; only
// a list without ids falls back to position and wording, which a retitle or an
// inserted step silently invalidates.
func matchTodoStep(step string, todos []TodoItem) TodoStepMatch {
	if m, ok := MatchStepID(step, todos); ok {
		return m
	}
	if n, ok := parseStepIndex(normalizeStepText(step)); ok && n >= 1 && n <= len(todos) {
		t := todos[n-1]
		return todoMatchAt(n, t)
	}
	for i, t := range todos {
		if sameStepText(step, t.Content) || sameStepText(step, t.ActiveForm) {
			return todoMatchAt(i+1, t)
		}
	}
	// Containment fallback for wording drift; an ambiguous citation (containing
	// or contained by two different todos) stays unmatched rather than guessing.
	norm := normalizeStepText(step)
	found := -1
	for i, t := range todos {
		if stepTextContains(norm, normalizeStepText(t.Content)) || stepTextContains(norm, normalizeStepText(t.ActiveForm)) {
			if found >= 0 && found != i {
				return TodoStepMatch{}
			}
			found = i
		}
	}
	if found >= 0 {
		t := todos[found]
		return todoMatchAt(found+1, t)
	}
	return TodoStepMatch{}
}

func parseStepIndex(step string) (int, bool) {
	step = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(step), "."))
	n, err := strconv.Atoi(step)
	return n, err == nil
}

// normalizeStepText folds the drift models introduce when citing a todo:
// fullwidth ASCII forms → halfwidth (："５ → :"5), all whitespace dropped,
// case-insensitive.
func normalizeStepText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0xFF01 && r <= 0xFF5E {
			r -= 0xFEE0
		}
		b.WriteRune(r)
	}
	return strings.ToLower(strings.Join(strings.Fields(b.String()), ""))
}

func sameStepText(a, b string) bool {
	na, nb := normalizeStepText(a), normalizeStepText(b)
	return na != "" && na == nb
}

// stepTextContains: substring match between normalized texts, but only when the
// shorter side is substantial enough (≥6 runes) to not match by accident.
func stepTextContains(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	short := a
	if utf8.RuneCountInString(b) < utf8.RuneCountInString(a) {
		short = b
	}
	if utf8.RuneCountInString(short) < 6 {
		return false
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}
