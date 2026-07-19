package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"

	"reasonix/internal/provider"
	"reasonix/internal/shellparse"
	"reasonix/internal/shellsafe"
)

// TodoItem mirrors the todo_write item shape the host needs for step matching.
type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm,omitempty"`
	Level      int    `json:"level,omitempty"`
}

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

// todoSegment is one serial unit of a task list: a level-0 phase header plus
// its level-1 sub-steps, or a single plain step. end is exclusive.
type todoSegment struct {
	head int
	end  int
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

// TodoStepMatch is the result of matching complete_step.step against the latest
// successful todo_write list in this turn.
type TodoStepMatch struct {
	Found      bool
	Index      int
	Content    string
	Status     string
	ActiveForm string
}

// Receipt is the host-runtime record of one tool call. It stays in memory for
// the current agent turn and is not serialized into prompts or session state.
type Receipt struct {
	ToolName  string          `json:"tool_name"`
	Args      json.RawMessage `json:"args,omitempty"`
	Success   bool            `json:"success"`
	Command   string          `json:"command,omitempty"`
	Step      string          `json:"step,omitempty"`
	StepProof bool            `json:"step_proof,omitempty"`
	TodoStep  *TodoStepMatch  `json:"todo_step,omitempty"`
	Paths     []string        `json:"paths,omitempty"`
	Read      bool            `json:"read,omitempty"`
	Write     bool            `json:"write,omitempty"`
	Mutation  bool            `json:"mutation,omitempty"`
	Todos     []TodoItem      `json:"todos,omitempty"`
	// OutputBytes is the host-observed length of the tool's (redacted, trimmed)
	// output. Content-evidence checks require it to be non-zero so a command
	// that printed nothing (head -n 0, >/dev/null) can never count as reading.
	OutputBytes int `json:"output_bytes,omitempty"`
}

// BackgroundLease identifies a background job whose evidence was provisionally
// merged into the current turn's ledger. The host commits these leases only
// after the turn passes its delivery gates, so a failed turn leaves the job's
// evidence collectable again.
type BackgroundLease struct {
	Session string
	JobID   string
}

// DeliveryCheckpoint is the compact, persistence-safe state carried across
// runs of one host-owned Goal. It intentionally stores no raw tool arguments or
// output. PendingMutation means a previously observed change still needs fresh
// verification, review, and sign-off before the Goal can finalize.
type DeliveryCheckpoint struct {
	ScopeID             string `json:"scopeID,omitempty"`
	CriteriaEstablished bool   `json:"criteriaEstablished,omitempty"`
	WorkObserved        bool   `json:"workObserved,omitempty"`
	MutationObserved    bool   `json:"mutationObserved,omitempty"`
	PendingMutation     bool   `json:"pendingMutation,omitempty"`
}

// Ledger stores the receipts available to complete_step for the current turn.
type Ledger struct {
	mu               sync.Mutex
	receipts         []Receipt
	backgroundLeases []BackgroundLease
}

func NewLedger() *Ledger { return &Ledger{} }

// Reset clears receipts and background leases between user turns.
func (l *Ledger) Reset() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.receipts = nil
	l.backgroundLeases = nil
}

// ResetBackgroundLeases starts a new run inside the same delivery scope. The
// durable receipts remain available, while per-run job leases must be collected
// and committed independently.
func (l *Ledger) ResetBackgroundLeases() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.backgroundLeases = nil
	l.mu.Unlock()
}

// NoteBackgroundLease records that a background job's evidence was merged into
// this turn. It returns false when the job was already noted this turn so the
// caller can skip a duplicate merge — collection is idempotent within a turn,
// while a fresh turn (after Reset) leases again.
func (l *Ledger) NoteBackgroundLease(session, jobID string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, lease := range l.backgroundLeases {
		if lease.Session == session && lease.JobID == jobID {
			return false
		}
	}
	l.backgroundLeases = append(l.backgroundLeases, BackgroundLease{Session: session, JobID: jobID})
	return true
}

// BackgroundLeases returns the background jobs merged into this turn, for the
// host to commit once the turn's delivery gates pass.
func (l *Ledger) BackgroundLeases() []BackgroundLease {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.backgroundLeases) == 0 {
		return nil
	}
	out := make([]BackgroundLease, len(l.backgroundLeases))
	copy(out, l.backgroundLeases)
	return out
}

// Record appends a receipt. Failed receipts are retained for auditability but
// are never accepted by the HasSuccessful* matchers.
func (l *Ledger) Record(r Receipt) {
	if l == nil {
		return
	}
	r.Command = strings.TrimSpace(r.Command)
	r.Step = strings.TrimSpace(r.Step)
	r.Paths = normalizePaths(r.Paths)
	r.Todos = normalizeTodos(r.Todos)
	if r.Args != nil {
		cp := make(json.RawMessage, len(r.Args))
		copy(cp, r.Args)
		r.Args = cp
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if r.ToolName == "complete_step" && r.Step != "" && r.TodoStep == nil {
		if match := latestTodoStep(r.Step, l.receipts); match.Found {
			r.TodoStep = &match
		}
	}
	l.receipts = append(l.receipts, r)
}

// Len returns the number of receipts recorded this turn, giving callers a
// stable index to pass to the *Since matchers.
func (l *Ledger) Len() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.receipts)
}

