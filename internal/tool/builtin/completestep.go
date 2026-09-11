package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/planmode"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(completeStep{}) }

// completeStep records an evidence-backed completion of one step of an approved
// plan. Like todo_write it has no host side effects — the claim and its evidence
// live in the call's args, which a frontend renders as a signed-off step. Its
// reason for existing is the enforcement in Execute: a completion with no evidence
// is rejected, so the model can't flip a step to "done" without showing why it is
// done (the verification it ran, the diff/files it changed, or a manual check).
// It complements todo_write — todo_write keeps the list moving (one item
// in_progress), complete_step is the formal sign-off of a finished step.
type completeStep struct{}

type stepEvidence struct {
	Kind        string   `json:"kind"`
	Summary     string   `json:"summary"`
	Command     string   `json:"command,omitempty"`
	Paths       []string `json:"paths,omitempty"`
	CriterionID string   `json:"criterion_id,omitempty"`
}

// validEvidenceKinds are the evidence forms a completion may cite. "checkpoint"
// (main's fourth kind) is omitted — v2 has no checkpoint system.
var validEvidenceKinds = map[string]bool{
	"verification": true, // a command/test was run; cite it and its outcome
	"review":       true, // a completed built-in review run, fresh for any later mutation
	"diff":         true, // a concrete code change; cite what changed
	"files":        true, // files created/edited/inspected; cite the paths
	"manual":       true, // a manual check; cite what was confirmed and how
}

func (completeStep) Name() string { return "complete_step" }

func (completeStep) Description() string {
	return "Record the completion of ONE step of an approved plan. Call it as you finish each step so the task list advances and the user sees what changed. Cite proof by RECEIPT ID: every tool result ends with the host's own id (`[receipt r_1a2b3c4d]`), and listing those ids in `receipt_ids` is exact — retyping a command instead makes the host match your text, which fails over a `cd` prefix, quoting, or argument order. For ordinary work the host has already recorded what your tools did, so anything it cannot confirm is reported alongside the sign-off rather than rejected; under a delivery floor the proof is still required. Fields: `step_id` or `step` (which task-list item), `result` (what is now true/changed), `receipt_ids` (preferred proof), `evidence` (optional items, each with `kind` = verification|review|diff|files|manual and a `summary`, plus optional `command`/`paths`, and `criterion_id` naming the acceptance criterion the proof satisfies), and optional `notes`."
}

