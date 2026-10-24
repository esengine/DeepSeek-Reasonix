package delegation

import (
	"context"
	"fmt"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"strings"
)

func (t *TaskTool) prepareTranscriptRunWithPrompt(ctx context.Context, subReg *tool.Registry, modelRef, effortRef, parentSession, execution, parentID, continueFrom, legacyForkFrom, systemPrompt, kind, name string, source *EnvironmentSource) (run *SubagentRun, err error) {
	defer func() {
		if err == nil {
			if bindErr := agent.BindChildComposition(ctx, run.Session, false); bindErr != nil {
				run.Release()
				run = nil
				err = bindErr
			}
		}
	}()
	continueFrom = strings.TrimSpace(continueFrom)
	legacyForkFrom = strings.TrimSpace(legacyForkFrom)
	parentSession = strings.TrimSpace(parentSession)
	if continueFrom != "" && legacyForkFrom != "" {
		return nil, fmt.Errorf("continue_from and fork_from are mutually exclusive; pass only continue_from")
	}
	if t.transcripts == nil {
		return nil, fmt.Errorf("subagent transcript store is required")
	}
	if systemPrompt == "" {
		systemPrompt = t.sysPrompt
	}
	systemPrompt = agent.ChildCompositionPrompt(ctx, systemPrompt)
	kind = firstNonEmpty(kind, "task")
	name = firstNonEmpty(name, "task")
	if parentSession == "" {
		if continueFrom != "" || legacyForkFrom != "" {
			return nil, fmt.Errorf("subagent continuation requires a persisted session; none is active in this run")
		}
		return EphemeralSubagentRun(systemPrompt), nil
	}
	identityModel, identityEffort := t.effectiveIdentity(modelRef, effortRef)
	spec := SubagentSpec{
		Kind:                  kind,
		Name:                  name,
		WorkspaceRoot:         t.workspaceRoot,
		ParentSession:         parentSession,
		ExecutionID:           execution,
		EnvironmentID:         t.environmentID,
		EnvironmentGeneration: t.environmentGeneration,
		OriginWorkspace:       t.environmentSource,
		ParentToolCallID:      parentID,
		SystemPrompt:          systemPrompt,
		Registry:              subReg,
		ToolContext:           childToolIdentityContext(ctx),
		Model:                 identityModel,
		Effort:                identityEffort,
		ResumedFrom:           firstNonEmpty(continueFrom, legacyForkFrom),
		UpstreamFrom:          upstreamSources(upstreamFromContext(ctx)),
		continuationSource:    source,
	}
	if continueFrom != "" {
		return t.transcripts.PrepareContinue(continueFrom, spec)
	}
	if legacyForkFrom != "" {
		return t.transcripts.PrepareLegacyForkFrom(legacyForkFrom, spec)
	}
	return t.transcripts.PrepareFresh(spec)
}
