package control

import (
	"context"
	"strings"

	"reasonix/internal/agent"
)

// resolveTurnImages resolves each user attachment once. Text-only parents keep
// the data only as candidates for a vision-capable child; vision-capable
// parents reuse the same data URLs for their own provider request.
func (c *Controller) resolveTurnImages(line string) (userImages, imageCandidates []string) {
	imageCandidates = c.resolveInputImageCandidates(line)
	if c.imageInputEnabled() {
		userImages = imageCandidates
	}
	return userImages, imageCandidates
}

func (c *Controller) prepareOrchestratedTurnImages(turn orchestratedTurn) orchestratedTurn {
	turn.userImages, turn.imageCandidates = c.resolveTurnImages(turn.imageReferenceInput())
	turn.imagesResolved = true
	return turn
}

func (c *Controller) imagesForOrchestratedTurn(ctx context.Context, turn orchestratedTurn) (userImages, imageCandidates []string) {
	if prepared, ok := ctx.Value(preparedImageReferencesContextKey{}).(preparedImageReferences); ok && len(prepared.inputs) > 0 {
		return nil, nil
	}
	if turn.imagesResolved {
		return turn.userImages, turn.imageCandidates
	}
	return c.resolveTurnImages(turn.imageReferenceInput())
}

func (c *Controller) withTurnImages(ctx context.Context, line string) context.Context {
	prepared, _ := ctx.Value(preparedImageReferencesContextKey{}).(preparedImageReferences)
	if inputs := prepared.inputs; len(inputs) > 0 {
		ctx = agent.WithUserImageInputs(ctx, inputs)
		ctx = agent.WithSubagentImageInputs(ctx, inputs)
		return ctx
	}
	userImages, imageCandidates := c.resolveTurnImages(line)
	ctx = agent.WithUserImages(ctx, userImages)
	return agent.WithSubagentImageCandidates(ctx, imageCandidates)
}

func withPreparedTurnImages(ctx context.Context) context.Context {
	prepared, _ := ctx.Value(preparedImageReferencesContextKey{}).(preparedImageReferences)
	if len(prepared.inputs) == 0 {
		return ctx
	}
	ctx = agent.WithUserImages(ctx, nil)
	ctx = agent.WithUserImageInputs(ctx, prepared.inputs)
	return agent.WithSubagentImageInputs(ctx, prepared.inputs)
}

func (turn orchestratedTurn) imageReferenceInput() string {
	if strings.TrimSpace(turn.imageRefs) != "" {
		return turn.imageRefs
	}
	return turn.raw
}

func (c *Controller) runGoalLoopWithFrozenImagesRawDisplay(ctx context.Context, input, raw, display string, images []string) error {
	return newTurnOrchestrator(c).runGoalLoopWithFrozenImagesRawDisplay(ctx, input, raw, display, images)
}

func (c *Controller) runEditedGoalLoopWithFrozenImagesRawDisplay(ctx context.Context, input, raw, display, original string, images []string) error {
	return newTurnOrchestrator(c).runEditedGoalLoopWithFrozenImagesRawDisplay(ctx, input, raw, display, original, images)
}