func (completeStep) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "step_id":{"type":"string","description":"PREFERRED: the stable step_id of the task-list item this completes, e.g. \"plan_step_02\". Unlike a title or a number it survives retitles, insertions, and reordering, so cite it whenever the item has one."},
  "step":{"type":"string","description":"Which plan step this completes — its title or number, matching the task list. Use only when the item has no step_id."},
  "step_index":{"type":"integer","minimum":1,"description":"Optional 1-based task-list item number. Use only when the item has no step_id; an index goes stale the moment a step is inserted above it."},
  "result":{"type":"string","description":"What is now true or changed as a result of finishing this step."},
  "evidence":{
    "type":"array",
    "description":"Proof the step is done. Optional for ordinary work — the host already recorded what your tools did; required when the run is under a delivery floor.",
    "items":{
      "type":"object",
      "properties":{
        "criterion_id":{"type":"string","description":"The acceptance criterion this proof satisfies, as the plan renders it (e.g. \"c2\" from \"accept [c2]: ...\"). Cite it whenever the step has criteria: a command succeeding is not the same as a criterion being met, and the host records the proof against the criterion you name."},
        "kind":{"type":"string","enum":["verification","review","diff","files","manual"],"description":"verification = a command/test was run (command REQUIRED); review = a built-in review run completed and, after changes, inspected the latest changed result (the verdict/findings still apply separately); diff = a concrete code change (paths REQUIRED); files = files created/edited/inspected (paths REQUIRED); manual = a manual check."},
        "summary":{"type":"string","description":"The evidence itself: the test result, what the diff does, or what was confirmed."},
        "command":{"type":"string","description":"REQUIRED for verification evidence: the command as it actually ran (e.g. \"go test ./...\") — it is checked against this session's real command history."},
        "paths":{"type":"array","items":{"type":"string"},"description":"REQUIRED for diff/files evidence: the files this evidence refers to, as the paths were passed to the tools that touched them."}
      },
      "required":["kind","summary"]
    }
  },
  "receipt_ids":{"type":"array","items":{"type":"string"},"description":"PREFERRED proof: the host receipt ids printed after the tool calls that did the work (e.g. \"r_1a2b3c4d\"). Citing an id is exact — the host issued it — so shell prefixes, quoting, argument order, and working directory never matter. Use these instead of retyping a command."},
  "operation_id":{"type":"string","description":"The host operation whose receipts are being cited. Required when citing runtime-issued receipts so evidence from another change cannot satisfy this step."},
  "notes":{"type":"string","description":"Optional caveats, follow-ups, or anything deferred."}
},
"required":["result"]
}`)
}

// ReadOnly is true: complete_step only records a claim (no filesystem or process
// effect), so it never needs approval and stays available alongside todo_write.
func (completeStep) ReadOnly() bool { return true }

// complete_step signs off execution work and is unavailable during planning.
// The host Plan gate remains authoritative for stale or hallucinated calls.
func (completeStep) ProviderVisible(ctx context.Context) bool {
	return !planmode.Active(ctx)
}

// PlanModeSafe reports false: although complete_step is read-only, it signs off a
// completed execution step, which is meaningful only after plan approval — not
// during planning. This explicit phase opt-out is the Plan gate's enforced
// exception to the ordinary Permissions/Sandbox path.
func (completeStep) PlanModeSafe() bool { return false }

func (completeStep) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		StepID      string         `json:"step_id"`
		Step        string         `json:"step"`
		StepIndex   int            `json:"step_index"`
		Result      string         `json:"result"`
		Evidence    []stepEvidence `json:"evidence"`
		ReceiptIDs  []string       `json:"receipt_ids"`
		OperationID string         `json:"operation_id"`
		Notes       string         `json:"notes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	cited, err := resolveCitedReceipts(ctx, p.ReceiptIDs)
	if err != nil {
		return "", err
	}
	if err := validateCitedReceiptsForOperation(ctx, cited, p.OperationID); err != nil {
		return "", err
	}
	step := completeStepIdentity(p.StepID, p.Step, p.StepIndex)
	if step == "" {
		return "", fmt.Errorf("step_id, step, or step_index is required — cite the task-list item you are completing, preferring its stable step_id")
	}
	if p.StepIndex < 0 {
		return "", fmt.Errorf("step_index must be a positive 1-based task-list number")
	}
	if strings.TrimSpace(p.Result) == "" {
		return "", fmt.Errorf("result is required — state what is now true after finishing this step")
	}
	// Ordinary work is settled by the real tool results the host already
	// recorded, so a sign-off with nothing to add is a note, not a violation.
	// Only the closed loop still demands proof at this boundary.
	strict := evidence.ClosedLoopExecutionFromContext(ctx)
	if len(p.Evidence) == 0 && len(cited) == 0 && strict {
		return "", fmt.Errorf("at least one evidence item is required — don't mark a step complete without showing why it's done (run a check, cite the diff, or confirm manually)")
	}
	kinds := make([]string, 0, len(p.Evidence))
	for i, e := range p.Evidence {
		if !validEvidenceKinds[e.Kind] {
			return "", fmt.Errorf("evidence %d: invalid kind %q (want verification|diff|files|manual)", i+1, e.Kind)
		}
		if strings.TrimSpace(e.Summary) == "" {
			return "", fmt.Errorf("evidence %d: summary is required — the evidence is the summary, not just its kind", i+1)
		}
		kinds = append(kinds, e.Kind)
	}
	if err := verifyCitedCriteria(ctx, p.Evidence); err != nil {
		return "", err
	}

	var gaps []string
	todoMatch, hasTodo, err := verifyTodoStep(ctx, step)
	if err != nil {
		if strict {
			return "", err
		}
		gaps, hasTodo = append(gaps, err.Error()), false
	}
	tally, err := verifyStepEvidence(ctx, p.Evidence, cited, p.OperationID)
	if err != nil {
		if strict {
			if hasTodo && todoMatch.Status == "in_progress" {
				return "", fmt.Errorf("%w; todo %d %q remains in_progress — repair the evidence and retry this step before moving on", err, todoMatch.Index, todoMatch.Content)
			}
			return "", err
		}
		gaps = append(gaps, err.Error())
	}
	gaps = append(gaps, unclassifiedCommandGaps(tally.unclassified)...)
	projectVerified, err := verifyProjectChecks(ctx, p.Evidence)
	if err != nil {
		if strict {
			return "", err
		}
		gaps = append(gaps, err.Error())
	}
	hostStatus := ""
	if _, ok := evidence.FromContext(ctx); ok {
		hostStatus = fmt.Sprintf(" Host evidence: host-verified %d, manual/unverified %d.", tally.hostVerified, tally.manualUnverified)
	}
	todoStatus := ""
	if hasTodo {
		todoStatus = fmt.Sprintf(" Todo step: todo-matched %d (%q).", todoMatch.Index, todoMatch.Content)
	}
	projectStatus := ""
	if projectVerified > 0 {
		projectStatus = fmt.Sprintf(" Project checks: project checks %d.", projectVerified)
	}
	advanceStatus := " The host advanced the task list; continue with the next step."
	if hasTodo {
		switch {
		case todoMatch.Status == "completed":
			// Renewal sign-off of an already-completed item: nothing advances.
			advanceStatus = " The matched todo was already completed; the task list is unchanged."
		case remainingTodoStepsAfterSignoff(ctx) <= 1:
			// The item being signed off is the only unfinished todo (serial
			// lists keep at most one in_progress plus pending successors), so
			// this sign-off completes the task list. Tell the model to wrap up
			// instead of waiting for a next step, which otherwise loops the
			// orchestration forever (#8816).
			advanceStatus = " All steps completed — the task list has no remaining steps; deliver a final summary and end the turn."
		}
	}
	return fmt.Sprintf("Step %q signed off with %d evidence item(s) [%s].%s%s%s",
		step, len(p.Evidence), strings.Join(kinds, ", "), hostStatus+todoStatus+projectStatus, advanceStatus, unverifiedNote(gaps)), nil
}

