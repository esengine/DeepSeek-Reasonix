package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/tool"
)

// CodeMaskedSecretCall identifies an MCP call refused for carrying the mark a
// model reads in place of a secret the user saved.
const CodeMaskedSecretCall = "secret.masked_call"

// refuseMarkedMCPCall stops an MCP call carrying a secret's mark: the server
// would act on the mark, writing it where the value belongs or sending it as a
// credential, and what it does with either is beyond the host's sight.
func (a *Agent) refuseMarkedMCPCall(plan *toolCallPlan) (toolOutcome, bool) {
	if _, ok := tool.MCPBindingFor(plan.execTool); !ok {
		return toolOutcome{}, false
	}
	labels := secrets.Masker().MarksJSON(plan.execArgs)
	if len(labels) == 0 {
		return toolOutcome{}, false
	}
	msg := fmt.Sprintf("refused: the arguments hold a [secret:...] mark (%s), which you read in place of a secret the user saved. An MCP tool would receive the mark, not the value; no tool call can carry the value. Use a shell command that reads $REASONIX_SECRET_<NAME> instead.",
		strings.Join(labels, ", "))
	return toolOutcome{output: msg, blocked: true, errMsg: firstLine(msg), refusalCode: CodeMaskedSecretCall}, true
}
