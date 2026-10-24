package control

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"reasonix/internal/agent"
	"reasonix/internal/attachment"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

// imageReadError distinguishes local attachment loss from upload/auth failures.
type imageReadError struct{ error }

func (e imageReadError) Unwrap() error { return e.error }

func lastImageRequestTurn(messages []provider.Message) int {
	for i, m := range slices.Backward(messages) {
		if m.Role == provider.RoleUser && !m.LocalOnly && !agent.IsHostGeneratedUserMessage(m) {
			return i
		}
	}
	return 0 // no known user boundary: keep all inputs required
}

// Resolve independently so a missing old attachment does not discard its
// healthy siblings. The durable references and UI transcript remain intact.
func (c *Controller) resolveReplayImages(ctx context.Context, inputs []attachment.ImageInput, route ImageRequestRoute, historical bool) ([]string, []int, error) {
	var images []string
	var missing []int
	for i, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		resolved, err := c.resolveImageInputsForRoute(ctx, []attachment.ImageInput{input}, route)
		if err == nil {
			images = append(images, resolved...)
			continue
		}
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return nil, nil, err
		}
		if historical {
			missing = append(missing, i+1)
			c.emitImageReplayWarning(err)
			continue
		}
		var item attachment.Error
		if errors.As(err, &item) {
			item.Index = i + 1
			return nil, nil, item
		}
		return nil, nil, err
	}
	return images, missing, nil
}

func noteUnavailableImages(m *provider.Message, positions []int) {
	if len(positions) == 0 {
		return
	}
	m.Content += fmt.Sprintf("\n[Historical image inputs %v are unavailable. Their contents have not been supplied in this request. Continue with available text and images; if those missing images are needed, ask the user to attach them again. Do not infer their contents.]", positions)
}

func noteUnreadableImages(m *provider.Message, count int) {
	positions := make([]int, count)
	for i := range positions {
		positions[i] = i + 1
	}
	m.Content += fmt.Sprintf("\n[Image inputs %v were not supplied: the selected model cannot read images and no image understanding model is configured. Do not infer their contents; tell the user to pick a model that reads images or set an image understanding model.]", positions)
}

func imageRequestFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("%s: %w", i18n.M.ImageRequestRecovery, &provider.ImageInputError{Err: err})
}
