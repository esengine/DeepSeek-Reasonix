package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(updateGoal{}) }

// updateGoal records the model's structured per-turn goal disposition for the
// active goal turn. Like complete_step it has no host side effects: the call
// only records candidate state, and the real FSM transition happens after the
// turn ends, once Delivery readiness and budget checks pass. It is a host
// workflow operation — it never requires write approval and grants no
// permissions. Outside an active goal turn it fails closed without changing
// any state, so plain chat cannot be hijacked into goal machinery.
type updateGoal struct{}

func (updateGoal) Name() string { return "update_goal" }

func (updateGoal) Description() string {
	return "Report this turn's disposition for the active goal. Call it at the end of every goal turn instead of using prose markers: `continue` (work is ongoing — give a concrete next_action), `complete` (the request is fully done, output format and constraints satisfied, and verification was attempted or reported unavailable), or `blocked` (only the user can unblock: missing user-only information, an irreversible/externally visible operation, or changed scope). The host validates your claim against Delivery acceptance criteria and decides whether to continue automatically. Fields: `status` (required, one of continue|complete|blocked), `reason` (required for continue and blocked, optional for complete), `next_action` (optional concrete next step; recommended for continue), `completion` (recommended with complete: `verified` is checked against real receipts, while `unverified` and `risks` are yours to declare and never count against you)."
}

func (updateGoal) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "status":{"type":"string","enum":["continue","complete","blocked"],"description":"continue = keep working autonomously; complete = the goal is fully done and verified; blocked = only the user can unblock."},
  "reason":{"type":"string","description":"Short explanation. REQUIRED for continue and blocked; optional for complete."},
  "next_action":{"type":"string","description":"Optional concrete next step. Recommended for continue so the host can guide the next turn."},
  "completion":{
    "type":"object",
    "description":"Your own account of the finished work.",
    "properties":{
      "verified":{"type":"array","items":{"type":"string"},"description":"Commands you ran as proof, as they actually ran. One that never ran, failed, or predates your latest change is recorded as an unbacked claim."},
      "unverified":{"type":"array","items":{"type":"string"},"description":"What you did NOT verify. The host cannot infer what you skipped, so stating it is the only way it is known — and it never blocks completion."},
      "risks":{"type":"array","items":{"type":"string"},"description":"Known risks to carry forward."}
    }
  }
},
"required":["status"]
}`)
}

// ReadOnly is true: update_goal only records a claim; the host performs the
// state transition after the turn. It never needs approval and cannot expand
// tool permissions or bypass sandbox policy.
func (updateGoal) ReadOnly() bool { return true }

func (updateGoal) ProviderVisible(ctx context.Context) bool {
	_, ok := tool.GoalTurnRecorderFromContext(ctx)
	return ok
}

// PlanModeSafe reports true: the tool is read-only host bookkeeping, and
// outside an active goal turn its Execute fails closed anyway.
func (updateGoal) PlanModeSafe() bool { return true }

func (updateGoal) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Status     string `json:"status"`
		Reason     string `json:"reason"`
		NextAction string `json:"next_action"`
		Completion struct {
			Verified   []string `json:"verified"`
			Unverified []string `json:"unverified"`
			Risks      []string `json:"risks"`
		} `json:"completion"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid update_goal args: %w", err)
	}
	p.Status = strings.ToLower(strings.TrimSpace(p.Status))
	switch p.Status {
	case "continue", "complete", "blocked":
	default:
		return "", fmt.Errorf("update_goal: status must be one of continue|complete|blocked, got %q — no goal state was changed", p.Status)
	}
	if (p.Status == "continue" || p.Status == "blocked") && strings.TrimSpace(p.Reason) == "" {
		return "", fmt.Errorf("update_goal: reason is required for %s — no goal state was changed", p.Status)
	}
	for i, command := range p.Completion.Verified {
		if strings.TrimSpace(command) == "" {
			return "", fmt.Errorf("update_goal: completion.verified[%d] is empty — cite the command as it actually ran, or leave the list out", i)
		}
	}
	recorder, ok := tool.GoalTurnRecorderFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("update_goal is only available while an active goal turn is running — no goal state was changed")
	}
	result, err := recorder.RecordGoalReport(tool.GoalReport{
		Status:     p.Status,
		Reason:     strings.TrimSpace(p.Reason),
		NextAction: strings.TrimSpace(p.NextAction),
	})
	if err != nil || p.Status != "complete" {
		return result, err
	}
	return result + completionNote(len(p.Completion.Verified), len(p.Completion.Unverified), len(p.Completion.Risks)), nil
}

// completionNote tells the model what its own account will contribute to the
// host's completion report. The claim is reconciled against real receipts
// after the turn; it is recorded, never trusted.
func completionNote(verified, unverified, risks int) string {
	if verified+unverified+risks == 0 {
		return " No completion account was given: the report will carry only what the host could observe."
	}
	return fmt.Sprintf(" Completion account recorded (%d verified, %d unverified, %d risks); the verified commands are reconciled against this session's receipts.",
		verified, unverified, risks)
}
