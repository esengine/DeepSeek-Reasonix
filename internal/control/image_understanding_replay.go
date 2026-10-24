package control

import (
	"context"
	"errors"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/imageinput"
	"reasonix/internal/provider"
	"reasonix/internal/secrets"
)

func (c *Controller) understandReplayImages(ctx context.Context, m *provider.Message, model string, historical bool) error {
	if m.VisionSummary != nil {
		return nil
	}
	svc := imageinput.New(imageinput.Config{Model: c.visionModel, Resolve: c.visionProviderResolver, Select: c.visionModelSelector})
	if c.executor != nil && c.executor.ImageInput() != nil {
		svc = c.executor.ImageInput()
	}
	target, err := svc.SelectModel(model, nil)
	if errors.Is(err, imageinput.ErrNoModel) && (m.Role != provider.RoleUser || historical) {
		noteUnreadableImages(m, len(m.ImageInputs))
		return nil
	}
	if err != nil {
		return err
	}
	route, err := c.imageRequestRoute(target)
	if err != nil {
		return err
	}
	images, missing, err := c.resolveReplayImages(ctx, m.ImageInputs, route, historical || m.Role == provider.RoleTool)
	if err != nil {
		return err
	}
	if len(images) > 0 {
		summary, err := svc.UnderstandSelected(ctx, target, images, nil, c.sink)
		if err != nil {
			return err
		}
		m.Content = imageinput.AppendSummary(m.Content, summary)
	}
	noteUnavailableImages(m, missing)
	return nil
}

// Only the current user attachments are required. Older inputs and images
// produced by an already-executed tool must not poison every later text turn.
// A child's image timeout may degrade; cancellation of the owning turn may not.
func (c *Controller) omitReplayImageFailure(ctx context.Context, m *provider.Message, err error, historical bool) bool {
	if (!historical && m.Role != provider.RoleTool) || ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return false
	}
	positions := make([]int, len(m.ImageInputs))
	for i := range positions {
		positions[i] = i + 1
	}
	if m.Role == provider.RoleTool {
		m.Content += "\n[Tool image inputs were not supplied because image processing failed. The tool already executed; its text result remains valid. Do not infer image contents or repeat the original action to retry image processing.]"
	} else {
		noteUnavailableImages(m, positions)
	}
	m.ImageInputs, m.Images = nil, nil
	c.emitImageReplayWarning(err)
	return true
}

func (c *Controller) emitImageReplayWarning(err error) {
	if c.sink == nil {
		return
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: event.NoticeCodeImageReplayUnavailable,
		MessageID: agent.NewMessageID(), Text: i18n.M.ImageReplayUnavailable,
		Detail: secrets.FailureDetail(&provider.ImageInputError{Err: err})})
}
