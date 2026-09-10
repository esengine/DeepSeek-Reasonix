package control

import (
	"context"
	"fmt"

	"reasonix/internal/agent"
	"reasonix/internal/imageinput"
	"reasonix/internal/provider"
)

// prepareVisionTurn shares the same per-session processor as tool results.
func (c *Controller) prepareVisionTurn(ctx context.Context, input string, images []string) (string, context.Context, error) {
	if c == nil || len(images) == 0 || c.imageInputEnabled() || c.visionModel == "" {
		return input, ctx, nil
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
	summary, err := svc.Understand(ctx, c.selection.ref, images, history, c.sink)
	if err != nil {
		return input, ctx, fmt.Errorf("图片理解失败，当前回答尚未发送：%w", err)
	}
	return imageinput.AppendSummary(input, summary), agent.WithVisionSummary(ctx, summary), nil
}
