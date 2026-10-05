package evidence

// What the ledger can be asked: whether a receipt of some shape is in it, and
// whether one landed after a given point. Every answer reads recorded receipts
// only — nothing here infers, and nothing re-runs anything.

import (
	"path/filepath"
	"strings"
)

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

// HasCompletedReview reports whether a review completed with evidence that is
// fresh for the latest mutation. Structured review_report receipts are the
// strongest proof and also cover collected background reviews. Foreground
// review/task adapters remain compatible, but after a mutation their child
// receipts must show that the changed result was actually inspected.
func (l *Ledger) HasCompletedReview() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	receipts := append([]Receipt(nil), l.receipts...)
	l.mu.Unlock()

	mutation := -1
	for i, r := range receipts {
		if r.Success && r.Mutation {
			mutation = i
		}
	}
	start := mutation + 1
	requiredPaths := []string(nil)
	if mutation >= 0 {
		requiredPaths = receipts[mutation].Paths
	}

	for i := start; i < len(receipts); i++ {
		r := receipts[i]
		if completedStructuredReviewReceipt(r, requiredPaths) {
			return true
		}
		if !successfulForegroundReviewReceipt(r) {
			continue
		}
		if mutation < 0 || receiptsReviewChanges(receipts, start, i, mutation) {
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
	start := max(after+1, 0)

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

// HasCorroboratedCitedCheckAfter reports whether a completion after `after`
// named a verification command that also ran successfully after `after`.
//
// complete_step already refuses a citation with no matching successful receipt,
// so the model's part is naming which command was the check — the thing a
// static table cannot know about a project's own script, and the model does
// know, having read or written it. The host still supplies both halves that
// matter: that the command ran, and that it passed.
func (l *Ledger) HasCorroboratedCitedCheckAfter(after int) bool {
	if l == nil {
		return false
	}
	for _, command := range l.citedChecksAfter(after) {
		if l.HasSuccessfulCommandAfter(command, after) {
			return true
		}
	}
	return false
}

func (l *Ledger) HasSuccessfulCompleteStepAfter(after int) bool {
	if l == nil {
		return false
	}
	start := max(after+1, 0)

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
	return l.deliverySignoffAfter(after, true)
}

// HasCitedVerificationAfter is the same minus the review requirement. They
// differ only when the review is what is missing — the one case where asking
// for a cited verification is false, since it has one.
func (l *Ledger) HasCitedVerificationAfter(after int) bool {
	return l.deliverySignoffAfter(after, false)
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
	start := max(after+1, 0)

	l.mu.Lock()
	receipts := append([]Receipt(nil), l.receipts...)
	l.mu.Unlock()
	if after >= len(receipts) {
		return false
	}
	return receiptsReviewChanges(receipts, start, len(receipts), after)
}

// HasHostReviewCoverageAfter reports whether host-observed content inspection
// after the latest mutation covers the production paths required by a Medium
// Delivery review. Every required path needs a read receipt naming it, or a
// call whose model-visible output carried the change itself (Showed). What the
// command looked like decides nothing: a `git diff` that printed the change and
// a `git diff --stat` that printed a count differ in their output, not in their
// shape, and the shape is what a compound statement hides.
func (l *Ledger) HasHostReviewCoverageAfter(after int, requiredPaths []string) bool {
	if l == nil {
		return false
	}
	start := max(after+1, 0)
	l.mu.Lock()
	receipts := append([]Receipt(nil), l.receipts...)
	l.mu.Unlock()
	if after >= len(receipts) {
		return false
	}
	wanted := normalizePaths(requiredPaths)
	if len(wanted) == 0 {
		return false
	}
	for _, path := range wanted {
		needle := strings.ToLower(filepath.ToSlash(path))
		covered := false
		for i := start; i < len(receipts) && !covered; i++ {
			r := receipts[i]
			if !r.Success {
				continue
			}
			if r.Read && pathsAnswerFor(r.Paths, needle) {
				covered = true
			}
			if !covered && pathsAnswerFor(r.Showed, needle) {
				covered = true
			}
		}
		if !covered {
			return false
		}
	}
	return true
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

// HasSuccessfulToolReceipt reports whether a named tool completed
// successfully in the current evidence scope.
func (l *Ledger) HasSuccessfulToolReceipt(name string) bool {
	name = strings.TrimSpace(name)
	if l == nil || name == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if r.Success && r.ToolName == name {
			return true
		}
	}
	return false
}

// HasSuccessfulMutationOtherThan distinguishes a workflow-specific state
// change (for example durable memory) from unrelated workspace mutations that
// still need the full Delivery verification/review contract.
func (l *Ledger) HasSuccessfulMutationOtherThan(allowed ...string) bool {
	if l == nil {
		return false
	}
	allow := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allow[strings.TrimSpace(name)] = true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if r.Success && r.Mutation && !allow[r.ToolName] {
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
	return l.HasSuccessfulVerificationCommandAfter(-1)
}

// HasSuccessfulVerificationCommandAfter reports whether verification succeeded
// after the named receipt index. Mutations before the boundary do not satisfy a
// role setting's post-change verification floor.
func (l *Ledger) HasSuccessfulVerificationCommandAfter(after int) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts[max(after+1, 0):] {
		if ReceiptRunsVerification(r) && verificationPassed(r) {
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

// HasSuccessfulAnchorRefreshReadAfter reports whether read_file refreshed a
// wanted path after the given receipt index. Windowed reads and grep/ls receipts
// are deliberately not enough for same-turn anchor edits: they may have observed
// a different region than the next old_string/delete_range anchor.
func (l *Ledger) HasSuccessfulAnchorRefreshReadAfter(paths []string, after int) bool {
	wanted := pathSet(normalizePaths(paths))
	if l == nil || len(wanted) == 0 {
		return false
	}
	start := max(after+1, 0)

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
	start := max(baseline+1, 0)
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
