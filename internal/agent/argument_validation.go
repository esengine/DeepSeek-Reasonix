package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"reasonix/internal/provider"
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
	if result.Skipped || (result.CompileErr == nil && len(result.Violations) == 0) {
		return toolOutcome{}, false
	}
	return a.argumentValidationFailure(plan, result), true
}

// argumentValidationFailure reports an unexecuted call, never a permission
// refusal. Repeated failures are owned by the batch storm breaker.
func (a *Agent) argumentValidationFailure(plan *toolCallPlan, result tool.ArgumentValidationResult) toolOutcome {
	category := "schema"
	if result.CompileErr == nil {
		category = result.Violations[0].Keyword
	}
	msg := argumentValidationMessage(plan, result)
	a.noteCapabilityInvocation(plan.call.Name, json.RawMessage(plan.call.Arguments), errors.New(msg))
	return toolOutcome{output: msg, errMsg: argumentValidationSignature(plan.permName, result.Fingerprint, category)}
}

// diagnoseCapabilityInputFailure runs only after the resolver identifies an
// input error. Successful resolution and unavailable/authorization errors keep
// their historical behavior, even if an ignored envelope field is invalid.
func (a *Agent) diagnoseCapabilityInputFailure(plan *toolCallPlan, err error) toolOutcome {
	result := tool.ValidateArguments(plan.tool, json.RawMessage(plan.call.Arguments))
	if !result.Skipped && (result.CompileErr != nil || len(result.Violations) > 0) {
		if a.capabilityAudit != nil {
			a.capabilityAudit.RecordArgumentValidation(true, false, false)
		}
		return a.argumentValidationFailure(plan, result)
	}
	return toolOutcome{
		output: truncateValidationMessage(fmt.Sprintf("error: %v\nThe capability call was not executed. Correct the indicated input and retry; normal permission checks still apply.", err)),
		errMsg: firstLine(err.Error()),
	}
}

func hostValidateBeforeDispatch(target tool.Tool, args json.RawMessage, capabilityID string) (bool, string) {
	result := tool.ValidateArguments(target, args)
	if result.Skipped || (result.CompileErr == nil && len(result.Violations) == 0) {
		return false, ""
	}
	return true, argumentValidationMessage(&toolCallPlan{
		permName: target.Name(), execTool: target, execArgs: args,
		call:     provider.ToolCall{Name: "use_capability"},
		resolved: tool.ResolvedCall{CapabilityID: capabilityID},
	}, result)
}

func argumentValidationMessage(plan *toolCallPlan, result tool.ArgumentValidationResult) string {
	if result.CompileErr != nil {
		return truncateValidationMessage(fmt.Sprintf("host configuration error: tool %q has an invalid argument schema (schema fingerprint %s); execution was not dispatched. The host schema must be corrected; rewriting call arguments cannot fix it.", plan.permName, shortSchemaFingerprint(result.Fingerprint)))
	}
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
		fmt.Fprintf(&b, "\nThe target was not executed. Correct the target parameters inside %s.arguments; keep the outer capability call envelope.", plan.call.Name)
		if strings.HasPrefix(id, "skill:") && plan.permName == "run_skill" {
			b.WriteString("\nUse this exact nested call shape:\n")
			b.WriteString(`{"action":"call","capability_id":"`)
			b.WriteString(escapeJSONString(id))
			b.WriteString(`","arguments":{"arguments":"specific review or implementation task"}}`)
		} else {
			fmt.Fprintf(&b, "\nInspect %q for its exact argument schema, if needed, then retry action=call with a JSON object matching it.", id)
		}
	} else {
		fmt.Fprintf(&b, "\nThe call was not executed. Pass the parameters for %s directly at the root of its input object, correct the indicated errors and retry.", plan.permName)
	}
	b.WriteString("\nNormal permission checks still apply.")
	if hasRedundantArgumentWrapper(plan.execTool, plan.execArgs) {
		b.WriteString("\nThe sole \"arguments\" wrapper does not match this tool's schema; its inner object matches the expected parameters. Remove that one wrapper from the target parameters when retrying; keep any outer capability call envelope.")
	}
	return truncateValidationMessage(b.String())
}

// hasRedundantArgumentWrapper is a conservative, value-free hint, not a
// transformation. Call only after the original arguments failed validation.
func hasRedundantArgumentWrapper(target tool.Tool, raw json.RawMessage) bool {
	if target == nil {
		return false
	}
	var schema map[string]json.RawMessage
	if json.Unmarshal(target.Schema(), &schema) != nil || string(schema["type"]) != `"object"` {
		return false
	}
	for _, key := range []string{"$ref", "$dynamicRef", "$recursiveRef", "allOf", "anyOf", "oneOf", "not", "if", "then", "else", "patternProperties", "dependencies", "dependentSchemas"} {
		if _, exists := schema[key]; exists {
			return false
		}
	}
	var props map[string]json.RawMessage
	if json.Unmarshal(schema["properties"], &props) != nil || props == nil {
		return false
	}
	if _, exists := props["arguments"]; exists {
		return false
	}
	var outer map[string]json.RawMessage
	if json.Unmarshal(raw, &outer) != nil || len(outer) != 1 {
		return false
	}
	inner, exists := outer["arguments"]
	if !exists {
		return false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(inner, &object) != nil || object == nil {
		return false
	}
	result := tool.ValidateArguments(target, inner)
	return !result.Skipped && result.CompileErr == nil && len(result.Violations) == 0
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
	end := maxArgumentValidationMessageBytes - len("\n[truncated]")
	for end > 0 && !utf8.RuneStart(message[end]) {
		end--
	}
	return message[:end] + "\n[truncated]"
}
