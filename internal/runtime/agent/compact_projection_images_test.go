package agent

import (
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

// The projection a fold stores keeps an image the host could not read back as
// its host record, not as the note a request renders: stored as text, the image
// could never be restored once its blob came back.
func TestCompressionProjectionKeepsUnavailableImagesStructured(t *testing.T) {
	lost := provider.UnavailableImage{Index: 0, Ref: "sha256:" + strings.Repeat("a", 64)}
	visible := []provider.Message{
		{Role: provider.RoleUser, Content: "old question"},
		{Role: provider.RoleAssistant, Content: "old answer"},
		{Role: provider.RoleUser, Content: "look", UnavailableImages: []provider.UnavailableImage{lost}},
	}
	plan := visibleCompressionPlan{foldMask: []bool{true, true, false}, firstFold: 0}
	projection := buildVisibleCompressionProjection(visible, plan, "summary")
	kept := projection[len(projection)-1]
	if kept.Content != "look" || len(kept.UnavailableImages) != 1 || kept.UnavailableImages[0] != lost {
		t.Fatalf("stored projection message = %+v, want the host record kept and the text untouched", kept)
	}
	if got := provider.ModelMessages(projection); !strings.Contains(got[len(got)-1].Content, "image(s) unavailable") {
		t.Fatal("the request built from the stored projection no longer says the image is gone")
	}
}
