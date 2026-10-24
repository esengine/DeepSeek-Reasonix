package agent

import (
	"context"

	"reasonix/internal/base/diff"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
)

func (a *Agent) previewForApproval(ctx context.Context, plan *toolCallPlan) {
	target, args := plan.executionTarget()
	if _, ok := target.(tool.Previewer); !ok {
		return
	}
	change, ok := tool.PreviewChange(ctx, target, args)
	if !ok {
		return
	}
	if a.svc.mutations != nil {
		plan.approvedChange = &change
	}
	a.publishToolPreview(ctx, plan, change)
}

func (a *Agent) authorizeMutationPaths(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	if a.svc.mutations == nil {
		return toolOutcome{}, false
	}
	if writer, ok := plan.runTool.(tool.WriteAuthorizer); ok {
		if err := writer.AuthorizeWrite(ctx, plan.runArgs); err != nil {
			return toolOutcome{output: err.Error(), errMsg: err.Error(), blocked: true}, true
		}
	}
	return toolOutcome{}, false
}

func (a *Agent) revalidateMutationPreview(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	if a.svc.mutations == nil || plan.approvedChange == nil {
		return toolOutcome{}, false
	}
	change, ok := tool.PreviewChange(ctx, plan.runTool, plan.runArgs)
	if ok && change == *plan.approvedChange {
		return toolOutcome{}, false
	}
	if ok {
		a.publishToolPreview(ctx, plan, change)
	}
	err := &mutation.VersionConflict{Path: plan.approvedChange.Path}
	return toolOutcome{output: err.Error() + "; retry with the refreshed preview", errMsg: err.Error(), blocked: true, refusalCode: err.RefusalCode()}, true
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
