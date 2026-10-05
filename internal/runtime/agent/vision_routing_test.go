package agent

import (
	"context"
	"testing"
)

// readsOnly answers for one vision-capable ref, standing in for the config
// lookup the controller injects.
func readsOnly(visionRef string) func(string) bool {
	return func(ref string) bool { return ref == visionRef }
}

func withImage(ctx context.Context) context.Context {
	return WithSubagentImageCandidates(ctx, []string{"data:image/png;base64,AAAA"})
}

// Without a configured role an attachment keeps its current fate, so nothing
// silently moves off the model it was chosen for.
func TestVisionRefLeavesTheChildAloneWhenNoRoleIsSet(t *testing.T) {
	ctx := withImage(WithVisionRouting(context.Background(), "", readsOnly("gw/looker")))
	if got := VisionRefFor(ctx, "gw/worker"); got != "gw/worker" {
		t.Fatalf("model ref = %q, want the child's own model", got)
	}
}

func TestVisionRefMovesOnlyTheChildThatWouldDropTheImage(t *testing.T) {
	ctx := withImage(WithVisionRouting(context.Background(), "gw/looker", readsOnly("gw/looker")))

	// A text-only child would drop the attachment during serialization.
	if got := VisionRefFor(ctx, "gw/worker"); got != "gw/looker" {
		t.Fatalf("model ref = %q, want the vision role", got)
	}
	// A child that already reads images keeps the model it was chosen for.
	if got := VisionRefFor(ctx, "gw/looker"); got != "gw/looker" {
		t.Fatalf("model ref = %q, want the child's own vision model", got)
	}
	// An inherited ref is resolved by the predicate, not assumed capable.
	if got := VisionRefFor(ctx, ""); got != "gw/looker" {
		t.Fatalf("inherited text-only ref = %q, want the vision role", got)
	}
}

// A turn carrying no attachment is not an occasion to change models. Without
// this, a review sub-agent would be moved onto the vision model for every task
// in a session that once had an image.
func TestVisionRefIgnoresTurnsWithoutImages(t *testing.T) {
	ctx := WithVisionRouting(context.Background(), "gw/looker", readsOnly("gw/looker"))
	if got := VisionRefFor(ctx, "gw/worker"); got != "gw/worker" {
		t.Fatalf("model ref = %q on an image-free turn, want the child's own model", got)
	}
}