// HasWriteOrCommandSince reports whether a successful write or command receipt
// was recorded at or after index — host-observable progress, as opposed to
// bookkeeping receipts (todo_write, complete_step, ask), which carry neither a
// write flag nor a command.
func (l *Ledger) HasWriteOrCommandSince(index int) bool {
	if l == nil {
		return false
	}
	if index < 0 {
		index = 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := index; i < len(l.receipts); i++ {
		r := l.receipts[i]
		if r.Success && (r.Mutation || r.Write || r.Command != "") {
			return true
		}
	}
	return false
}

// SuccessfulProgressSignaturesSince returns stable identities for successful
// host-observed work recorded at or after index. Callers can keep a per-turn set
// of these signatures so a new read, command, or mutation renews an execution
// lease while exact repeats do not masquerade as progress.
func (l *Ledger) SuccessfulProgressSignaturesSince(index int) []string {
	if l == nil {
		return nil
	}
	if index < 0 {
		index = 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for i := index; i < len(l.receipts); i++ {
		if sig, ok := progressReceiptSignature(l.receipts[i]); ok {
			out = append(out, sig)
		}
	}
	return out
}

func progressReceiptSignature(r Receipt) (string, bool) {
	if !r.Success {
		return "", false
	}
	kind := ""
	switch {
	case r.Mutation || r.Write:
		kind = "mutation"
	case r.Command != "":
		kind = "command"
	case r.Read && r.OutputBytes > 0:
		kind = "read"
	default:
		return "", false
	}
	payload := strings.TrimSpace(string(r.Args))
	var decoded any
	if json.Unmarshal(r.Args, &decoded) == nil {
		if canonical, err := json.Marshal(decoded); err == nil {
			payload = string(canonical)
		}
	}
	sum := sha256.Sum256([]byte(kind + "\x00" + r.ToolName + "\x00" + payload))
	return fmt.Sprintf("%x", sum), true
}

func (l *Ledger) HasSuccessfulCommand(command string) bool {
	command = strings.TrimSpace(command)
	if l == nil || command == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if r.Success && r.ToolName == "bash" && CommandMatches(command, r.Command) {
			return true
		}
	}
	return false
}

// HasFailedCommand reports whether the cited command ran this turn but exited
// non-zero — so callers can distinguish "ran and failed" from "never ran".
func (l *Ledger) HasFailedCommand(command string) bool {
	command = strings.TrimSpace(command)
	if l == nil || command == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if !r.Success && r.ToolName == "bash" && CommandMatches(command, r.Command) {
			return true
		}
	}
	return false
}

// SuccessfulCommands returns up to limit successful bash commands from this
// turn, most recent first, for self-correction hints in rejection errors.
func (l *Ledger) SuccessfulCommands(limit int) []string {
	if l == nil || limit <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for i := len(l.receipts) - 1; i >= 0 && len(out) < limit; i-- {
		r := l.receipts[i]
		if r.Success && r.ToolName == "bash" && r.Command != "" {
			out = append(out, r.Command)
		}
	}
	return out
}

