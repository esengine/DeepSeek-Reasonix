package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
)

func (b foregroundOnlyBash) DescribeMutation(ctx context.Context, args json.RawMessage) (tool.MutationImpact, error) {
	if describe, ok := b.inner.(tool.MutationDescriber); ok {
		return describe.DescribeMutation(ctx, args)
	}
	return tool.MutationImpact{Kind: tool.MutationWorkspace}, nil
}

func (w pathBoundWriter) DescribeMutation(ctx context.Context, args json.RawMessage) (tool.MutationImpact, error) {
	if describe, ok := w.inner.(tool.MutationDescriber); ok {
		return describe.DescribeMutation(ctx, args)
	}
	paths, err := w.WritePaths(args)
	return tool.MutationImpact{Kind: tool.MutationNamedPaths, Paths: paths}, err
}

func describeMutation(ctx context.Context, plan *toolCallPlan) (tool.MutationImpact, error) {
	if describe, ok := plan.runTool.(tool.MutationDescriber); ok {
		return describe.DescribeMutation(ctx, plan.runArgs)
	}
	if plan.readOnly || tool.ReachOf(plan.runTool) == tool.ReachHostControl {
		return tool.MutationImpact{Kind: tool.MutationReadOnly}, nil
	}
	if len(plan.declaredPaths) > 0 {
		return tool.MutationImpact{Kind: tool.MutationNamedPaths, Paths: plan.declaredPaths}, nil
	}
	if writer, ok := plan.runTool.(tool.WritePathResolver); ok && tool.WritesNamedPaths(plan.runTool) {
		paths, err := writer.WritePaths(plan.runArgs)
		return tool.MutationImpact{Kind: tool.MutationNamedPaths, Paths: paths}, err
	}
	return tool.MutationImpact{Kind: tool.MutationWorkspace}, nil
}

func (a *Agent) mutationIntent(ctx context.Context, plan *toolCallPlan) (mutation.Intent, error) {
	impact, err := describeMutation(ctx, plan)
	if err != nil {
		return mutation.Intent{}, err
	}
	intent := mutation.Intent{Effect: mutation.Effect(impact.Kind), Paths: append(append([]string(nil), impact.Paths...), impact.SharedResources...)}
	switch impact.Kind {
	case tool.MutationReadOnly, tool.MutationExternal:
		if len(intent.Paths) != 0 {
			return mutation.Intent{}, mutation.ErrInvalidIntent
		}
	case tool.MutationNamedPaths, tool.MutationGitMetadata:
		if len(intent.Paths) == 0 {
			return mutation.Intent{}, mutation.ErrInvalidIntent
		}
	case tool.MutationWorkspace:
		intent.Paths = append(intent.Paths, a.extraRoots...)
	default:
		return mutation.Intent{}, fmt.Errorf("%w: unknown tool mutation kind", mutation.ErrInvalidIntent)
	}
	if impact.Kind != tool.MutationReadOnly && impact.Kind != tool.MutationExternal && toolHooksMayMutateWorkspace(a.svc.hooks) {
		intent.Effect = mutation.Workspace
		intent.Paths = append(intent.Paths, a.extraRoots...)
	}
	return intent, nil
}
