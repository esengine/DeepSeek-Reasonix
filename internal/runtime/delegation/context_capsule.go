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
	if spec.IsolationRef != "" {
		// Older readers must refuse continuation outside the bound worktree.
		schemaHash = bytesHash([]byte(schemaHash + "\x00" + spec.IsolationRef + "\x00" + spec.IsolationGrant))
	}
	if spec.BudgetScope != "" {
		schemaHash = bytesHash([]byte(schemaHash + "\x00budget:" + spec.BudgetScope))
	}
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
		Inherited:          sessionstore.InheritedContext{UpstreamFrom: spec.UpstreamFrom, ResultRefs: spec.ResultRefs},
	}
	return sessionstore.SubagentMeta{
		BudgetScope:      spec.BudgetScope,
		IsolationRef:     spec.IsolationRef,
		IsolationGrant:   spec.IsolationGrant,
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
