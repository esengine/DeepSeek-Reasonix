package main

import (
	"strings"
	"testing"

	"reasonix/internal/attachment"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncontent"
)

func TestHistoryMessagesIncludeSessionAttachments(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	msgs := []provider.Message{{
		Role:    provider.RoleUser,
		Content: "look",
		ImageInputs: []attachment.ImageInput{{
			Kind: attachment.KindAttachment,
			Attachment: &attachment.AttachmentRef{
				Version:     attachment.RefVersion,
				Content:     sessioncontent.Ref{Digest: digest, Bytes: 12, MediaType: "image/png", Name: "shot.png"},
				Width:       8,
				Height:      8,
				DisplayName: "shot.png",
			},
		}},
	}}
	got := historyMessages(msgs, historyReplayUserContent)
	if len(got) != 1 || len(got[0].Attachments) != 1 {
		t.Fatalf("history attachments = %+v", got)
	}
	att := got[0].Attachments[0]
	if att.Kind != "image" || att.Digest != digest || att.Name != "shot.png" || att.MIME != "image/png" {
		t.Fatalf("attachment = %+v", att)
	}
}
