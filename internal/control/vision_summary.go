package control

import (
	"context"

	"reasonix/internal/agent"
	"reasonix/internal/imageinput"
	"reasonix/internal/provider"
)

// prepareVisionTurn shares the same per-session processor as tool results.
func (c *Controller) prepareVisionTurn(ctx context.Context, input string, images []string) (string, context.Context, error) {
	prepared, _ := ctx.Value(preparedImageReferencesContextKey{}).(preparedImageReferences)
	if c == nil || (len(images) == 0 && len(prepared.inputs) == 0) || c.imageInputEnabled() {
		return input, ctx, nil
	}
	if c.visionModel == "" && !prepared.requiresImageUnderstanding {
		// Keep the frozen bytes available to vision-capable child agents while
		// withholding them from the text-only parent provider request.
		return input, agent.WithUserImageInputs(ctx, nil), nil
	}
	var svc *imageinput.Service
	var history func() []provider.Message
	if c.executor != nil {
		svc = c.executor.ImageInput()
		history = c.executor.Session().Snapshot
	}
	if svc == nil {
		svc = imageinput.New(imageinput.Config{Model: c.visionModel, Resolve: c.visionProviderResolver, Select: c.visionModelSelector})
	}
	target, err := svc.SelectModel(c.selection.ref, images)
	if err != nil {
		return input, ctx, imageRequestFailure(ctx, err)
	}
	if len(prepared.inputs) > 0 {
		route, routeErr := c.imageRequestRoute(target)
		if routeErr != nil {
			return input, ctx, imageRequestFailure(ctx, routeErr)
		}
		images, err = c.resolveImageInputsForRoute(ctx, prepared.inputs, route)
		if err != nil {
			return input, ctx, imageRequestFailure(ctx, err)
		}
	}
	summary, err := svc.UnderstandSelected(ctx, target, images, history, c.sink)
	if err != nil {
		return input, ctx, imageRequestFailure(ctx, err)
	}
	return imageinput.AppendSummary(input, summary), agent.WithVisionSummary(ctx, summary), nil
}