// TouchedPaths returns up to limit distinct paths from this turn's successful
// receipts, most recent first; writtenOnly restricts it to writer receipts.
func (l *Ledger) TouchedPaths(limit int, writtenOnly bool) []string {
	if l == nil || limit <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for i := len(l.receipts) - 1; i >= 0 && len(out) < limit; i-- {
		r := l.receipts[i]
		if !r.Success || (writtenOnly && !r.Write) || (!writtenOnly && !r.Read && !r.Write) {
			continue
		}
		for _, p := range r.Paths {
			if !seen[p] && len(out) < limit {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// HasSuccessfulBashMentioningPaths reports whether every path appears in some
// successful bash command this turn — files created or edited through shell
// redirection (`seq … > file`) leave no reader/writer receipt, so the command
// text naming the path is the receipt.
func (l *Ledger) HasSuccessfulBashMentioningPaths(paths []string) bool {
	wanted := normalizePaths(paths)
	if l == nil || len(wanted) == 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, p := range wanted {
		needle := strings.ToLower(filepath.ToSlash(p))
		found := false
		for _, r := range l.receipts {
			if !r.Success || r.ToolName != "bash" {
				continue
			}
			command := strings.ToLower(strings.ReplaceAll(r.Command, `\`, `/`))
			if strings.Contains(command, needle) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (l *Ledger) HasSuccessfulCommandAfter(command string, after int) bool {
	command = strings.TrimSpace(command)
	if l == nil || command == "" {
		return false
	}
	start := after + 1
	if start < 0 {
		start = 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	for i := start; i < len(l.receipts); i++ {
		r := l.receipts[i]
		if r.Success && r.ToolName == "bash" && CommandMatches(command, r.Command) {
			return true
		}
	}
	return false
}

func (l *Ledger) HasSuccessfulCompleteStepAfter(after int) bool {
	if l == nil {
		return false
	}
	start := after + 1
	if start < 0 {
		start = 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	for i := start; i < len(l.receipts); i++ {
		r := l.receipts[i]
		if r.Success && r.ToolName == "complete_step" {
			return true
		}
	}
	return false
}

// HasSuccessfulDeliverySignoffAfter reports whether a successful complete_step
// after the latest mutation cites a verification command that also succeeded
// after that mutation. complete_step already validates the cited command against
// host receipts; the additional ordering check prevents a pre-change test from
// signing off changed code in the delivery profile.
func (l *Ledger) HasSuccessfulDeliverySignoffAfter(after int) bool {
	if l == nil {
		return false
	}
	start := after + 1
	if start < 0 {
		start = 0
	}

	l.mu.Lock()
	receipts := append([]Receipt(nil), l.receipts...)
	l.mu.Unlock()
	for i := start; i < len(receipts); i++ {
		r := receipts[i]
		if !r.Success || r.ToolName != "complete_step" {
			continue
		}
		if after >= 0 && !receiptsReviewChanges(receipts, start, i, after) {
			continue
		}
		for _, command := range completeStepVerificationCommands(r.Args) {
			if !bashCommandIsVerification(command) {
				continue
			}
			for j := start; j < i; j++ {
				candidate := receipts[j]
				if candidate.Success && candidate.ToolName == "bash" && CommandMatches(command, candidate.Command) {
					return true
				}
			}
		}
	}
	return false
}

// HasSuccessfulReviewAfter reports whether the changed result was inspected
// after the latest mutation. A read of a touched path is sufficient; git/diff
// inspection commands cover shell-driven or delegated mutations whose paths are
// not knowable to the host. A negative index is the restored-checkpoint
// baseline: the mutation predates this ledger (controller rebuild or cold
// resume), so any successful review-shaped receipt counts.
func (l *Ledger) HasSuccessfulReviewAfter(after int) bool {
	if l == nil {
		return false
	}
	start := after + 1
	if start < 0 {
		start = 0
	}

	l.mu.Lock()
	receipts := append([]Receipt(nil), l.receipts...)
	l.mu.Unlock()
	if after >= len(receipts) {
		return false
	}
	return receiptsReviewChanges(receipts, start, len(receipts), after)
}

func receiptsReviewChanges(receipts []Receipt, start, end, mutationIndex int) bool {
	if mutationIndex >= len(receipts) {
		return false
	}
	// A negative mutationIndex is the restored-checkpoint baseline: the
	// mutation's receipt is not in this ledger, so its touched paths are
	// unknowable and any successful review-shaped receipt counts.
	var wanted map[string]bool
	if mutationIndex >= 0 {
		wanted = pathSet(receipts[mutationIndex].Paths)
	}
	for i := start; i < end && i < len(receipts); i++ {
		r := receipts[i]
		if !r.Success {
			continue
		}
		if r.ToolName == "bash" && commandReviewsChanges(r.Command) {
			return true
		}
		if r.ToolName == "bash" && len(wanted) > 0 && !bashMayMutate(r.Command) && commandMentionsPaths(r.Command, wanted) {
			return true
		}
		if !r.Read {
			continue
		}
		if len(wanted) == 0 {
			return true
		}
		for _, p := range r.Paths {
			if wanted[p] {
				return true
			}
		}
	}
	return false
}

func (l *Ledger) HasSuccessfulTodoWrite() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if r.Success && r.ToolName == "todo_write" {
			return true
		}
	}
	return false
}

// HasSuccessfulAcceptanceCriteria reports whether the current turn established
// a non-empty task list. Delivery mode uses that list as its host-observable
// acceptance contract before permitting state-changing work.
func (l *Ledger) HasSuccessfulAcceptanceCriteria() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if r.Success && r.ToolName == "todo_write" && len(r.Todos) > 0 {
			return true
		}
	}
	return false
}

// HasSuccessfulTodoProgressReceipt reports whether any successful receipt in
// the turn reflects execution progress rather than read-only context gathering
// or a bare todo snapshot.
func (l *Ledger) HasSuccessfulTodoProgressReceipt() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if !r.Success || r.ToolName == "todo_write" || r.Read {
			continue
		}
		return true
	}
	return false
}

func (l *Ledger) IncompleteLatestTodos() ([]TodoStepMatch, bool) {
	if l == nil {
		return nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.receipts) - 1; i >= 0; i-- {
		r := l.receipts[i]
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
			return TodoStepMatch{Found: true, Index: i + 1, Content: candidate.Content, Status: candidate.Status, ActiveForm: candidate.ActiveForm}, true
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
	return TodoStepMatch{Found: true, Index: found + 1, Content: candidate.Content, Status: candidate.Status, ActiveForm: candidate.ActiveForm}, true
}

// HasAnySuccessfulReceipt reports whether any tool succeeded this turn — the
// signal that the turn did real work, not pure conversation.
func (l *Ledger) HasAnySuccessfulReceipt() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if r.Success {
			return true
		}
	}
	return false
}

// HasSuccessfulWorkReceipt excludes workflow bookkeeping and reports whether
// the assistant actually inspected, executed, or changed something this turn.
// Delivery mode uses it to reject text-only claims for technical tasks while
// still allowing ordinary conversation to finish without tools.
func (l *Ledger) HasSuccessfulWorkReceipt() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if !r.Success {
			continue
		}
		switch r.ToolName {
		case "ask", "todo_write", "complete_step":
			continue
		}
		return true
	}
	return false
}

// HasSuccessfulVerificationCommand reports whether the turn ran at least one
// command classified as verification rather than inspection or mutation.
func (l *Ledger) HasSuccessfulVerificationCommand() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if r.Success && r.ToolName == "bash" && bashCommandIsVerification(r.Command) {
			return true
		}
	}
	return false
}

func (l *Ledger) HasSuccessfulWrite(paths []string) bool {
	return l.hasSuccessfulPaths(paths, func(r Receipt) bool { return r.Write })
}

func (l *Ledger) HasSuccessfulReadOrWrite(paths []string) bool {
	return l.hasSuccessfulPaths(paths, func(r Receipt) bool { return r.Read || r.Write })
}

func (l *Ledger) LatestSuccessfulWriteIndex(paths []string) (int, bool) {
	wanted := pathSet(normalizePaths(paths))
	if l == nil || len(wanted) == 0 {
		return 0, false
	}
	latest := -1

	l.mu.Lock()
	defer l.mu.Unlock()
	for i, r := range l.receipts {
		if !r.Success || !r.Write {
			continue
		}
		for _, p := range r.Paths {
			if wanted[p] {
				latest = i
				break
			}
		}
	}
	return latest, latest >= 0
}

// HasSuccessfulAnchorRefreshReadAfter reports whether read_file refreshed a
// wanted path after the given receipt index. Windowed reads and grep/ls receipts
// are deliberately not enough for same-turn anchor edits: they may have observed
// a different region than the next old_string/delete_range anchor.
func (l *Ledger) HasSuccessfulAnchorRefreshReadAfter(paths []string, after int) bool {
	wanted := pathSet(normalizePaths(paths))
	if l == nil || len(wanted) == 0 {
		return false
	}
	start := after + 1
	if start < 0 {
		start = 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	for i := start; i < len(l.receipts); i++ {
		r := l.receipts[i]
		if !r.Success || !anchorRefreshRead(r) {
			continue
		}
		for _, p := range r.Paths {
			if wanted[p] {
				return true
			}
		}
	}
	return false
}

func anchorRefreshRead(r Receipt) bool {
	if r.ToolName != "read_file" || !r.Read {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(r.Args, &fields); err != nil {
		return false
	}
	if limit, ok := intField(fields, "limit"); ok && limit > 0 {
		return false
	}
	if offset, ok := intField(fields, "offset"); ok && offset > 0 {
		return false
	}
	return true
}

func (l *Ledger) LatestSuccessfulWriterIndex() (int, bool) {
	if l == nil {
		return 0, false
	}
	latest := -1

	l.mu.Lock()
	defer l.mu.Unlock()
	for i, r := range l.receipts {
		if r.Success && r.Write {
			latest = i
		}
	}
	return latest, latest >= 0
}

// LatestSuccessfulMutationIndex returns the most recent host-observed
// state-changing call. It includes known file writers, writer-capable delegated
// or external tools, and bash commands that are not demonstrably observational
// or verification-only.
func (l *Ledger) LatestSuccessfulMutationIndex() (int, bool) {
	if l == nil {
		return 0, false
	}
	latest := -1
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, r := range l.receipts {
		if r.Success && r.Mutation {
			latest = i
		}
	}
	return latest, latest >= 0
}

func (l *Ledger) MatchLatestTodoStep(step string) (TodoStepMatch, bool) {
	step = strings.TrimSpace(step)
	if l == nil || step == "" {
		return TodoStepMatch{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.receipts) - 1; i >= 0; i-- {
		r := l.receipts[i]
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
	for i := len(l.receipts) - 1; i >= 0; i-- {
		r := l.receipts[i]
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
	for i := len(receipts) - 1; i >= 0; i-- {
		r := receipts[i]
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

func hasFailedCompleteStepRecoveryForTodo(receipts []Receipt, baseline int, index int, current []TodoItem) bool {
	for i := baseline + 1; i < len(receipts); i++ {
		r := receipts[i]
		if r.Success || r.ToolName != "complete_step" || strings.TrimSpace(r.Step) == "" || !r.StepProof {
			continue
		}
		if !hasSuccessfulProgressBeforeReceipt(receipts, baseline, i) {
			continue
		}
		if r.TodoStep != nil && r.TodoStep.Found {
			if index < 1 || index > len(current) {
				continue
			}
			if sameTodoMatch(current[index-1], *r.TodoStep) {
				return true
			}
			if !todoContentRelates(current[index-1], *r.TodoStep) {
				continue
			}
		}
		match := matchTodoStep(r.Step, current)
		if match.Found && match.Index == index {
			return true
		}
	}
	return false
}

// Recovery only trusts progress that happened before the failed sign-off.
// Later unrelated work must not retroactively authorize an earlier completion.
func hasSuccessfulProgressBeforeReceipt(receipts []Receipt, baseline int, before int) bool {
	start := baseline + 1
	if start < 0 {
		start = 0
	}
	for i := start; i < before && i < len(receipts); i++ {
		r := receipts[i]
		if !r.Success || r.ToolName == "todo_write" || r.ToolName == "complete_step" || r.Read {
			continue
		}
		return true
	}
	return false
}

func (l *Ledger) hasSuccessfulPaths(paths []string, accept func(Receipt) bool) bool {
	wanted := pathSet(normalizePaths(paths))
	if l == nil || len(wanted) == 0 {
		return false
	}
	found := map[string]bool{}

	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if !r.Success || !accept(r) {
			continue
		}
		for _, p := range r.Paths {
			if _, ok := wanted[p]; ok {
				found[p] = true
			}
		}
	}
	return len(found) == len(wanted)
}

type contextKey struct{}
type sessionMessagesKey struct{}
type deliveryProfileKey struct{}
type todoStateKey struct{}

func WithLedger(ctx context.Context, ledger *Ledger) context.Context {
	if ledger == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, ledger)
}

func FromContext(ctx context.Context) (*Ledger, bool) {
	ledger, ok := ctx.Value(contextKey{}).(*Ledger)
	return ledger, ok && ledger != nil
}

// WithDeliveryProfile marks tool execution as subject to the delivery-first
// final-readiness contract. Tools use this only for stricter evidence validation;
// it is ephemeral host state and is never serialized into sessions or prompts.
func WithDeliveryProfile(ctx context.Context) context.Context {
	return context.WithValue(ctx, deliveryProfileKey{}, true)
}

// DeliveryProfileFromContext reports whether the current tool call must produce
// evidence that the delivery final-readiness gate can accept.
func DeliveryProfileFromContext(ctx context.Context) bool {
	enabled, _ := ctx.Value(deliveryProfileKey{}).(bool)
	return enabled
}

// WithSessionMessages attaches the full conversation history so verifyStepEvidence
// can fall back to scanning the transcript when the per-turn ledger misses a
// command (cross-turn references, non-bash tool calls, truncated command strings).
func WithSessionMessages(ctx context.Context, msgs []provider.Message) context.Context {
	return context.WithValue(ctx, sessionMessagesKey{}, msgs)
}

// SessionMessagesFromContext retrieves the conversation history attached by
// WithSessionMessages.
func SessionMessagesFromContext(ctx context.Context) ([]provider.Message, bool) {
	msgs, ok := ctx.Value(sessionMessagesKey{}).([]provider.Message)
	return msgs, ok
}

// WithTodoState attaches the host's canonical task list to a tool call. The
// per-turn ledger resets between user messages, while unfinished tasks remain
// active across those turns.
func WithTodoState(ctx context.Context, todos []TodoItem) context.Context {
	return context.WithValue(ctx, todoStateKey{}, append([]TodoItem(nil), todos...))
}

// TodoStateFromContext returns a copy of the host's canonical task list.
func TodoStateFromContext(ctx context.Context) ([]TodoItem, bool) {
	todos, ok := ctx.Value(todoStateKey{}).([]TodoItem)
	return append([]TodoItem(nil), todos...), ok
}

// PathsProvenInSession reports whether every path is covered by a successful
// (non-errored) tool call somewhere in msgs — the cross-turn fallback for diff
// and files evidence, mirroring verifyCommandFromSession for the per-turn
// ledger's path receipts (which reset each turn). wantWrite restricts to writer
// tools (diff); false accepts a reader or writer (files).
func PathsProvenInSession(msgs []provider.Message, paths []string, wantWrite bool) bool {
	wanted := pathSet(normalizePaths(paths))
	if len(wanted) == 0 {
		return false
	}
	failed := failedSessionCallIDs(msgs)
	found := map[string]bool{}
	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			if failed[tc.ID] {
				continue
			}
			r := ReceiptFromToolCall(tc.Name, json.RawMessage(tc.Arguments), true, false)
			if wantWrite && !r.Write {
				continue
			}
			if !wantWrite && !r.Read && !r.Write {
				continue
			}
			for _, p := range normalizePaths(r.Paths) {
				if _, ok := wanted[p]; ok {
					found[p] = true
				}
			}
		}
	}
	return len(found) == len(wanted)
}

func failedSessionCallIDs(msgs []provider.Message) map[string]bool {
	failed := map[string]bool{}
	for _, msg := range msgs {
		if msg.Role != provider.RoleTool || msg.ToolCallID == "" {
			continue
		}
		if strings.HasPrefix(msg.Content, "error:") || strings.HasPrefix(msg.Content, "blocked:") {
			failed[msg.ToolCallID] = true
		}
	}
	return failed
}

func ReceiptFromToolCall(toolName string, args json.RawMessage, success bool, readOnly bool) Receipt {
	r := Receipt{
		ToolName: toolName,
		Args:     args,
		Success:  success,
		Mutation: ToolCallMutates(toolName, args, readOnly),
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err == nil {
		if toolName == "bash" {
			r.Command = stringField(fields, "command")
		}
		if toolName == "complete_step" {
			r.Step = completeStepIdentity(fields)
			r.StepProof = completeStepHasProof(fields)
		}
		if toolName == "todo_write" {
			r.Todos = todoItemsField(fields, "todos")
		}
		r.Paths = extractPaths(fields)
	}

	if isWriterTool(toolName) {
		r.Write = true
	} else if isReadReceipt(toolName, readOnly) {
		r.Read = true
	}
	return r
}

// ToolCallMutates is the delivery profile's conservative state-change
// classifier. Trusted read-only tools never mutate. Meta tools that only
// delegate (task, run_skill, review, …) never mutate by themselves — real
// writes arrive via child evidence merge. Writer-capable tools do mutate,
// except for bash commands that the host can prove are inspection or
// verification commands.
func ToolCallMutates(toolName string, args json.RawMessage, readOnly bool) bool {
	if readOnly {
		return false
	}
	if IsNonMutationMetaTool(toolName) {
		return false
	}
	switch toolName {
	case "ask", "todo_write", "complete_step", "bash_output", "wait":
		return false
	case "bash":
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(args, &fields); err != nil {
			return true
		}
		return bashMayMutate(stringField(fields, "command"))
	default:
		return true
	}
}

// ToolCallRequiresDeliveryCriteria reports whether a call begins execution
// work that needs an acceptance contract. Mutations always qualify; verification
// commands also qualify even though they are intentionally not mutations.
func ToolCallRequiresDeliveryCriteria(toolName string, args json.RawMessage, readOnly bool) bool {
	if ToolCallMutates(toolName, args, readOnly) {
		return true
	}
	if toolName != "bash" {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return true
	}
	return bashCommandIsVerification(stringField(fields, "command"))
}

// BashToolCallMixesMutationAndVerification reports whether a bash call combines
// a host-recognized verifier with another segment the host cannot prove is
// read-only. Delivery mode blocks this shape before execution. Besides avoiding
// accidental workspace changes during a check, this keeps scratch-file setup
// (for example, writing /tmp/check.js before node --check) from becoming the
// latest opaque mutation and invalidating otherwise valid delivery evidence.
func BashToolCallMixesMutationAndVerification(args json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return false
	}
	command := stringField(fields, "command")
	return bashContainsVerificationSegment(command) && bashMayMutate(command)
}

// BashToolCallMasksVerificationExit reports the common `check; echo $?` shape.
// The trailing reporter makes the shell call itself succeed even when the
// verifier failed, so a successful tool receipt cannot prove the check passed.
// It is separated from the broader mixed-command classifier so the agent can
// give a precise recovery instruction instead of inviting repeated rewrites.
func BashToolCallMasksVerificationExit(args json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return false
	}
	command := strings.TrimSpace(stringField(fields, "command"))
	if command == "" || !bashContainsVerificationSegment(command) {
		return false
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok {
		return false
	}
	seenVerifier := false
	for _, segment := range segments {
		normalized, _ := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
		argv, malformed := shellparse.StaticFields(normalized)
		if malformed == "" && bashSegmentIsVerification(argv) {
			seenVerifier = true
			continue
		}
		if !seenVerifier || !strings.Contains(segment, "$?") {
			continue
		}
		lower := strings.ToLower(strings.TrimSpace(segment))
		if strings.HasPrefix(lower, "echo ") || strings.HasPrefix(lower, "printf ") {
			return true
		}
	}
	return false
}

// BashToolCallUsesOpaqueInlineInterpreter reports whether a bash call executes
// source supplied directly on an interpreter's command line. Delivery mode
// cannot prove whether snippets such as node -e or python -c only inspect state
// or also write files. Letting them run and then treating them as opaque
// mutations invalidates otherwise valid review/verification receipts, while
// treating them as read-only would create a delivery bypass. The agent blocks
// this shape before execution and directs callers to auditable file tools,
// script files, or conventional verifier commands instead.
func BashToolCallUsesOpaqueInlineInterpreter(args json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return false
	}
	command := strings.TrimSpace(stringField(fields, "command"))
	if command == "" {
		return false
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok {
		return false
	}
	for _, segment := range segments {
		normalized, _ := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
		argv, malformed := shellparse.StaticFields(normalized)
		if malformed != "" || len(argv) == 0 {
			continue
		}
		base := strings.ToLower(filepath.Base(argv[0]))
		args := argv[1:]
		switch base {
		case "node", "bun":
			if hasCommandArg(args, "-e", "--eval", "-p", "--print") {
				return true
			}
		case "python", "python3", "ruby", "perl":
			if hasCommandArg(args, "-c", "-e") {
				return true
			}
		case "php":
			if hasCommandArg(args, "-r") {
				return true
			}
		case "deno":
			if len(args) > 0 && strings.EqualFold(args[0], "eval") {
				return true
			}
		}
	}
	return false
}

func bashContainsVerificationSegment(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok {
		return false
	}
	for _, segment := range segments {
		normalized, _ := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
		fields, malformed := shellparse.StaticFields(normalized)
		if malformed == "" && bashSegmentIsVerification(fields) {
			return true
		}
	}
	return false
}

func bashMayMutate(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return true
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok || len(segments) == 0 {
		return true
	}
	for _, segment := range segments {
		normalized, safeRedirects := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
		if !safeRedirects || shellsafe.ContainsShellSyntax(normalized) {
			return true
		}
		fields, malformed := shellparse.StaticFields(normalized)
		if malformed != "" || len(fields) == 0 {
			return true
		}
		if bashSegmentIsVerification(fields) {
			continue
		}
		base, sub, readOnly := shellsafe.CommandIsReadOnly(normalized)
		if !readOnly || bashReadOnlyCommandWrites(base, sub, fields) {
			return true
		}
	}
	return false
}

func bashCommandIsVerification(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok || len(segments) == 0 {
		return false
	}
	found := false
	for _, segment := range segments {
		normalized, safeRedirects := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
		if !safeRedirects {
			return false
		}
		fields, malformed := shellparse.StaticFields(normalized)
		if malformed != "" || len(fields) == 0 {
			return false
		}
		if bashSegmentIsVerification(fields) {
			found = true
			continue
		}
		if _, _, readOnly := shellsafe.CommandIsReadOnly(normalized); !readOnly {
			return false
		}
	}
	return found
}

// IsDeliveryVerificationCommand reports whether command is a host-recognized
// verification command for delivery finalization. Keep complete_step and the
// final-readiness gate on this single classifier so a sign-off cannot claim a
// command that the final gate will immediately reject.
func IsDeliveryVerificationCommand(command string) bool {
	return bashCommandIsVerification(command)
}

func bashSegmentIsVerification(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	base := strings.ToLower(filepath.Base(fields[0]))
	args := fields[1:]
	if hasCommandArg(args, "--fix", "--write", "-w", "--update", "-u") {
		return false
	}
	if hasWriteOutputFlag(args) {
		return false
	}
	switch base {
	case "go":
		if len(args) == 0 {
			return false
		}
		if args[0] == "vet" {
			return true
		}
		if args[0] == "test" {
			for _, arg := range args[1:] {
				if goTestFlagWritesFile(arg) {
					return false
				}
			}
			return true
		}
		return args[0] == "build" && !hasCommandArg(args, "-o")
	case "git":
		return len(args) > 1 && args[0] == "diff" && hasCommandArg(args[1:], "--check")
	case "pytest", "py.test", "gotestsum", "staticcheck", "golangci-lint", "tsc":
		return true
	case "mypy":
		for _, arg := range args {
			if mypyFlagWritesReport(arg) {
				return false
			}
		}
		return true
	case "npm", "pnpm", "yarn", "bun", "cargo":
		if len(args) > 0 && hasCommandArg(args[:1], "test", "check", "lint", "clippy") {
			return true
		}
		return len(args) > 1 && args[0] == "run" && hasCommandArg(args[1:2], "test", "check", "lint", "typecheck")
	case "node":
		return nodeSegmentIsVerification(args)
	case "make", "just":
		return len(args) > 0 && hasCommandArg(args[:1], "test", "check", "lint", "verify", "ci")
	case "python", "python3":
		return len(args) > 1 && args[0] == "-m" && hasCommandArg(args[1:2], "pytest", "unittest", "compileall")
	case "dotnet":
		return len(args) > 0 && args[0] == "test"
	case "mvn", "mvnw", "gradle", "gradlew":
		return len(args) > 0 && hasCommandArg(args, "test", "check", "verify")
	}
	return false
}

func nodeSegmentIsVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	// Node CLI flags are case-sensitive: -c/--check is the syntax-only mode,
	// while -C/--conditions executes the target with custom export conditions.
	switch args[0] {
	case "--check", "-c":
		// Syntax-check mode does not execute the target. Fail closed on any
		// additional option: preload/eval/import flags could execute code before
		// the check and turn a purported verifier into an opaque mutation.
		for _, arg := range args[1:] {
			if arg != "-" && strings.HasPrefix(arg, "-") {
				return false
			}
		}
		return true
	case "--test":
		// Match the repository's treatment of other conventional test runners,
		// but fail closed on test-runner and Node runtime flags that write files.
		for _, arg := range args[1:] {
			if nodeTestFlagWritesFile(arg) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func nodeTestFlagWritesFile(arg string) bool {
	name := strings.ToLower(arg)
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "--cpu-prof", "--heap-prof", "--heapsnapshot-near-heap-limit", "--heapsnapshot-signal",
		"--localstorage-file", "--perf-basic-prof", "--perf-basic-prof-only-functions", "--perf-prof",
		"--prof", "--redirect-warnings", "--report-on-fatalerror", "--report-on-signal",
		"--report-uncaught-exception", "--test-reporter-destination", "--test-rerun-failures",
		"--test-update-snapshots", "--tls-keylog", "--trace-events-enabled":
		return true
	default:
		return false
	}
}

func bashReadOnlyCommandWrites(base, sub string, fields []string) bool {
	args := fields[1:]
	if sub != "" && len(args) > 0 {
		args = args[1:]
	}
	switch base {
	case "find":
		return hasCommandArg(args, "-exec", "-execdir", "-delete", "-ok", "-okdir", "-fls", "-fprint", "-fprint0", "-fprintf")
	case "sort":
		for _, arg := range args {
			if arg == "-o" || arg == "--output" || strings.HasPrefix(arg, "--output=") || strings.HasPrefix(arg, "-o") {
				return true
			}
		}
	case "git":
		if sub == "diff" || sub == "show" || sub == "log" {
			for _, arg := range args {
				if arg == "--output" || strings.HasPrefix(arg, "--output=") {
					return true
				}
			}
		}
	case "go":
		return sub == "env" && hasCommandArg(args, "-w", "-u")
	}
	return false
}

func hasCommandArg(args []string, candidates ...string) bool {
	for _, arg := range args {
		for _, candidate := range candidates {
			if strings.EqualFold(arg, candidate) {
				return true
			}
		}
	}
	return false
}

// writeOutputFlags are test-runner and linter flags that write snapshot,
// report, or profile files. Snapshot flags rewrite checked-in fixtures (the
// --update/-u class rejected above); the others write explicit output paths.
// A runner invoked with one of them changes workspace state, so the segment
// must not count as read-only verification.
var writeOutputFlags = map[string]bool{
	"snapshot-update": true, // pytest-snapshot / syrupy
	"updatesnapshot":  true, // jest --updateSnapshot via npm/yarn wrappers
	"junitxml":        true, // pytest
	"junit-xml":       true, // pytest / mypy
	"junitfile":       true, // gotestsum
	"jsonfile":        true, // gotestsum
	"coverprofile":    true, // go test
	"cpuprofile":      true, // go test
	"memprofile":      true, // go test
	"blockprofile":    true, // go test
	"mutexprofile":    true, // go test
	"testlogfile":     true, // go test binary
	"gocoverdir":      true, // go test binary
	"outputfile":      true, // jest/vitest --outputFile (with --json)
	"report-log":      true, // pytest-reportlog
}

func hasWriteOutputFlag(args []string) bool {
	for _, arg := range args {
		name := strings.TrimLeft(arg, "-")
		if len(name) == len(arg) || name == "" {
			continue // not a flag
		}
		if i := strings.IndexByte(name, '='); i >= 0 {
			name = name[:i]
		}
		// go test flags accept an optional test. prefix (-test.coverprofile)
		// that the go tool passes through to the test binary.
		name = strings.TrimPrefix(strings.ToLower(name), "test.")
		if writeOutputFlags[name] {
			return true
		}
		// Vitest exposes dotted per-reporter forms (--outputFile.json=path).
		if i := strings.IndexByte(name, '.'); i > 0 && writeOutputFlags[name[:i]] {
			return true
		}
	}
	return false
}

// mypyFlagWritesReport reports whether a mypy flag writes a report directory:
// every mypy report option follows the --<type>-report DIR shape (txt, html,
// xml, cobertura-xml, any-exprs, linecount, linecoverage, lineprecision), and
// mypy has no read-only flag with that suffix. --junit-xml is covered by the
// global write-output flags.
func mypyFlagWritesReport(arg string) bool {
	name := strings.ToLower(arg)
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	return strings.HasPrefix(name, "--") && strings.HasSuffix(name, "-report")
}

// goTestFlagWritesFile reports whether a go test flag writes a workspace
// artifact: -c/-o emit the test binary, -trace and the profile flags write
// profiles, and -artifacts/-testlogfile/-gocoverdir write test outputs. The
// short and ambiguous names stay out of writeOutputFlags because the
// dash-stripped global match would also hit node -c (a syntax-only check)
// and pytest --trace (a read-only debugger flag). go test flags accept
// single- and double-dash forms and an optional test. prefix that the go
// tool passes through to the test binary.
func goTestFlagWritesFile(arg string) bool {
	name := strings.ToLower(arg)
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	trimmed := strings.TrimLeft(name, "-")
	if len(trimmed) == len(name) || trimmed == "" {
		return false // not a flag
	}
	trimmed = strings.TrimPrefix(trimmed, "test.")
	switch trimmed {
	case "c", "o", "trace", "artifacts", "testlogfile", "gocoverdir",
		"coverprofile", "cpuprofile", "memprofile", "blockprofile", "mutexprofile":
		return true
	default:
		return false
	}
}

func completeStepVerificationCommands(args json.RawMessage) []string {
	var p struct {
		Evidence []struct {
			Kind    string `json:"kind"`
			Command string `json:"command"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil
	}
	var out []string
	for _, item := range p.Evidence {
		if item.Kind == "verification" && strings.TrimSpace(item.Command) != "" {
			out = append(out, strings.TrimSpace(item.Command))
		}
	}
	return out
}

// commandShowsContentForPath reports whether a bash command demonstrably
// printed the content of the (normalized, slash-lowered) claimed path: a
// content-printing program — cat/head/tail/diff/cmp or git diff/git show —
// whose statically parsed argv names the path exactly or by trailing path
// components. The receipt must contain exactly one simple statement; compound
// statements and pipelines are rejected because unrelated output can satisfy
// the aggregate OutputBytes receipt. Redirected, negated, background, or
// dynamically expanded commands and summary/quiet flags that suppress the
// patch body (--stat, --name-only, -q, …) are rejected too. Matching is
// per-argument and exact, so reading path.bak never satisfies path.
func commandShowsContentForPath(command, needle string) bool {
	file, err := shellparse.ParseBash(command)
	if err != nil || shellparse.HasHereDoc(file) || len(file.Stmts) != 1 {
		return false
	}
	return contentStatementShowsPath(file.Stmts[0], needle)
}

func contentStatementShowsPath(stmt *syntax.Stmt, needle string) bool {
	if stmt == nil || stmt.Negated || stmt.Background || stmt.Coprocess {
		return false
	}
	if len(stmt.Redirs) > 0 {
		// Any redirect can divert the content away from the transcript.
		return false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		// Pipelines transform or swallow content; AND/OR lists can contribute
		// unrelated bytes to the aggregate receipt. Neither proves file output.
		return false
	case *syntax.CallExpr:
		argv := make([]string, 0, len(cmd.Args))
		for _, w := range cmd.Args {
			f, ok := shellparse.StaticWord(w)
			if !ok {
				return false
			}
			argv = append(argv, f)
		}
		return contentArgvShowsPath(argv, needle)
	default:
		return false
	}
}

// contentSuppressingFlags turn a content command into a summary that never
// shows the patch body; their presence disqualifies the receipt as evidence.
var contentSuppressingFlags = map[string]bool{
	"-q": true, "--quiet": true, "-s": true, "--silent": true,
	"--brief": true, "--no-patch": true, "--name-only": true,
	"--name-status": true, "--numstat": true, "--shortstat": true,
	"--summary": true, "--check": true,
}

func contentArgvShowsPath(argv []string, needle string) bool {
	if len(argv) == 0 {
		return false
	}
	rest := argv[1:]
	gitShow := false
	switch strings.ToLower(filepath.Base(argv[0])) {
	case "cat", "head", "tail", "diff", "cmp":
	case "git":
		if len(rest) == 0 {
			return false
		}
		sub := strings.ToLower(rest[0])
		if sub != "diff" && sub != "show" {
			return false
		}
		gitShow = sub == "show"
		rest = rest[1:]
	default:
		return false
	}
	named := false
	for _, a := range rest {
		lower := strings.ToLower(a)
		if contentSuppressingFlags[lower] || strings.HasPrefix(lower, "--stat") || strings.HasPrefix(lower, "--dirstat") {
			return false
		}
		if gitShow {
			if argNamesGitRevisionPath(a, needle) {
				named = true
			}
		} else if argNamesPath(a, needle) {
			named = true
		}
	}
	return named
}

// argNamesGitRevisionPath accepts only git show's REV:path form. The ordinary
// `git show REV -- path` form can print commit metadata with no file body while
// still producing a non-empty aggregate receipt.
func argNamesGitRevisionPath(arg, needle string) bool {
	tok := strings.ToLower(filepath.ToSlash(normalizePath(arg)))
	if tok == "" || strings.HasPrefix(tok, "-") {
		return false
	}
	i := strings.Index(tok, ":")
	if i <= 0 || i == len(tok)-1 {
		return false
	}
	path := tok[i+1:]
	return path == needle || strings.HasSuffix(path, "/"+needle)
}

// argNamesPath reports whether one static argv token names the claimed path:
// exact after normalization, a trailing-components match of a fuller token,
// or the path part of a git REV:path spec.
func argNamesPath(arg, needle string) bool {
	tok := strings.ToLower(filepath.ToSlash(normalizePath(arg)))
	if tok == "" || strings.HasPrefix(tok, "-") {
		return false
	}
	if tok == needle || strings.HasSuffix(tok, "/"+needle) {
		return true
	}
	if i := strings.Index(tok, ":"); i >= 0 {
		rest := tok[i+1:]
		if rest == needle || strings.HasSuffix(rest, "/"+needle) {
			return true
		}
	}
	return false
}

func commandReviewsChanges(command string) bool {
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok {
		return false
	}
	for _, segment := range segments {
		normalized, safe := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
		if !safe {
			continue
		}
		fields, malformed := shellparse.StaticFields(normalized)
		if malformed != "" || len(fields) == 0 {
			continue
		}
		base := strings.ToLower(filepath.Base(fields[0]))
		if base == "diff" || base == "cmp" {
			return true
		}
		if base == "git" && len(fields) > 1 {
			sub := strings.ToLower(fields[1])
			if sub == "diff" || sub == "status" || sub == "show" {
				return true
			}
		}
	}
	return false
}

func commandMentionsPaths(command string, wanted map[string]bool) bool {
	normalized := strings.ToLower(strings.ReplaceAll(command, `\`, "/"))
	for path := range wanted {
		if strings.Contains(normalized, strings.ToLower(filepath.ToSlash(path))) {
			return true
		}
	}
	return false
}

func isReadReceipt(name string, readOnly bool) bool {
	switch name {
	case "todo_write", "complete_step":
		return false
	default:
		return isReaderTool(name) || readOnly
	}
}

func isWriterTool(name string) bool {
	switch name {
	case "write_file", "edit_file", "multi_edit", "move_file", "notebook_edit", "delete_range", "delete_symbol":
		return true
	default:
		return false
	}
}

func isReaderTool(name string) bool {
	switch name {
	case "read_file", "ls", "grep":
		return true
	default:
		return false
	}
}

func extractPaths(fields map[string]json.RawMessage) []string {
	var paths []string
	for _, key := range []string{"path", "file_path", "notebook_path", "source_path", "destination_path"} {
		if s := stringField(fields, key); s != "" {
			paths = append(paths, s)
		}
	}
	for _, key := range []string{"paths", "file_paths"} {
		paths = append(paths, stringSliceField(fields, key)...)
	}
	return paths
}

func stringField(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func completeStepIdentity(fields map[string]json.RawMessage) string {
	if n, ok := intField(fields, "step_index"); ok && n > 0 {
		return strconv.Itoa(n)
	}
	return stringField(fields, "step")
}

func intField(fields map[string]json.RawMessage, key string) (int, bool) {
	raw, ok := fields[key]
	if !ok {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	return n, true
}

func stringSliceField(fields map[string]json.RawMessage, key string) []string {
	raw, ok := fields[key]
	if !ok {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
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

func sameTodoIdentity(a, b TodoItem) bool {
	return sameStepText(a.Content, b.Content) || sameStepText(a.ActiveForm, b.ActiveForm)
}

func hasSuccessfulCompleteStepForTodo(receipts []Receipt, index int, current []TodoItem) bool {
	for _, r := range receipts {
		if !r.Success || r.ToolName != "complete_step" || strings.TrimSpace(r.Step) == "" {
			continue
		}
		if r.TodoStep != nil && r.TodoStep.Found {
			if index < 1 || index > len(current) {
				continue
			}
			if sameTodoMatch(current[index-1], *r.TodoStep) {
				return true
			}
			if !todoContentRelates(current[index-1], *r.TodoStep) {
				continue
			}
		}
		match := matchTodoStep(r.Step, current)
		if match.Found && match.Index == index {
			return true
		}
	}
	return false
}

func latestTodoStep(step string, receipts []Receipt) TodoStepMatch {
	for i := len(receipts) - 1; i >= 0; i-- {
		r := receipts[i]
		if !r.Success || r.ToolName != "todo_write" {
			continue
		}
		return matchTodoStep(step, r.Todos)
	}
	return TodoStepMatch{}
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

func matchTodoStep(step string, todos []TodoItem) TodoStepMatch {
	if n, ok := parseStepIndex(normalizeStepText(step)); ok && n >= 1 && n <= len(todos) {
		t := todos[n-1]
		return TodoStepMatch{Found: true, Index: n, Content: t.Content, Status: t.Status, ActiveForm: t.ActiveForm}
	}
	for i, t := range todos {
		if sameStepText(step, t.Content) || sameStepText(step, t.ActiveForm) {
			return TodoStepMatch{Found: true, Index: i + 1, Content: t.Content, Status: t.Status, ActiveForm: t.ActiveForm}
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
		return TodoStepMatch{Found: true, Index: found + 1, Content: t.Content, Status: t.Status, ActiveForm: t.ActiveForm}
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

func pathSet(paths []string) map[string]bool {
	out := map[string]bool{}
	for _, p := range paths {
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func normalizePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = normalizePath(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, `\`, `/`)
	p = filepath.Clean(filepath.FromSlash(p))
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}
