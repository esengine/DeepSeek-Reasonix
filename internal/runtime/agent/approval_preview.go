package agent

import (
	"context"

	"reasonix/internal/base/diff"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

func (a *Agent) previewForApproval(ctx context.Context, plan *toolCallPlan) {
	target, args := plan.executionTarget()
	if _, ok := target.(tool.Previewer); !ok {
		return
	}
	change, _ := tool.PreviewChange(ctx, target, args)
	a.publishToolPreview(ctx, plan, change)
}

func (a *Agent) publishToolPreview(ctx context.Context, plan *toolCallPlan, change diff.Change) {
	preview := event.FileDiff{Diff: change.Diff, Added: change.Added, Removed: change.Removed}
	if plan.preview != nil && *plan.preview == preview {
		return
	}
	plan.preview = &preview
	plan.call.Diff, plan.call.Added, plan.call.Removed = change.Diff, change.Added, change.Removed
	a.sess.conversation.UpdateToolCallPreview(plan.call)
	a.emitFullToolDispatch(ctx, plan.call, true)
}