// unclassifiedCommandGaps names successful commands the host does not
// recognize as standard verifiers, so the gap is visible exactly once.
func unclassifiedCommandGaps(commands []string) []string {
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		out = append(out, fmt.Sprintf("command %q succeeded but is not a recognized verifier", command))
	}
	return out
}

// unverifiedNote records what the host could not confirm without turning it
// into a rejection. The gap is stated once, for the user and the final gates
// to act on; asking the model to re-cite the same work is the loop this
// replaces.
func unverifiedNote(gaps []string) string {
	if len(gaps) == 0 {
		return ""
	}
	return " Recorded as unverified (the step still stands; the host did not confirm): " + strings.Join(gaps, "; ") + "."
}

// remainingTodoStepsAfterSignoff counts the todos that will still need work
// once the current in_progress item is signed off: every item that is not yet
// completed (the item being signed off plus any pending successors). The
// caller compares against 1 — only the item being signed off is unfinished —
// to detect that the sign-off completes the task list.
func remainingTodoStepsAfterSignoff(ctx context.Context) int {
	ledger, ok := evidence.FromContext(ctx)
	var todos []evidence.TodoItem
	if ok {
		todos, _ = ledger.LatestTodos()
	}
	if len(todos) == 0 {
		todos, _ = evidence.TodoStateFromContext(ctx)
	}
	remaining := 0
	for _, todo := range todos {
		if strings.TrimSpace(todo.Status) != "completed" {
			remaining++
		}
	}
	return remaining
}

// completeStepIdentity picks the citation to resolve against the task list,
// most stable first: an id survives a replan, an index survives a retitle, a
// title survives neither.
func completeStepIdentity(stepID, step string, stepIndex int) string {
	if id := strings.TrimSpace(stepID); id != "" {
		return id
	}
	if stepIndex > 0 {
		return strconv.Itoa(stepIndex)
	}
	return strings.TrimSpace(step)
}

// stepEvidenceTally is what the host could actually confirm about one
// completion. Unclassified is a successful command the host does not recognize
// as a standard verifier — a project script or a wrapper — which is reported,
// never rejected: rejecting it only made the model rewrite the same command.
type stepEvidenceTally struct {
	hostVerified     int
	manualUnverified int
	unclassified     []string
}

