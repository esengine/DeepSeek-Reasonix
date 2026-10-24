package delegation

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/contract/tool"
)

func (t *TaskTool) resumeEnvironment(ctx context.Context, spec ProfileExecSpec) (*EnvironmentSource, error) {
	if strings.TrimSpace(spec.Context.ContinueFrom) != "" && strings.TrimSpace(spec.Context.ForkFrom) != "" {
		return nil, fmt.Errorf("continue_from and fork_from are mutually exclusive; pass only continue_from")
	}
	ref := firstNonEmpty(spec.Context.ContinueFrom, spec.Context.ForkFrom)
	if ref == "" || t.transcripts == nil {
		return nil, nil
	}
	if spec.Context.parentSession(ctx) == "" {
		return nil, fmt.Errorf("subagent continuation requires a persisted session; none is active in this run")
	}
	meta, err := t.transcripts.LoadMeta(ref)
	if err != nil {
		return nil, err
	}
	parent := strings.TrimSpace(spec.Context.parentSession(ctx))
	if parent == "" {
		return nil, tool.Refusal{Code: "isolation.unsupported_arg", Message: "isolated continuation requires its owning session"}
	}
	if spec.Context.ForkFrom != "" && strings.TrimSpace(meta.ParentSession) == parent {
		return nil, fmt.Errorf("fork_from cannot be safely converted for subagent reference %q in the current conversation; use continue_from to continue it in place or start a fresh subagent for independent work", ref)
	}
	if !transcriptInWorkspace(meta, firstNonEmpty(t.environmentSource, t.workspaceRoot)) {
		return nil, tool.Refusal{Code: "isolation.unsupported_arg", Message: "isolated transcript belongs to a different workspace"}
	}
	if meta.EnvironmentID != "" && spec.Sched.Environment == "shared" {
		return nil, tool.Refusal{Code: "isolation.unsupported_arg", Message: "continue this isolated task in its retained environment; a shared continuation is unavailable"}
	}
	return t.transcripts.continuationSource(ctx, ref, parent)
}
