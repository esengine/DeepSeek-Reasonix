package main

import (
	"reasonix/internal/attachment"
	"reasonix/internal/transcript"
)

func historyDisplayAttachments(inputs []attachment.ImageInput) []transcript.Attachment {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]transcript.Attachment, 0, len(inputs))
	for _, in := range inputs {
		switch in.Kind {
		case attachment.KindAttachment:
			if in.Attachment == nil {
				continue
			}
			out = append(out, transcript.Attachment{
				Kind:   "image",
				Digest: in.Attachment.Digest(),
				Name:   in.Attachment.DisplayName,
				MIME:   in.Attachment.MIME(),
				Width:  in.Attachment.Width,
				Height: in.Attachment.Height,
				Bytes:  in.Attachment.Content.Bytes,
			})
		case attachment.KindURL:
			if in.URL == "" {
				continue
			}
			out = append(out, transcript.Attachment{Kind: "url", Name: in.URL})
		case attachment.KindFiles:
			if in.FilesID == "" {
				continue
			}
			out = append(out, transcript.Attachment{Kind: "files", Name: in.FilesID})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
