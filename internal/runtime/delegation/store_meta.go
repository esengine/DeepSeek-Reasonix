package delegation

import (
	"context"
	"strings"
	"time"

	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/memory"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/jobs"
)

func metaFromSpec(ref string, status sessionstore.SubagentStatus, created, updated time.Time, spec SubagentSpec) sessionstore.SubagentMeta {
	scope, schemaHash := toolIdentity(spec.Registry, spec.ToolContext)
	capsule := sessionstore.ContextCapsule{
		WorkspaceRoot:      strings.TrimSpace(spec.WorkspaceRoot),
		SystemPromptSource: strings.TrimSpace(spec.Kind),
		SystemPromptHash:   bytesHash([]byte(spec.SystemPrompt)),
		ToolScope:          scope,
		ToolSchemaHash:     schemaHash,
		Model:              strings.TrimSpace(spec.Model),
		Effort:             strings.TrimSpace(spec.Effort),
		ParentSession:      strings.TrimSpace(spec.ParentSession),
		ParentToolCallID:   strings.TrimSpace(spec.ParentToolCallID),
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
		ParentTurn:       strings.TrimSpace(spec.ParentTurn),
		SystemPromptHash: capsule.SystemPromptHash,
		ToolScope:        capsule.ToolScope,
		ToolSchemaHash:   capsule.ToolSchemaHash,
		Model:            capsule.Model,
		Effort:           capsule.Effort,
		Capsule:          capsule,
		CapsuleHash:      capsule.Hash(),
	}
}

const DefaultReadOnlyTaskSystemPrompt = `You are a read-only research sub-agent invoked by a parent coding agent.
Use only the provided read-only tools to inspect code, docs, history, and safe shell output.
Return a concise, self-contained final answer with the evidence the parent needs.`

func childToolIdentityContext(ctx context.Context) context.Context {
	ctx = tool.WithoutGoalTurnRecorder(ctx)
	ctx = memory.WithoutQueue(ctx)
	ctx = jobs.WithoutManager(ctx)
	return planmode.WithActive(ctx, agent.PlanModeFromContext(ctx))
}

type liveRun struct {
	sess *sessionstore.Session
}
