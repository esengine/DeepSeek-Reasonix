package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// System prompt provenance values recorded in a capsule.
const (
	SystemPromptTaskDefault     = "task-default"
	SystemPromptReadOnlyDefault = "readonly-default"
	SystemPromptProfilePrefix   = "profile:"
)

// InheritedContext states, item by item, what a child received from its parent.
// Every field is false today: children are isolated by construction, and this
// record exists so that stays a decision rather than an accident.
type InheritedContext struct {
	StandingInstructions bool `json:"standingInstructions"`
	Memory               bool `json:"memory"`
	ParentConversation   bool `json:"parentConversation"`
	Goal                 bool `json:"goal"`
	PlannerOutput        bool `json:"plannerOutput"`
}

// ContextCapsule is the immutable manifest of what one child run was actually
// given. It holds references and digests, never copied parent context, so a
// later question — why did this reviewer not see that constraint? — is answered
// from the record instead of guessed from logs.
type ContextCapsule struct {
	WorkspaceRoot      string           `json:"workspaceRoot,omitempty"`
	SystemPromptSource string           `json:"systemPromptSource"`
	SystemPromptHash   string           `json:"systemPromptHash"`
	ToolScope          []string         `json:"toolScope,omitempty"`
	ToolSchemaHash     string           `json:"toolSchemaHash,omitempty"`
	Model              string           `json:"model,omitempty"`
	Effort             string           `json:"effort,omitempty"`
	ParentSession      string           `json:"parentSession,omitempty"`
	ParentToolCallID   string           `json:"parentToolCallId,omitempty"`
	ResumedFrom        string           `json:"resumedFrom,omitempty"`
	Inherited          InheritedContext `json:"inherited"`
}

// Hash is the stable identity of a capsule. Two children with the same hash saw
// the same context; a hash that moves between runs is the thing to explain.
func (c ContextCapsule) Hash() string {
	encoded, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

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
func metaFromSpec(ref string, status SubagentStatus, created, updated time.Time, spec SubagentSpec) SubagentMeta {
	scope, schemaHash := toolIdentity(spec.Registry, spec.ToolContext)
	capsule := ContextCapsule{
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
	}
	return SubagentMeta{
		Ref:              ref,
		CreatedAt:        created,
		UpdatedAt:        updated,
		Status:           status,
		Kind:             strings.TrimSpace(spec.Kind),
		Name:             strings.TrimSpace(spec.Name),
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
