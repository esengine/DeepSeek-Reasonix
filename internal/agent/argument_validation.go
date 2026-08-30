package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/tool"
)

const maxArgumentValidationMessageBytes = 4 << 10

// applyArgumentValidation runs after a proxy has resolved to its concrete
// target and before hooks, permission, leases, subagents, or MCP tools/call.
func (a *Agent) applyResolvedTargetGates(plan *toolCallPlan) (toolOutcome, bool) {
	if blocked, early := a.applyDispatchGenerationGate(plan); early {
		return blocked, true
	}
	return a.applyArgumentValidation(plan)
}

func (a *Agent) applyArgumentValidation(plan *toolCallPlan) (toolOutcome, bool) {
	if plan == nil || plan.execTool == nil {
		return toolOutcome{}, false
	}
	normalized := tool.NormalizeArguments(plan.execArgs)
	plan.execArgs = normalized
	plan.permArgs = normalized
	plan.evidenceArgs = normalized
	result := tool.ValidateArguments(plan.execTool, normalized)
	failed := result.CompileErr != nil || len(result.Violations) > 0
	if a.capabilityAudit != nil {
		a.capabilityAudit.RecordArgumentValidation(failed, result.Skipped, false)
	}
	if result.Skipped {
		return toolOutcome{}, false
	}
	if result.CompileErr != nil {
		msg := fmt.Sprintf("host configuration error: tool %q has an invalid argument schema (schema fingerprint %s); execution was not dispatched", plan.permName, shortSchemaFingerprint(result.Fingerprint))
		a.noteCapabilityInvocation(plan.call.Name, json.RawMessage(plan.call.Arguments), errors.New(msg))
		return toolOutcome{output: "error: " + msg, errMsg: argumentValidationSignature(plan.permName, result.Fingerprint, "schema")}, true
	}
	if len(result.Violations) == 0 {
		a.clearSchemaError(plan.permName, result.Fingerprint)
		return toolOutcome{}, false
	}
	sig := argumentValidationSignature(plan.permName, result.Fingerprint, result.Violations[0].Keyword)
	msg, blocked := a.schemaErrorContract(plan, result, sig)
	a.noteCapabilityInvocation(plan.call.Name, json.RawMessage(plan.call.Arguments), errors.New(msg))
	return toolOutcome{output: msg, blocked: blocked, errMsg: sig}, true
}

func hostValidateBeforeDispatch(target tool.Tool, args json.RawMessage) (bool, string) {
	result := tool.ValidateArguments(target, args)
	if result.Skipped {
		return false, ""
	}
	if result.CompileErr != nil {
		return true, fmt.Sprintf("host configuration error: tool %q has an invalid argument schema (schema fingerprint %s); execution was not dispatched", target.Name(), shortSchemaFingerprint(result.Fingerprint))
	}
	if len(result.Violations) == 0 {
		return false, ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "argument validation failed for %q (schema fingerprint %s; remote_dispatched=false):", target.Name(), shortSchemaFingerprint(result.Fingerprint))
	for _, violation := range result.Violations {
		path := violation.Path
		if path == "" {
			path = "/"
		}
		fmt.Fprintf(&b, "\n- %s: %s; expected %s", path, violation.Keyword, violation.Expected)
	}
	return true, truncateValidationMessage(b.String())
}

func argumentValidationMessage(plan *toolCallPlan, result tool.ArgumentValidationResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "argument validation failed for %q (schema fingerprint %s; remote_dispatched=false):", plan.permName, shortSchemaFingerprint(result.Fingerprint))
	for _, violation := range result.Violations {
		path := violation.Path
		if path == "" {
			path = "/"
		}
		fmt.Fprintf(&b, "\n- %s: %s; expected %s", path, violation.Keyword, violation.Expected)
	}
	if id := strings.TrimSpace(plan.resolved.CapabilityID); id != "" {
		if strings.HasPrefix(id, "skill:") && plan.permName == "run_skill" {
			b.WriteString("\nUse this exact nested call shape:\n")
			b.WriteString(`{"action":"call","capability_id":"`)
			b.WriteString(escapeJSONString(id))
			b.WriteString(`","arguments":{"arguments":"specific review or implementation task"}}`)
		} else {
			fmt.Fprintf(&b, "\nInspect %q for its exact argument schema, then retry action=call once with a JSON object matching it.", id)
		}
	}
	return truncateValidationMessage(b.String())
}

func argumentValidationSignature(target, fingerprint, category string) string {
	return "argument_validation:" + target + ":" + shortSchemaFingerprint(fingerprint) + ":" + category
}

func shortSchemaFingerprint(fingerprint string) string {
	if len(fingerprint) <= 16 {
		return fingerprint
	}
	return fingerprint[:16]
}

func escapeJSONString(value string) string {
	b, _ := json.Marshal(value)
	if len(b) < 2 {
		return ""
	}
	return string(b[1 : len(b)-1])
}

func truncateValidationMessage(message string) string {
	if len(message) <= maxArgumentValidationMessageBytes {
		return message
	}
	return message[:maxArgumentValidationMessageBytes-len("\n[truncated]")] + "\n[truncated]"
}
