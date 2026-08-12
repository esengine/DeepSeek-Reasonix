package agent

import (
	"encoding/json"
	"strings"
)

// taskPolicyToolGate binds natural-language constraints to both direct tools
// and use_capability-resolved targets before any workspace mutation.
func (a *Agent) taskPolicyToolGate(plan *toolCallPlan, args json.RawMessage) (toolOutcome, bool) {
	if !a.turnPolicySet {
		return toolOutcome{}, false
	}
	if plan.mutates && !a.turnPolicy.AllowsMutation() {
		return policyBlock("the current task policy forbids workspace modifications (user constraint or plan mode)", "task policy forbids mutation")
	}
	if !a.turnPolicy.AllowsExternal() && isExternalActionTool(plan.evidenceName, plan.permName, args) {
		return policyBlock("the current task policy forbids push/publish/deploy-style external actions", "task policy forbids external action")
	}
	if isVerificationCommandTool(plan.evidenceName, plan.permName, args) {
		if !a.turnPolicy.AllowsTests() {
			return policyBlock("the current task policy forbids running tests (user constraint)", "task policy forbids tests")
		}
		if !a.turnPolicy.AllowsCommand(bashCommandFromArgs(args)) {
			return policyBlock("the current task policy allows only the verification commands named by the user", "verification command is outside the user allowlist")
		}
	}
	if !a.turnPolicy.AllowExploreSubagent && isExplorationSubagentTool(plan.evidenceName, plan.permName, args) {
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
	cmd := strings.ToLower(bashCommandFromArgs(args))
	for _, verifier := range []string{
		"go test", "npm test", "npm run test", "pnpm test", "yarn test",
		"pytest", "cargo test", "mvn test", "gradle test", "make test", "bun test", "deno test",
	} {
		if strings.Contains(cmd, verifier) {
			return true
		}
	}
	return false
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
