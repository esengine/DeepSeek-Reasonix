package evidence

import (
	"encoding/json"

	"reasonix/internal/shellsafe"
)

// ToolEffects projects shell effects onto policy and evidence boundaries.
type ToolEffects struct {
	StateMutation, WorkspaceMutation, ContentMutation, RepositoryMutation bool
	Known                                                                 bool
	Reason                                                                string
}

// ClassifyToolCall returns durable effects for one concrete invocation.
func ClassifyToolCall(toolName string, args json.RawMessage, readOnly bool) ToolEffects {
	return ClassifyEffect(EffectInput{
		ToolName:       toolName,
		Args:           args,
		StaticReadOnly: readOnly,
	}).ToolEffects()
}

// ClassifyBashToolCall parses once and returns effects plus permission trust.
func ClassifyBashToolCall(args json.RawMessage) (ToolEffects, bool) {
	profile := ClassifyEffect(EffectInput{ToolName: "bash", Args: args})
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return profile.ToolEffects(), false
	}
	effect := shellsafe.ClassifyBash(stringField(fields, "command"))
	return profile.ToolEffects(), effect.IsPermissionReader()
}

func commandEffectReason(effect shellsafe.CommandEffect) string {
	domain := ""
	switch {
	case effect.Writes&shellsafe.WriteWorkspaceContent != 0:
		domain = "workspace content write"
	case effect.Writes&shellsafe.WriteRepositoryMetadata != 0:
		domain = "repository metadata write"
	case effect.Writes&shellsafe.WriteHostState != 0:
		domain = "host state write"
	case effect.Writes&shellsafe.WriteExternalState != 0:
		domain = "external state write"
	}
	if domain == "" {
		return effect.Reason
	}
	if effect.CommandFamily == "" {
		return domain
	}
	return domain + " by " + effect.CommandFamily
}

// ToolCallMutates is the compatibility projection for durable state changes.
func ToolCallMutates(toolName string, args json.RawMessage, readOnly bool) bool {
	return ClassifyToolCall(toolName, args, readOnly).StateMutation
}
