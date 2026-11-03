package control

import (
	"context"
	"strings"

	"reasonix/internal/state/sessioninbox"
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
	images, skipped := c.resolveInputImageCandidates(input)
	for _, err := range skipped {
		errs = append(errs, err.Error())
	}
	return block, images, errs
}

const foreignImagesBlock = "queued with images by another version of Reasonix; this build cannot read them, so the line was held instead of being sent without them"

func applyInboxReferences(env sessioninbox.PromptEnvelope) (submit string, images []string, blockReason string, err error) {
	if len(env.ReferenceErrors) > 0 {
		return "", nil, strings.Join(env.ReferenceErrors, "; "), nil
	}
	if env.HasForeignImages() {
		return "", nil, foreignImagesBlock, nil
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

func blockCodeFor(env sessioninbox.PromptEnvelope) sessioninbox.BlockCode {
	if env.HasForeignImages() {
		return sessioninbox.BlockForeignImages
	}
	return ""
}
