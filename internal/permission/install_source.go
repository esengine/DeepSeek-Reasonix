package permission

import (
	"encoding/json"
	"strings"
)

// InstallSourceIsPlanOnly reports an install_source call that only produces a
// plan. The tool is a writer as a whole, but its preview phase writes nothing.
// An uninstall is not a preview: it removes what is already installed, and it
// is the one op with nothing to read first.
func InstallSourceIsPlanOnly(args json.RawMessage) bool {
	var call struct {
		Op    string `json:"op"`
		Apply bool   `json:"apply"`
	}
	if err := json.Unmarshal(args, &call); err != nil {
		return false
	}
	if call.Apply {
		return false
	}
	return call.Op == "" || call.Op == "install"
}

// selfExtendHumanRisk is the install-plan grade no blanket allow covers. It
// mirrors installsource.RiskHigh, which cannot be imported here without pulling
// the install stack underneath the permission layer;
// installsource's TestHighRiskPlanAsksEvenUnderBlanketAllow holds the two together.
const selfExtendHumanRisk = "high:"

// installSourceTool is the rule-name form self-extension is decided under.
const installSourceTool = "install_source"

// subjectRequiresHuman reports a call that comes back to the user even when the
// fallback mode allows everything. "Allow every write" is a statement about
// this workspace's files, never permission for the agent to give itself a
// resident process, a lifecycle hook, or an external server. An explicit allow
// rule for that plan's ticket still wins — that is how the line gets moved.
func subjectRequiresHuman(toolName, subject string) bool {
	if widensWriteFence(toolName) {
		return true
	}
	return canonicalRuleTool(toolName) == installSourceTool && strings.HasPrefix(subject, selfExtendHumanRisk)
}
