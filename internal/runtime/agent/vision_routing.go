// vision_routing.go — which model reads an attachment the parent could not.
package agent

import (
	"context"
	"fmt"
	"strings"
)

type visionRoutingKey struct{}

// visionRouting travels with the image candidates because it answers the same
// question they raise: the parent could not read this, so who can.
type visionRouting struct {
	model  string
	effort string
	reads  func(modelRef string) bool
}

// WithVisionRouting binds the image-reading model and its effort. The predicate
// keeps a child that already reads images on its own model and effort.
func WithVisionRouting(ctx context.Context, model, effort string, reads func(modelRef string) bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return ctx
	}
	return context.WithValue(ctx, visionRoutingKey{}, visionRouting{model: model, effort: effort, reads: reads})
}

// VisionRefFor selects the vision role only when a child cannot read this turn's
// images. A child that already reads them keeps its own model.
func VisionRefFor(ctx context.Context, childRef string) string {
	if ctx == nil || len(SubagentImageCandidates(ctx)) == 0 {
		return childRef
	}
	routing, ok := ctx.Value(visionRoutingKey{}).(visionRouting)
	if !ok || routing.model == "" {
		return childRef
	}
	if routing.reads != nil && routing.reads(childRef) {
		return childRef
	}
	return routing.model
}

func VisionEffortFor(ctx context.Context, childRef, childEffort string) string {
	if VisionRefFor(ctx, childRef) == childRef {
		return childEffort
	}
	routing, _ := ctx.Value(visionRoutingKey{}).(visionRouting)
	if routing.effort != "" {
		return routing.effort
	}
	return "auto"
}

// SubagentImageNote tells a delegate that the pictures ride the message it is
// reading. Without it the child sees a path in its task and calls read_file,
// which reads text and refuses — the delegation exists precisely because
// something had to look at the image instead.
func SubagentImageNote(ctx context.Context) string {
	n := len(SubagentImageCandidates(ctx))
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(
		"<attached-images>\nThe %d image(s) this task is about are attached to this message. Look at them directly.\n"+
			"Never call read_file on an image and never hexdump one: read_file reads text and will refuse.\n"+
			"If you cannot see them at all, say so plainly instead of guessing what they show.\n</attached-images>\n\n",
		n)
}
