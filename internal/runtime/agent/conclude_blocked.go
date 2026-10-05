package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

// ConcludeBlockedTool is the execution turn's honest exit. The host otherwise
// recognizes one way to finish — everything verified — so saying a task is
// impossible was reported as a failed run while quietly faking the check was
// not. The claim costs what success costs: every reference must match a receipt
// from this turn, so "I cannot" is as investigated as "I did".
type ConcludeBlockedTool struct{}

func NewConcludeBlockedTool() *ConcludeBlockedTool { return &ConcludeBlockedTool{} }

func (*ConcludeBlockedTool) Name() string { return "conclude_blocked" }

func (*ConcludeBlockedTool) Description() string {
	return "Declare that part of the task cannot be completed as specified, after you have established why. Use it when the work is genuinely impossible or needs a decision only the user can make — mutually exclusive requirements, a missing credential, an external service that is down — never as an exit from work that is merely hard, and never before you have done the part that is possible. Cite `evidence` the host can check against what you ran or read this turn; a claim with nothing behind it is rejected. Name the `criterion_id` when a specific acceptance criterion is the one that cannot be met. The turn ends reported as blocked, and `blocker` is what the user is told."
}

func (*ConcludeBlockedTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "blocker":{"type":"string","description":"What cannot be done and why it cannot. Shown to the user."},
  "criterion_id":{"type":"string","description":"Optional id of the acceptance criterion that cannot be met."},
  "evidence":{
    "type":"array",
    "minItems":1,
    "description":"What you ran or read that establishes the blocker. At least one must match a receipt from this turn.",
    "items":{
      "type":"object",
      "properties":{
        "command":{"type":"string","description":"A command you ran this turn, exactly as it ran. A command that failed is evidence too."},
        "paths":{"type":"array","items":{"type":"string"},"description":"Files you read or wrote this turn."}
      }
    }
  }
},
"required":["blocker","evidence"]
}`)
}

// ReadOnly is true: the call records a conclusion and changes nothing. The
// evidence it cites was produced by the work that came before it.
func (*ConcludeBlockedTool) ReadOnly() bool { return true }

func (*ConcludeBlockedTool) Reach() tool.Reach { return tool.ReachHostControl }

type blockedEvidence struct {
	Command string   `json:"command"`
	Paths   []string `json:"paths"`
}

func (*ConcludeBlockedTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var payload struct {
		Blocker     string            `json:"blocker"`
		CriterionID string            `json:"criterion_id"`
		Evidence    []blockedEvidence `json:"evidence"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	blocker := strings.TrimSpace(payload.Blocker)
	if blocker == "" {
		return "", fmt.Errorf("conclude_blocked needs a blocker: state what cannot be done and why")
	}
	if _, planning := planSubmissionFromContext(ctx); planning {
		return "", fmt.Errorf("conclude_blocked ends an execution turn, not a planning one; submit a plan that names the unknown, or call conclude_no_changes")
	}
	ledger, ok := evidence.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("conclude_blocked is unavailable without an evidence ledger for this turn")
	}
	if err := checkBlockedEvidence(ledger, payload.Evidence); err != nil {
		return "", err
	}
	return "Recorded: blocked. The turn ends here and is reported as blocked — state the same thing in your reply, including what you did complete.", nil
}

// checkBlockedEvidence requires one reference the host observed. Unlike a
// sign-off, a failed command counts: the run that proves a task impossible is
// usually the run that failed, and demanding a successful one would leave the
// honest conclusion unprovable.
func checkBlockedEvidence(ledger *evidence.Ledger, items []blockedEvidence) error {
	if len(items) == 0 {
		return fmt.Errorf("conclude_blocked needs evidence: cite a command you ran or files you read that establish the blocker")
	}
	for i, item := range items {
		command := strings.TrimSpace(item.Command)
		if command != "" {
			if ledger.HasSuccessfulCommand(command) || ledger.HasFailedCommand(command) {
				return nil
			}
			continue
		}
		if len(item.Paths) > 0 && ledger.HasSuccessfulReadOrWrite(item.Paths) {
			return nil
		}
		if command == "" && len(item.Paths) == 0 {
			return fmt.Errorf("evidence %d: give a command or paths", i+1)
		}
	}
	return fmt.Errorf("none of the cited evidence matches a receipt from this turn — cite a command exactly as it ran, or files you actually read")
}