func verifyStepEvidence(ctx context.Context, items []stepEvidence, cited []evidence.ReceiptRef, operationID string) (tally stepEvidenceTally, err error) {
	ledger, ok := evidence.FromContext(ctx)
	if !ok {
		return tally, nil
	}
	for i, e := range items {
		switch e.Kind {
		case "verification":
			// A host-issued receipt is exact proof. Only a citation that names
			// no receipt falls back to matching the command text, which is what
			// rejected real verifications over a prefix or a quote style.
			if citedReceiptForOperation(ledger, cited, operationID, evidence.ReceiptKindVerification, evidence.ReceiptKindCommand, evidence.ReceiptKindReview) {
				tally.hostVerified++
				continue
			}
			command := strings.TrimSpace(e.Command)
			if command == "" {
				return tally, fmt.Errorf("evidence %d: verification command is required for host verification — cite the command you ran in this session, cite its receipt id in receipt_ids%s, or use kind \"files\", \"diff\", or \"manual\"", i+1, availableReceiptHint(ledger))
			}
			if !ledger.HasSuccessfulCommand(command) && !verifyCommandFromSession(ctx, command) {
				if ledger.HasFailedCommand(command) {
					return tally, fmt.Errorf("evidence %d: verification command %q ran but exited non-zero, so it can't prove the step; if the non-zero exit is itself the expected proof (e.g. a file is gone), re-run it so it succeeds (append \"|| true\") and sign off again", i+1, command)
				}
				return tally, missingVerificationReceipt(ctx, ledger, i+1, command)
			}
			_, closedLoopHasMutation := ledger.LatestSuccessfulMutationIndex()
			if closedLoopHasMutation && !evidence.IsVerificationCommand(command) {
				// It succeeded; the host just does not recognize it. Report
				// that once instead of sending the model back to invent a
				// command shape the host happens to know.
				tally.unclassified = append(tally.unclassified, command)
			}
			tally.hostVerified++
		case "review":
			if !citedReceiptForOperation(ledger, cited, operationID, evidence.ReceiptKindReview) && !ledger.HasCompletedReview() {
				return tally, fmt.Errorf("evidence %d: review evidence requires a completed review run in this turn; after a mutation, the review must be newer and cover the changed result", i+1)
			}
			tally.hostVerified++
		case "diff":
			if len(e.Paths) == 0 {
				return tally, fmt.Errorf("evidence %d: diff evidence requires paths for host verification — cite the files you changed", i+1)
			}
			if !citedCoversPaths(cited, evidence.ReceiptKindMutation, e.Paths) && !ledger.HasSuccessfulWrite(e.Paths) && !verifyPathsFromSession(ctx, e.Paths, true) {
				return tally, fmt.Errorf("evidence %d: diff paths have no matching successful writer receipt in this turn%s", i+1, receiptHint("files written this turn", ledger.TouchedPaths(8, true)))
			}
			tally.hostVerified++
		case "files":
			if len(e.Paths) == 0 {
				return tally, fmt.Errorf("evidence %d: files evidence requires paths for host verification — cite the files you touched", i+1)
			}
			if !citedCoversPaths(cited, "", e.Paths) && !ledger.HasSuccessfulReadOrWrite(e.Paths) && !ledger.HasSuccessfulBashMentioningPaths(e.Paths) && !verifyPathsFromSession(ctx, e.Paths, false) {
				return tally, fmt.Errorf("evidence %d: file paths have no matching successful read/write receipt in this turn%s", i+1, receiptHint("files touched this turn", ledger.TouchedPaths(8, false)))
			}
			tally.hostVerified++
		case "manual":
			tally.manualUnverified++
		}
	}
	return tally, nil
}

func verifyProjectChecks(ctx context.Context, items []stepEvidence) (int, error) {
	checks := instruction.FromContext(ctx)
	if len(checks) == 0 {
		return 0, nil
	}
	ledger, ok := evidence.FromContext(ctx)
	if !ok {
		return 0, nil
	}
	after, ok := latestWriteBackedEvidenceIndex(ledger, items)
	if !ok {
		return 0, nil
	}
	for _, check := range checks {
		command := strings.TrimSpace(check.Command)
		if command == "" {
			continue
		}
		if !ledger.HasSuccessfulCommandAfter(command, after) {
			return 0, fmt.Errorf("project check %q from %s has no matching successful bash receipt after the latest matching write in this turn", command, checkSource(check))
		}
	}
	return len(checks), nil
}

