package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/instruction"
)

func init() { tool.RegisterBuiltin(completeStep{}) }

// completeStep records an evidence-backed completion of one step of an approved
// plan. It has no host side effects; what it exists for is the enforcement in
// Execute, which rejects a completion carrying no evidence — so a step cannot
// flip to "done" without showing why. todo_write keeps the list moving;
// complete_step is the formal sign-off of a finished step.
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
	return "Record the evidence-backed completion of ONE step of an approved plan. Call it as you finish each step instead of silently moving on: a completion with no evidence is REJECTED, so don't claim a step is done until you can show why. The host advances the list for you — it marks this step completed and moves the next to in_progress, so a todo_write that marks such an item completed on its own is REJECTED too. Sign off one step per tool-call ROUND, not one per call: the sign-off promotes the next item, and a second sign-off sent beside it signs against the list as it stood before."
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
    "minItems":1,
    "description":"Proof the step is done. At least one item is required.",
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
  "notes":{"type":"string","description":"Optional caveats, follow-ups, or anything deferred."}
},
"required":["result","evidence"]
}`)
}

// ReadOnly is true: complete_step only records a claim (no filesystem or process
// effect), so it never needs approval and stays available alongside todo_write.
func (completeStep) ReadOnly() bool { return true }

// Sequential is true even though ReadOnly is: signing a step off advances the
// task list, so two sign-offs in one reply have to land in the order sent.
func (completeStep) Sequential(context.Context, json.RawMessage) bool { return true }

// complete_step signs off execution work and is unavailable during planning.
// The host Plan gate remains authoritative for stale or hallucinated calls.
func (completeStep) ProviderVisible(ctx context.Context) bool {
	return !planmode.Active(ctx)
}

// Plan mode being active is what ProviderVisible reads, so that is what the
// code names — not "unapproved", which is a conclusion about a plan this tool
// never sees.
var planModeActive = tool.Refusal{
	Code:    "plan.mode_active",
	Message: "blocked: complete_step is only available after plan approval. While planning, keep task state with todo_write and present the plan for user approval.",
}

func (completeStep) Unavailable(context.Context) tool.Refusal { return planModeActive }

// PlanModeSafe reports false: although complete_step is read-only, it signs off a
// completed execution step, which is meaningful only after plan approval — not
// during planning. This explicit phase opt-out is the Plan gate's enforced
// exception to the ordinary Permissions/Sandbox path.
func (completeStep) PlanModeSafe() bool { return false }

func (completeStep) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		StepID    string         `json:"step_id"`
		Step      string         `json:"step"`
		StepIndex int            `json:"step_index"`
		Result    string         `json:"result"`
		Evidence  []stepEvidence `json:"evidence"`
		Notes     string         `json:"notes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
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
	if len(p.Evidence) == 0 {
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

	todoMatch, hasTodo, err := verifyTodoStep(ctx, step)
	if err != nil {
		return "", err
	}
	hostVerified, manualUnverified, err := verifyStepEvidence(ctx, p.Evidence)
	if err != nil {
		if hasTodo && todoMatch.Status == "in_progress" {
			return "", fmt.Errorf("%w; todo %d %q remains in_progress — repair the evidence and retry this step before moving on", err, todoMatch.Index, todoMatch.Content)
		}
		return "", err
	}
	projectVerified, err := verifyProjectChecks(ctx, p.Evidence)
	if err != nil {
		return "", err
	}
	hostStatus := ""
	if _, ok := evidence.FromContext(ctx); ok {
		hostStatus = fmt.Sprintf(" Host evidence: host-verified %d, manual/unverified %d.", hostVerified, manualUnverified)
	}
	todoStatus := ""
	if hasTodo {
		todoStatus = fmt.Sprintf(" Todo step: todo-matched %d (%q).", todoMatch.Index, todoMatch.Content)
	}
	projectStatus := ""
	if projectVerified > 0 {
		projectStatus = fmt.Sprintf(" Project checks: project checks %d.", projectVerified)
	}
	advanceStatus := planAdvanceStatus(ctx, todoMatch, hasTodo)
	return fmt.Sprintf("Step %q signed off with %d evidence item(s) [%s].%s%s",
		step, len(p.Evidence), strings.Join(kinds, ", "), hostStatus+todoStatus+projectStatus, advanceStatus), nil
}

// planAdvanceStatus says what the sign-off left behind. Whether a step remains
// is a fact the task list holds, and telling the model to continue past the
// last one is what keeps a finished plan executing (#8816).
func planAdvanceStatus(ctx context.Context, match evidence.TodoStepMatch, hasTodo bool) string {
	if !hasTodo {
		return " The host recorded the sign-off; there is no task list to advance."
	}
	if terminal, next, ok := planAdvanceAfter(ctx, match); ok {
		switch {
		case terminal:
			return " Every step in the task list is now complete. Do not open new work under this plan."
		case next.Content != "":
			return fmt.Sprintf(" The host advanced the task list; the next step is %d %q.", next.Index, next.Content)
		}
	}
	if match.Status == "completed" {
		return " The matched todo was already completed; the task list is unchanged."
	}
	return " The host advanced the task list; continue with the next step."
}

