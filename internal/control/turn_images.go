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
	if turn.imagesResolved {
		return turn.userImages, turn.imageCandidates
	}
	if turn.goalContinuation != nil {
		// A Goal continuation belongs to the same visible user turn, so keep its
		// child-only image candidates. Do not add them to the synthetic parent
		// message: a vision parent already has the image in its earlier history.
		return nil, agent.SubagentImageCandidates(ctx)
	}
	return c.resolveTurnImages(turn.imageReferenceInput())
}

func (c *Controller) withTurnImages(ctx context.Context, line string) context.Context {
	userImages, imageCandidates := c.resolveTurnImages(line)
	ctx = agent.WithUserImages(ctx, userImages)
	return agent.WithSubagentImageCandidates(ctx, imageCandidates)
}

func (turn orchestratedTurn) imageReferenceInput() string {
	if strings.TrimSpace(turn.imageRefs) != "" {
		return turn.imageRefs
	}
	return turn.raw
}

func (c *Controller) runGoalLoopWithImageRefsRawDisplay(ctx context.Context, input, raw, imageRefs, display string) error {
	return newTurnOrchestrator(c).runGoalLoopWithImageRefsRawDisplay(ctx, input, raw, imageRefs, display)
}

func (c *Controller) runGoalLoopWithFrozenImagesRawDisplay(ctx context.Context, input, raw, display string, images []string) error {
	return newTurnOrchestrator(c).runGoalLoopWithFrozenImagesRawDisplay(ctx, input, raw, display, images)
}

func (c *Controller) runEditedGoalLoopWithImageRefsRawDisplay(ctx context.Context, input, raw, imageRefs, display, original string) error {
	return newTurnOrchestrator(c).runEditedGoalLoopWithImageRefsRawDisplay(ctx, input, raw, imageRefs, display, original)
}