func latestWriteBackedEvidenceIndex(ledger *evidence.Ledger, items []stepEvidence) (int, bool) {
	latest := -1
	for _, item := range items {
		switch item.Kind {
		case "diff", "files":
			if i, ok := ledger.LatestSuccessfulWriteIndex(item.Paths); ok && i > latest {
				latest = i
			}
		}
	}
	return latest, latest >= 0
}

func checkSource(check instruction.VerifyCheck) string {
	source := strings.TrimSpace(check.SourcePath)
	if source == "" {
		source = "project memory"
	}
	if check.Line > 0 {
		return fmt.Sprintf("%s:%d", source, check.Line)
	}
	return source
}

func verifyTodoStep(ctx context.Context, step string) (evidence.TodoStepMatch, bool, error) {
	ledger, ok := evidence.FromContext(ctx)
	var todos []evidence.TodoItem
	if ok {
		todos, _ = ledger.LatestTodos()
	}
	if len(todos) == 0 {
		todos, _ = evidence.TodoStateFromContext(ctx)
	}
	if len(todos) == 0 {
		return evidence.TodoStepMatch{}, false, nil
	}
	match, found := evidence.MatchStep(step, todos)
	if !found {
		allCompleted := true
		for _, todo := range todos {
			if strings.TrimSpace(todo.Status) != "completed" {
				allCompleted = false
				break
			}
		}
		if allCompleted {
			last := len(todos) - 1
			return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q has no matching todo_write item and every current todo is already completed; this is a renewal sign-off, so retry complete_step with step_index %d (the final existing todo %q) and the fresh evidence — do not invent a new step or rewrite the completed list", step, last+1, todos[last].Content)
		}
		if ids := evidence.TodoStepIDs(todos); len(ids) > 0 {
			return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q has no matching todo_write item in the current task list; cite the item's stable step_id — available ids: %s (list: %s)", step, strings.Join(ids, ", "), todoListInventory(todos))
		}
		return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q has no matching todo_write item in the current task list; cite a todo verbatim or by number: %s", step, todoListInventory(todos))
	}
	switch match.Status {
	case "in_progress":
		if unfinished, ok := evidence.FirstUnfinishedSubStep(todos, match.Index-1); ok && unfinished >= 0 {
			return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q matches phase %d %q whose sub-steps are unfinished; complete sub-step %d %q first, then sign the phase off", step, match.Index, match.Content, unfinished+1, todos[unfinished].Content)
		}
		return match, true, nil
	case "completed":
		return match, true, nil
	case "", "pending":
		current := ""
		for i, todo := range todos {
			if strings.TrimSpace(todo.Status) != "in_progress" {
				continue
			}
			// The deepest in_progress item is the signable end of the current
			// chain: prefer an active sub-step over its phase header.
			current = fmt.Sprintf("; finish todo %d %q first", i+1, todo.Content)
			if todo.Level == 1 {
				break
			}
		}
		return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q matches pending todo %d %q; complete_step only signs the current in_progress item%s", step, match.Index, match.Content, current)
	default:
		return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q matches todo %d (%q) but its status is %q; complete_step requires in_progress or completed", step, match.Index, match.Content, match.Status)
	}
}

func todoInventory(ledger *evidence.Ledger) string {
	todos, ok := ledger.LatestTodos()
	if !ok || len(todos) == 0 {
		return "(no todos recorded this turn)"
	}
	return todoListInventory(todos)
}

func todoListInventory(todos []evidence.TodoItem) string {
	parts := make([]string, 0, len(todos))
	for i, t := range todos {
		content := t.Content
		if r := []rune(content); len(r) > 60 {
			content = string(r[:60]) + "…"
		}
		parts = append(parts, fmt.Sprintf("%d) %q", i+1, content))
		if len(parts) == 12 && len(todos) > 12 {
			parts = append(parts, fmt.Sprintf("… %d more", len(todos)-12))
			break
		}
	}
	return strings.Join(parts, ", ")
}

