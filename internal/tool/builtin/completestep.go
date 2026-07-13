package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
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
	Kind    string   `json:"kind"`
	Summary string   `json:"summary"`
	Command string   `json:"command,omitempty"`
	Paths   []string `json:"paths,omitempty"`
}

// validEvidenceKinds are the evidence forms a completion may cite. "checkpoint"
// (main's fourth kind) is omitted — v2 has no checkpoint system.
var validEvidenceKinds = map[string]bool{
	"verification": true, // a command/test was run; cite it and its outcome
	"diff":         true, // a concrete code change; cite what changed
	"files":        true, // files created/edited/inspected; cite the paths
	"manual":       true, // a manual check; cite what was confirmed and how
}

func (completeStep) Name() string { return "complete_step" }

func (completeStep) Description() string {
	return "Record the evidence-backed completion of ONE step of an approved plan. Call it as you finish each step instead of silently moving on: it signs the step off with PROOF it is done — the verification you ran (command + result), the diff/files you changed, or a manual check. A completion with no evidence is REJECTED, so don't claim a step is done until you can show why. The host advances the task list for you when you sign off — it marks this step completed and moves the next to in_progress, so you don't need a separate todo_write to mark completions. Fields: `step` (which step — its title or number, matching the task list), `result` (what is now true/changed), `evidence` (≥1 item, each with `kind` = verification|diff|files|manual and a `summary`, plus optional `command`/`paths`), and optional `notes`."
}

func (completeStep) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "step":{"type":"string","description":"Which plan step this completes — its title or number, matching the task list."},
  "step_index":{"type":"integer","minimum":1,"description":"Optional 1-based task-list item number. Prefer this when the step title is long or easy to mistype."},
  "result":{"type":"string","description":"What is now true or changed as a result of finishing this step."},
  "evidence":{
    "type":"array",
    "minItems":1,
    "description":"Proof the step is done. At least one item is required.",
    "items":{
      "type":"object",
      "properties":{
        "kind":{"type":"string","enum":["verification","diff","files","manual"],"description":"verification = a command/test was run (command REQUIRED); diff = a concrete code change (paths REQUIRED); files = files created/edited/inspected (paths REQUIRED); manual = a manual check."},
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

// PlanModeSafe reports false: although complete_step is read-only, it signs off a
// completed execution step, which is meaningful only after plan approval — not
// during planning. This self-report backs up its knownBlockedTools entry so the
// gate refuses it even if the classifier wiring regresses.
func (completeStep) PlanModeSafe() bool { return false }

func (completeStep) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Step      string         `json:"step"`
		StepIndex int            `json:"step_index"`
		Result    string         `json:"result"`
		Evidence  []stepEvidence `json:"evidence"`
		Notes     string         `json:"notes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	step := completeStepIdentity(p.Step, p.StepIndex)
	if step == "" {
		return "", fmt.Errorf("step or step_index is required — name the plan step you are completing, or cite its 1-based task-list number")
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

	hostVerified, manualUnverified, err := verifyStepEvidence(ctx, p.Evidence)
	if err != nil {
		return "", err
	}
	todoMatch, hasTodo, err := verifyTodoStep(ctx, step)
	if err != nil {
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
	return fmt.Sprintf("Step %q signed off with %d evidence item(s) [%s].%s The host advanced the task list; continue with the next step.",
		step, len(p.Evidence), strings.Join(kinds, ", "), hostStatus+todoStatus+projectStatus), nil
}

func completeStepIdentity(step string, stepIndex int) string {
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
				return 0, 0, fmt.Errorf("evidence %d: command %q ran successfully but is not a recognized delivery verification; use a project test/check/lint command, or for JavaScript syntax use node --check <file> (a read-only extraction pipeline ending in node --check also works). Arbitrary node -e is treated as an opaque mutation", i+1, command)
			}
			hostVerified++
		case "diff":
			if len(e.Paths) == 0 {
				return 0, 0, fmt.Errorf("evidence %d: diff evidence requires paths for host verification — cite the files you changed", i+1)
			}
			if !ledger.HasSuccessfulWrite(e.Paths) && !verifyPathsFromSession(ctx, e.Paths, true) {
				return 0, 0, fmt.Errorf("evidence %d: diff paths have no matching successful writer receipt in this turn%s", i+1, receiptHint("files written this turn", ledger.TouchedPaths(8, true)))
			}
			hostVerified++
		case "files":
			if len(e.Paths) == 0 {
				return 0, 0, fmt.Errorf("evidence %d: files evidence requires paths for host verification — cite the files you touched", i+1)
			}
			if !ledger.HasSuccessfulReadOrWrite(e.Paths) && !ledger.HasSuccessfulBashMentioningPaths(e.Paths) && !verifyPathsFromSession(ctx, e.Paths, false) {
				return 0, 0, fmt.Errorf("evidence %d: file paths have no matching successful read/write receipt in this turn%s", i+1, receiptHint("files touched this turn", ledger.TouchedPaths(8, false)))
			}
			hostVerified++
		case "manual":
			manualUnverified++
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
	if !ok {
		return evidence.TodoStepMatch{}, false, nil
	}
	match, hasTodo := ledger.MatchLatestTodoStep(step)
	if !hasTodo {
		return evidence.TodoStepMatch{}, false, nil
	}
	if !match.Found {
		return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q has no matching todo_write item in this turn; cite a todo verbatim or by number: %s", step, todoInventory(ledger))
	}
	switch match.Status {
	case "", "pending", "in_progress", "completed":
		return match, true, nil
	default:
		return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q matches todo %d (%q) but its status is %q; complete_step requires pending, in_progress, or completed", step, match.Index, match.Content, match.Status)
	}
}

func todoInventory(ledger *evidence.Ledger) string {
	todos, ok := ledger.LatestTodos()
	if !ok || len(todos) == 0 {
		return "(no todos recorded this turn)"
	}
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
