package evidence

import (
	"encoding/json"
	"strings"

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
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "bash" || name == "shell" {
		effects, _ := ClassifyBashToolCall(args)
		return effects
	}
	if readOnly || IsNonMutationMetaTool(toolName) {
		return ToolEffects{Known: true}
	}
	switch name {
	case "ask", "todo_write", "complete_step", "bash_output", "wait":
		return ToolEffects{Known: true}
	default:
		return ToolEffects{StateMutation: true, WorkspaceMutation: true, ContentMutation: true, Known: true, Reason: "workspace content write by a writer-capable tool"}
	}
}

// ClassifyBashToolCall parses once and returns effects plus permission trust.
func ClassifyBashToolCall(args json.RawMessage) (ToolEffects, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return unknownToolEffects("invalid shell arguments"), false
	}
	command := stringField(fields, "command")
	effect := shellsafe.ClassifyBash(command)
	if effect.Certainty == shellsafe.EffectKnown {
		return ToolEffects{
			StateMutation: effect.AnyMutation(), WorkspaceMutation: effect.WorkspaceMutation(),
			ContentMutation: effect.ContentMutation(), RepositoryMutation: effect.RepositoryMutation(),
			Known: true, Reason: commandEffectReason(effect),
		}, effect.IsPermissionReader()
	}
	// Unknown syntax stays fail-closed unless the host recognized a verifier
	// such as `go test` that ClassifyBash does not model as a known reader.
	if bashCommandIsVerification(command) {
		return ToolEffects{Known: true}, false
	}
	return unknownToolEffects(effect.Reason), false
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

func unknownToolEffects(reason string) ToolEffects {
	if strings.TrimSpace(reason) == "" {
		reason = "tool effects are not statically known"
	}
	return ToolEffects{StateMutation: true, WorkspaceMutation: true, ContentMutation: true, Reason: reason}
}

// ToolCallMutates is the compatibility projection for durable state changes.
func ToolCallMutates(toolName string, args json.RawMessage, readOnly bool) bool {
	return ClassifyToolCall(toolName, args, readOnly).StateMutation
}