// planAdvanceAfter replays the host's own advance on a copy of the list, so
// what this result says and what the task list does cannot drift apart: the
// alternative is a second implementation of the same state machine.
func planAdvanceAfter(ctx context.Context, match evidence.TodoStepMatch) (terminal bool, next evidence.TodoStepMatch, ok bool) {
	todos := currentTodos(ctx)
	if len(todos) == 0 || match.Index <= 0 || match.Index > len(todos) {
		return false, evidence.TodoStepMatch{}, false
	}
	advanced := append([]evidence.TodoItem(nil), todos...)
	evidence.AdvanceSerialTodo(advanced, match.Index-1)
	for i, todo := range advanced {
		if strings.TrimSpace(todo.Status) == "in_progress" {
			return false, evidence.TodoStepMatch{Found: true, Index: i + 1, Content: todo.Content,
				Status: todo.Status, ActiveForm: todo.ActiveForm, StepID: todo.StepID}, true
		}
	}
	// AdvanceSerialTodo leaves exactly one item current while any remains, so
	// no current item means none is left.
	return true, evidence.TodoStepMatch{}, true
}

// currentTodos reads the same list verifyTodoStep matched against.
func currentTodos(ctx context.Context) []evidence.TodoItem {
	if ledger, ok := evidence.FromContext(ctx); ok {
		if todos, _ := ledger.LatestTodos(); len(todos) > 0 {
			return todos
		}
	}
	todos, _ := evidence.TodoStateFromContext(ctx)
	return todos
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

func verifyStepEvidence(ctx context.Context, items []stepEvidence) (hostVerified int, manualUnverified int, err error) {
	ledger, ok := evidence.FromContext(ctx)
	if !ok {
		return 0, 0, nil
	}
	for i, e := range items {
		switch e.Kind {
		case "verification":
			command := strings.TrimSpace(e.Command)
			if command == "" {
				return 0, 0, fmt.Errorf("evidence %d: verification command is required for host verification — cite the command you ran, or use kind \"manual\"", i+1)
			}
			if !ledger.HasSuccessfulCommand(command) && !verifyCommandFromSession(ctx, command) {
				if ledger.HasFailedCommand(command) {
					return 0, 0, fmt.Errorf("evidence %d: verification command %q ran but exited non-zero, so it can't prove the step; if the non-zero exit is itself the expected proof (e.g. a file is gone), re-run it so it succeeds (append \"|| true\") and sign off again", i+1, command)
				}
				hint := allCommandHints(ctx, ledger)
				return 0, 0, fmt.Errorf("evidence %d: verification command %q has no matching successful receipt — cite the command exactly as it ran in the session%s", i+1, command, hint)
			}
			_, deliveryHasMutation := ledger.LatestSuccessfulMutationIndex()
			if evidence.DeliveryProfileFromContext(ctx) && deliveryHasMutation && !evidence.IsDeliveryVerificationCommand(command) {
				// Naming only the category sends the model back with another
				// unrecognized command — a project's own checker (go run ./tools/x)
				// reads as "a project lint command" and is refused just the same.
				// The summary is the accepted set, so cite it here as well.
				return 0, 0, fmt.Errorf("evidence %d: command %q ran successfully but is not a recognized delivery verification; do not cite an opaque command as verification. If this was only a visible/manual inspection, cite kind manual or files without a command, then rerun and cite a recognized verifier after any opaque mutation. %s", i+1, command, evidence.VerificationCommandSummary())
			}
			hostVerified++
		case "review":
			if !ledger.HasCompletedReview() {
				return 0, 0, fmt.Errorf("evidence %d: review evidence requires a completed review run in this turn; after a mutation, the review must be newer and cover the changed result", i+1)
			}
			hostVerified++
		case "diff":
			if len(e.Paths) == 0 {
				return 0, 0, fmt.Errorf("evidence %d: diff evidence requires paths for host verification — cite the files you changed", i+1)
			}
			if !everyCitationProven(ctx, e.Paths, func(p []string) bool {
				return ledger.HasSuccessfulWrite(p) || verifyPathsFromSession(ctx, p, true)
			}) {
				return 0, 0, fmt.Errorf("evidence %d: diff paths have no matching successful writer receipt in this turn%s", i+1, receiptHint("files written this turn", ledger.TouchedPaths(8, true)))
			}
			hostVerified++
		case "files":
			if len(e.Paths) == 0 {
				return 0, 0, fmt.Errorf("evidence %d: files evidence requires paths for host verification — cite the files you touched", i+1)
			}
			if !everyCitationProven(ctx, e.Paths, func(p []string) bool {
				return ledger.HasSuccessfulReadOrWrite(p) || ledger.HasSuccessfulBashMentioningPaths(p) || verifyPathsFromSession(ctx, p, false)
			}) {
				return 0, 0, fmt.Errorf("evidence %d: file paths have no matching successful read/write receipt in this turn%s", i+1, receiptHint("files touched this turn", ledger.TouchedPaths(8, false)))
			}
			hostVerified++
		case "manual":
			manualUnverified++
		}
	}
	// A manual note stands alone only where the turn has nothing else to cite.
	// Once the ledger holds a write or a command, the proof is in the receipts,
	// and prose may not stand in for the one the host can actually read.
	if hostVerified == 0 && manualUnverified > 0 {
		_, mutated := ledger.LatestSuccessfulMutationIndex()
		if mutated || ledger.HasSuccessfulVerificationCommand() {
			return 0, 0, fmt.Errorf("manual evidence cannot stand alone in a turn that already has receipts — cite what the host observed: the command you ran (kind verification), or the files you changed or read (kind diff/files)%s", receiptHint("touched this turn", ledger.TouchedPaths(8, false)))
		}
	}
	return hostVerified, manualUnverified, nil
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
		parts = append(parts, evidence.TodoCitation(t.StepID, i+1, strconv.Quote(content)))
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

// builtinToolFacts answers for a name in the transcript out of the set this
// binary ships, which is the same reach the writer name table it replaces had.
// An unregistered name reads as a read, so a replay invents no writes.
var builtinToolFacts = func() func(string) evidence.ToolFacts {
	byName := sync.OnceValue(func() map[string]evidence.ToolFacts {
		m := make(map[string]evidence.ToolFacts)
		for _, t := range tool.Builtins() {
			m[t.Name()] = evidence.ToolFacts{ReadOnly: t.ReadOnly(), WritesNamedPaths: tool.WritesNamedPaths(t), EffectsUnstated: tool.EffectsUnstated(t)}
		}
		return m
	})
	return func(name string) evidence.ToolFacts {
		if f, ok := byName()[name]; ok {
			return f
		}
		return evidence.ToolFacts{ReadOnly: true}
	}
}()

// everyCitationProven reports whether each cited path has a receipt under one
// of the forms that name the same workspace file.
func everyCitationProven(ctx context.Context, cited []string, proven func([]string) bool) bool {
	root := evidence.WorkspaceRootFromContext(ctx)
	for _, p := range cited {
		if !slices.ContainsFunc(evidence.CitationForms(root, p), func(form string) bool { return proven([]string{form}) }) {
			return false
		}
	}
	return true
}

// verifyPathsFromSession is the diff/files analogue of verifyCommandFromSession:
// it lets a completion cite a file written or read in an earlier turn (the
// per-turn ledger only has this turn). wantWrite restricts to writer tools.
func verifyPathsFromSession(ctx context.Context, paths []string, wantWrite bool) bool {
	msgs, ok := evidence.SessionMessagesFromContext(ctx)
	if !ok {
		return false
	}
	return evidence.PathsProvenInSession(msgs, paths, wantWrite, builtinToolFacts)
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
	return fmt.Sprintf("; %s: %q — cite one as it actually ran, or run the check now", label, distinguish(items))
}

// What tells one of these apart from the next, within a budget. Cutting the
// tail is the wrong end for paths: under a deep root every entry becomes the
// same eighty characters and an ellipsis — one real session offered three
// identical strings as the evidence it should have cited. The shared head
// carries nothing once they are read together, so it goes before the middle.
func distinguish(items []string) []string {
	out := make([]string, len(items))
	copy(out, items)
	if head := commonDirPrefix(out); head != "" {
		for i, item := range out {
			out[i] = "…/" + strings.TrimPrefix(item, head)
		}
	}
	for i, item := range out {
		if len([]rune(item)) <= receiptHintWidth {
			continue
		}
		r := []rune(item)
		keep := receiptHintWidth - 1
		out[i] = string(r[:keep/3]) + "…" + string(r[len(r)-(keep-keep/3):])
	}
	return out
}

// How wide one cited item may be. Wide enough for a path with a couple of
// directories on it, narrow enough that a list of eight stays readable.
const receiptHintWidth = 80

// The longest directory prefix every item shares. Directory, not character: a
// prefix that stops mid-name would read as a different file.
func commonDirPrefix(items []string) string {
	if len(items) < 2 {
		return ""
	}
	head := items[0]
	for _, item := range items[1:] {
		for !strings.HasPrefix(item, head) {
			cut := strings.LastIndexByte(strings.TrimSuffix(head, "/"), '/')
			if cut <= 0 {
				return ""
			}
			head = head[:cut+1]
		}
	}
	if !strings.HasSuffix(head, "/") {
		cut := strings.LastIndexByte(head, '/')
		if cut <= 0 {
			return ""
		}
		head = head[:cut+1]
	}
	return head
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
