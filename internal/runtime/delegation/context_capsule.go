package delegation

import (
	"reasonix/internal/state/sessionstore"
	"strings"
	"time"
)

// System prompt provenance values recorded in a capsule.
const (
	SystemPromptTaskDefault     = "task-default"
	SystemPromptReadOnlyDefault = "readonly-default"
	SystemPromptProfilePrefix   = "profile:"
)

// systemPromptSource names where a child's system prompt came from without
// embedding the prompt itself.
func systemPromptSource(kind, name, systemPrompt string) string {
	if strings.TrimSpace(kind) == "skill" && strings.TrimSpace(name) != "" {
		return SystemPromptProfilePrefix + strings.TrimSpace(name)
	}
	if systemPrompt == DefaultReadOnlyTaskSystemPrompt {
		return SystemPromptReadOnlyDefault
	}
	return SystemPromptTaskDefault
}

// metaFromSpec assembles the sidecar record for one run: the execution identity
// continuation must match, plus the capsule describing the context it was given.
func metaFromSpec(ref string, status sessionstore.SubagentStatus, created, updated time.Time, spec SubagentSpec) sessionstore.SubagentMeta {
	scope, schemaHash := toolIdentity(spec.Registry, spec.ToolContext)
	capsule := sessionstore.ContextCapsule{
		WorkspaceRoot:      strings.TrimSpace(spec.WorkspaceRoot),
		SystemPromptSource: systemPromptSource(spec.Kind, spec.Name, spec.SystemPrompt),
		SystemPromptHash:   bytesHash([]byte(spec.SystemPrompt)),
		ToolScope:          scope,
		ToolSchemaHash:     schemaHash,
		Model:              strings.TrimSpace(spec.Model),
		Effort:             strings.TrimSpace(spec.Effort),
		ParentSession:      strings.TrimSpace(spec.ParentSession),
		ParentToolCallID:   strings.TrimSpace(spec.ParentToolCallID),
		ResumedFrom:        strings.TrimSpace(spec.ResumedFrom),
		Inherited:          sessionstore.InheritedContext{UpstreamFrom: spec.UpstreamFrom},
	}
	return sessionstore.SubagentMeta{
		Ref:              ref,
		CreatedAt:        created,
		UpdatedAt:        updated,
		Status:           status,
		Kind:             strings.TrimSpace(spec.Kind),
		Name:             strings.TrimSpace(spec.Name),
		ExecutionID:      strings.TrimSpace(spec.ExecutionID),
		WorkspaceRoot:    capsule.WorkspaceRoot,
		ParentSession:    capsule.ParentSession,
		ParentToolCallID: capsule.ParentToolCallID,
		SystemPromptHash: capsule.SystemPromptHash,
		ToolScope:        capsule.ToolScope,
		ToolSchemaHash:   capsule.ToolSchemaHash,
		Model:            capsule.Model,
		Effort:           capsule.Effort,
		Capsule:          capsule,
		CapsuleHash:      capsule.Hash(),
	}
}
