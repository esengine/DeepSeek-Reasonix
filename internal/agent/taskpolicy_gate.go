package agent

import (
	"encoding/json"
	"strings"

	"reasonix/internal/evidence"
)

// taskPolicyToolGate binds natural-language constraints to both direct tools
// and use_capability-resolved targets before any workspace mutation.
func (a *Agent) taskPolicyToolGate(plan *toolCallPlan, args json.RawMessage) (toolOutcome, bool) {
	if !a.turn.policySet {
		return toolOutcome{}, false
	}
	if plan.effects.StateMutation && !a.turn.policy.AllowsMutation() {
		reason := strings.TrimSpace(plan.effects.Reason)
		if reason == "" {
			reason = "state mutation whose effects cannot be proven read-only"
		}
		return policyBlock(
			"the current task policy forbids "+reason+" (user constraint or plan mode)",
			"task policy forbids "+reason,
		)
	}
	if !a.turn.policy.AllowsExternal() && isExternalActionTool(plan.evidenceName, plan.permName, args) {
		return policyBlock("the current task policy forbids push/publish/deploy-style external actions", "task policy forbids external action")
	}
	if isVerificationCommandTool(plan.evidenceName, plan.permName, args) {
		if !a.turn.policy.AllowsTests() {
			return policyBlock("the current task policy forbids running verification commands (user constraint)", "task policy forbids verification commands")
		}
		if !a.turn.policy.AllowsCommand(bashCommandFromArgs(args)) {
			return policyBlock("the current task policy allows only the verification commands named by the user", "verification command is outside the user allowlist")
		}
	}
	if !a.turn.policy.AllowExploreSubagent && isExplorationSubagentTool(plan.evidenceName, plan.permName, args) {
		return policyBlock("the current task policy does not allow proactive explore/research sub-agents for this turn", "exploration sub-agent is outside the turn policy")
	}
	return toolOutcome{}, false
}

func policyBlock(output, reason string) (toolOutcome, bool) {
	return toolOutcome{output: "blocked: " + output, blocked: true, errMsg: "blocked: " + reason}, true
}

func isVerificationCommandTool(evidenceName, permName string, args json.RawMessage) bool {
	name := strings.ToLower(strings.TrimSpace(evidenceName))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(permName))
	}
	if name != "bash" && name != "shell" {
		return false
	}
	return evidence.IsVerificationCommand(bashCommandFromArgs(args))
}

func isExplorationSubagentTool(evidenceName, permName string, args json.RawMessage) bool {
	name := strings.ToLower(strings.TrimSpace(evidenceName))
	if name == "" || name == "use_capability" {
		name = strings.ToLower(strings.TrimSpace(permName))
	}
	if name == "explore" || name == "research" {
		return true
	}
	if name != "run_skill" && name != "read_only_skill" {
		return false
	}
	var payload struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(args, &payload) != nil {
		return false
	}
	skillName := strings.ToLower(strings.TrimSpace(payload.Name))
	return skillName == "explore" || skillName == "research"
}
