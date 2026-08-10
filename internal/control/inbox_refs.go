package control

import (
	"context"
	"strings"

	"reasonix/internal/sessioninbox"
)

func (c *Controller) freezeInboxReferences(ctx context.Context, submit string, explicit []string) (string, []string, []string) {
	var line strings.Builder
	line.WriteString(submit)
	for _, path := range explicit {
		path = strings.TrimSpace(path)
		if path != "" {
			line.WriteString(" @")
			line.WriteString(EscapeRefPath(path))
		}
	}
	input := line.String()
	if !c.HasRefs(input) {
		return "", nil, nil
	}
	block, errs := c.ResolveRefs(ctx, input)
	images := c.resolveInputImageCandidates(input)
	return block, images, errs
}

func applyInboxReferences(env sessioninbox.PromptEnvelope) (submit string, images []string, blockReason string, err error) {
	if len(env.ReferenceErrors) > 0 {
		return "", nil, strings.Join(env.ReferenceErrors, "; "), nil
	}
	submit = env.SubmitText
	images = append([]string(nil), env.FrozenImages...)
	if env.FrozenRefBlock != "" {
		submit = "Referenced context:\n\n" + env.FrozenRefBlock + "\n\n" + submit
		return submit, images, "", nil
	}
	if len(env.Refs) == 0 {
		return submit, images, "", nil
	}
	legacyBlock, bodies, materializeErr := sessioninbox.MaterializeRefs(context.Background(), "", env.Refs)
	if materializeErr != nil || legacyBlock != "" {
		return "", nil, legacyBlock, materializeErr
	}
	return sessioninbox.ApplyFrozenRefs(submit, bodies), images, "", nil
}
