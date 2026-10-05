package provider

import (
	"fmt"
	"testing"
)

// An image the host could not read back is said in the request, never stored
// in the text, and a transcript without one reaches the provider untouched.
func TestModelMessagesRenderUnavailableImages(t *testing.T) {
	clean := []Message{{Role: RoleUser, Content: "look", Images: []string{"data:image/png;base64,AAAA"}}}
	if got := ModelMessages(clean); &got[0] != &clean[0] {
		t.Fatal("a transcript with nothing unavailable was copied")
	}
	lost := []Message{{Role: RoleTool, Content: "captured", UnavailableImages: []UnavailableImage{{Index: 0, Ref: "sha256:00"}, {Index: 2, Ref: "sha256:11"}}}}
	got := ModelMessages(lost)
	if want := "captured" + fmt.Sprintf(UnavailableImagesNote, 2); got[0].Content != want || got[0].UnavailableImages != nil {
		t.Fatalf("request message = %+v, want content %q and no host record", got[0], want)
	}
	if lost[0].Content != "captured" {
		t.Fatal("the stored message was rewritten")
	}
	if kept := ProjectionMessages(lost); len(kept[0].UnavailableImages) != 2 || kept[0].Content != "captured" {
		t.Fatalf("a stored projection lost the host record: %+v", kept[0])
	}
}
