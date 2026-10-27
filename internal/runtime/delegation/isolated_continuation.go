package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

type isolatedRunKey struct{}
type isolatedRunBinding struct{ ID, Grant, Source, Worktree string }

func (t *TaskTool) isolatedSpecContext(ctx context.Context, spec ProfileExecSpec) (context.Context, error) {
	binding := isolatedBinding(ctx)
	if binding.ID == "" {
		return ctx, nil
	}
	claim, err := relocateClaim(spec.Grant.WritePaths, binding.Worktree, binding.Source)
	if err != nil {
		return ctx, err
	}
	spec.Grant.WritePaths = claim
	binding.Grant = isolationGrant(spec)
	return context.WithValue(ctx, isolatedRunKey{}, binding), nil
}

func isolatedBinding(ctx context.Context) isolatedRunBinding {
	binding, _ := ctx.Value(isolatedRunKey{}).(isolatedRunBinding)
	return binding
}

func isolationGrant(spec ProfileExecSpec) string {
	data, _ := json.Marshal(spec.Grant.WritePaths)
	return bytesHash(data)
}

func (t *TaskTool) isolatedContinuation(ctx context.Context, spec *ProfileExecSpec) (sessionstore.SubagentMeta, error) {
	ref := firstNonEmpty(spec.Context.ContinueFrom, spec.Context.ForkFrom)
	if ref == "" {
		return sessionstore.SubagentMeta{}, nil
	}
	if agent.ParentSession(ctx) == "" {
		return sessionstore.SubagentMeta{}, fmt.Errorf("continue_from requires a persisted session")
	}
	if t.transcripts == nil {
		return sessionstore.SubagentMeta{}, fmt.Errorf("continue_from requires a transcript store")
	}
	meta, err := t.transcripts.LoadMeta(ref)
	if err != nil {
		return meta, err
	}
	if meta.IsolationRef == "" {
		return meta, nil
	}
	if spec.Context.ForkFrom != "" {
		return meta, fmt.Errorf("fork_from cannot copy an isolated worktree; use continue_from")
	}
	if err := validateContinueOwner(meta, SubagentSpec{ParentSession: agent.ParentSession(ctx)}); err != nil {
		return meta, err
	}
	if meta.WorkspaceRoot != firstNonEmpty(t.transcriptRoot, t.workspaceRoot) {
		return meta, fmt.Errorf("isolated continuation belongs to another workspace")
	}
	if binding := isolatedBinding(ctx); binding.ID == meta.IsolationRef {
		if binding.Grant != meta.IsolationGrant {
			return meta, fmt.Errorf("isolated continuation write scope changed")
		}
		return meta, nil
	}
	if spec.Sched.Isolation != "" && spec.Sched.Isolation != isolationMode {
		return meta, tool.Refusal{Code: codeIsolationUnsupported, Message: "isolated continuation requires worktree isolation"}
	}
	if isolationGrant(*spec) != meta.IsolationGrant {
		return meta, fmt.Errorf("isolated continuation requires its original write_paths")
	}
	spec.Sched.Isolation = isolationMode
	return meta, nil
}
