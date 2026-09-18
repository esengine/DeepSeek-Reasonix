package provider

import (
	"testing"

	"reasonix/internal/attachment"
	"reasonix/internal/sessioncontent"
)

func TestMessageRejectsCombinedImageFields(t *testing.T) {
	msg := Message{
		Images: []string{"data:image/png;base64,AA=="},
		ImageInputs: []attachment.ImageInput{{
			Kind: attachment.KindAttachment,
			Attachment: &attachment.AttachmentRef{
				Version: attachment.RefVersion,
				Content: sessioncontent.Ref{Digest: "aa", Bytes: 1, MediaType: "image/png"},
				Width:   1, Height: 1,
			},
		}},
	}
	if err := msg.ValidateImageFields(); err == nil {
		t.Fatal("expected combined image fields to be rejected")
	}
}

func TestLegacyImagesRemainValid(t *testing.T) {
	msg := Message{Images: []string{"https://cdn.example.com/a.png"}}
	if err := msg.ValidateImageFields(); err != nil {
		t.Fatal(err)
	}
	if !msg.HasImagePayload() {
		t.Fatal("legacy images should count as a payload")
	}
}