// verifyCommandFromSession scans the full conversation history (not just the
// per-turn ledger) so a complete_step can cite a command that ran in an
// earlier turn (the ledger resets per turn) or via a named tool instead of
// bash. Calls whose recorded result is an error or a block are skipped — they
// prove the command was attempted, not that it succeeded.
func verifyCommandFromSession(ctx context.Context, command string) bool {
	msgs, ok := evidence.SessionMessagesFromContext(ctx)
	if !ok {
		return false
	}
	lookup := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(command), "..."), "…")
	if lookup == "" {
		return false
	}
	toolName := firstWord(lookup)
	failed := failedCallIDs(msgs)

	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			if failed[tc.ID] {
				continue
			}
			cmd := extractCommandFromCall(tc.Name, tc.Arguments)
			if cmd == "" {
				continue
			}
			if evidence.CommandMatches(lookup, cmd) {
				return true
			}
			if toolName != "" && toolName != "bash" && tc.Name == toolName {
				return true
			}
		}
	}
	return false
}

// verifyPathsFromSession is the diff/files analogue of verifyCommandFromSession:
// it lets a completion cite a file written or read in an earlier turn (the
// per-turn ledger only has this turn). wantWrite restricts to writer tools.
func verifyPathsFromSession(ctx context.Context, paths []string, wantWrite bool) bool {
	msgs, ok := evidence.SessionMessagesFromContext(ctx)
	if !ok {
		return false
	}
	return evidence.PathsProvenInSession(msgs, paths, wantWrite)
}

func failedCallIDs(msgs []provider.Message) map[string]bool {
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

func receiptHint(label string, items []string) string {
	if len(items) == 0 {
		return ""
	}
	for i, item := range items {
		if len(item) > 80 {
			items[i] = item[:80] + "…"
		}
	}
	return fmt.Sprintf("; %s: %q — cite one as it actually ran, or run the check now", label, items)
}

// allCommandHints builds a combined hint from both the per-turn ledger and the
// full session history, so the model can self-correct a mismatched citation.
func allCommandHints(ctx context.Context, ledger *evidence.Ledger) string {
	seen := map[string]bool{}
	var cmds []string
	if ledger != nil {
		for _, c := range ledger.SuccessfulCommands(8) {
			if !seen[c] {
				seen[c] = true
				cmds = append(cmds, c)
			}
		}
	}
	if msgs, ok := evidence.SessionMessagesFromContext(ctx); ok {
		failed := failedCallIDs(msgs)
		for _, msg := range msgs {
			for _, tc := range msg.ToolCalls {
				if failed[tc.ID] {
					continue
				}
				if tc.Name == "todo_write" || tc.Name == "complete_step" {
					continue
				}
				c := extractCommandFromCall(tc.Name, tc.Arguments)
				if c == "" || seen[c] {
					continue
				}
				seen[c] = true
				cmds = append(cmds, c)
				if len(cmds) >= 12 {
					break
				}
			}
			if len(cmds) >= 12 {
				break
			}
		}
	}
	if len(cmds) == 0 {
		return ""
	}
	// Truncate long entries for readability.
	for i, c := range cmds {
		if len(c) > 80 {
			cmds[i] = c[:80] + "…"
		}
	}
	return fmt.Sprintf("; commands that ran: %q — pick the matching one and retry complete_step", cmds)
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, " \t\n"); idx >= 0 {
		return s[:idx]
	}
	return s
}

// extractCommandFromCall extracts the bash "command" argument from a tool call
// args JSON, or returns the tool name + path for non-bash tools.
func extractCommandFromCall(name string, argsJSON string) string {
	if name == "bash" {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return ""
		}
		return strings.TrimSpace(args.Command)
	}
	// For non-bash tools, return "name path" so the command "ls ." can match
	// against a tool call `ls` with path `.`.
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.Path == "" {
		return name
	}
	return name + " " + args.Path
}

// verifyCitedCriteria rejects a proof citing a criterion the approved plan does
// not have. Resolving an unknown id into nothing would leave the real criterion
// unproven and only surface much later, as a completion the host refuses for a
// reason the model never connected to this call.
func verifyCitedCriteria(ctx context.Context, items []stepEvidence) error {
	known, ok := evidence.AcceptanceCriteriaFromContext(ctx)
	if !ok {
		return nil
	}
	for i, item := range items {
		id := strings.TrimSpace(item.CriterionID)
		if id == "" || slices.Contains(known, id) {
			continue
		}
		return fmt.Errorf("evidence %d: criterion_id %q is not in the approved plan; cite one of: %s", i+1, id, strings.Join(known, ", "))
	}
	return nil
}
