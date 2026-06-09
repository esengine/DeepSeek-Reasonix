package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
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
	return "Record the evidence-backed completion of ONE step of an approved plan. Call it as you finish each step instead of silently moving on: it signs the step off with PROOF it is done — the verification you ran (command + result), the diff/files you changed, or a manual check. A completion with no evidence is REJECTED, so don't claim a step is done until you can show why. Keep the task list moving with todo_write (set the next step in_progress); use complete_step for the formal, evidenced sign-off of the finished one. Fields: `step` (which step — its title or number, matching the task list), `result` (what is now true/changed), `evidence` (≥1 item, each with `kind` = verification|diff|files|manual and a `summary`, plus optional `command`/`paths`), and optional `notes`."
}

func (completeStep) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "step":{"type":"string","description":"Which plan step this completes — its title or number, matching the task list."},
  "result":{"type":"string","description":"What is now true or changed as a result of finishing this step."},
  "evidence":{
    "type":"array",
    "minItems":1,
    "description":"Proof the step is done. At least one item is required.",
    "items":{
      "type":"object",
      "properties":{
        "kind":{"type":"string","enum":["verification","diff","files","manual"],"description":"verification = a command/test was run; diff = a concrete code change; files = files created/edited/inspected; manual = a manual check."},
        "summary":{"type":"string","description":"The evidence itself: the test result, what the diff does, or what was confirmed."},
        "command":{"type":"string","description":"The command run, for verification evidence (e.g. \"go test ./...\")."},
        "paths":{"type":"array","items":{"type":"string"},"description":"Files this evidence refers to."}
      },
      "required":["kind","summary"]
    }
  },
  "notes":{"type":"string","description":"Optional caveats, follow-ups, or anything deferred."}
},
"required":["step","result","evidence"]
}`)
}

// ReadOnly is true: complete_step only records a claim (no filesystem or process
// effect), so it never needs approval and stays available alongside todo_write.
func (completeStep) ReadOnly() bool { return true }

func (completeStep) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Step     string         `json:"step"`
		Result   string         `json:"result"`
		Evidence []stepEvidence `json:"evidence"`
		Notes    string         `json:"notes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.Step) == "" {
		return "", fmt.Errorf("step is required — name the plan step you are completing")
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
	todoMatch, hasTodo, err := verifyTodoStep(ctx, p.Step)
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
		todoStatus = fmt.Sprintf(" Todo step: todo-matched %d.", todoMatch.Index)
	}
	projectStatus := ""
	if projectVerified > 0 {
		projectStatus = fmt.Sprintf(" Project checks: project checks %d.", projectVerified)
	}
	return fmt.Sprintf("Step %q signed off with %d evidence item(s) [%s].%s Move the next step to in_progress with todo_write.",
		p.Step, len(p.Evidence), strings.Join(kinds, ", "), hostStatus+todoStatus+projectStatus), nil
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
				return 0, 0, fmt.Errorf("evidence %d: verification command is required for host verification", i+1)
			}
			if !ledger.HasSuccessfulCommand(command) && !verifyCommandFromSession(ctx, command) {
				return 0, 0, fmt.Errorf("evidence %d: verification command %q has no matching successful bash receipt in this turn", i+1, command)
			}
			hostVerified++
		case "diff":
			if len(e.Paths) == 0 {
				return 0, 0, fmt.Errorf("evidence %d: diff evidence requires paths for host verification", i+1)
			}
			if !ledger.HasSuccessfulWrite(e.Paths) {
				return 0, 0, fmt.Errorf("evidence %d: diff paths have no matching successful writer receipt in this turn", i+1)
			}
			hostVerified++
		case "files":
			if len(e.Paths) == 0 {
				return 0, 0, fmt.Errorf("evidence %d: files evidence requires paths for host verification", i+1)
			}
			if !ledger.HasSuccessfulReadOrWrite(e.Paths) {
				return 0, 0, fmt.Errorf("evidence %d: file paths have no matching successful read/write receipt in this turn", i+1)
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
		return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q has no matching todo_write item in this turn", step)
	}
	switch match.Status {
	case "", "pending", "in_progress", "completed":
		return match, true, nil
	default:
		return evidence.TodoStepMatch{}, true, fmt.Errorf("step %q matches todo %d (%q) but its status is %q; complete_step requires pending, in_progress, or completed", step, match.Index, match.Content, match.Status)
	}
}

// verifyCommandFromSession scans the full conversation history (not just the
// per-turn ledger) so that a complete_step can cite commands that ran in an
// earlier turn, that used a named tool instead of bash, or that were cited
// with a slightly different string (trailing …,
// truncation).
//
// Three scenarios this covers:
//  1. Cross-turn: command ran in a prior turn, ledger was reset — scan the
//     session transcript for the exact bash command.
//  2. Non-bash tool: the model used the `ls` reader tool rather than
//     bash "ls .", but complete_step cites command="ls ." — match by tool
//     name (the first word of the command).
//  3. Truncated string: the actual command was
//     `find . -type f -not -path '*/node_modules/*'` but the model wrote
//     command="find . -type f ..." — normalize both sides (strip …,
//     trim, collapse whitespace) and fall back to prefix matching.
func verifyCommandFromSession(ctx context.Context, command string) bool {
	msgs, ok := evidence.SessionMessagesFromContext(ctx)
	if !ok {
		return false
	}
	lookup := normalizeVerifyCommand(command)
	if lookup == "" {
		return false
	}
	toolName := firstWord(lookup)

	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			cmd := extractCommandFromCall(tc.Name, tc.Arguments)
			if cmd == "" {
				continue
			}
			norm := normalizeVerifyCommand(cmd)
			if norm == lookup {
				return true
			}
			// Prefix match for truncated commands (scenario 3). Require the
			// shorter side to be at least 8 chars to prevent false positives
			// on very short fragments like "find" or "ls".
			if len(norm) >= 8 && len(lookup) >= 8 {
				if strings.HasPrefix(norm, lookup) || strings.HasPrefix(lookup, norm) {
					return true
				}
			}
			// Non-bash tool name match (scenario 2): the first word of the
			// command matches a tool call name that isn't bash.
			if toolName != "" && toolName != "bash" && tc.Name == toolName {
				return true
			}
		}
	}
	return false
}

// normalizeVerifyCommand strips trailing ellipsis,
// trims whitespace, and collapses internal whitespace into single spaces.
func normalizeVerifyCommand(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "...")
	s = strings.TrimSuffix(s, "…")
	s = strings.TrimSpace(s)
	return strings.Join(strings.Fields(s), " ")
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
