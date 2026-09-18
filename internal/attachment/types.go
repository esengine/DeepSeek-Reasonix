package attachment

import "reasonix/internal/sessioncontent"

const (
	RefVersion = 1

	KindAttachment Kind = "attachment"
	KindURL        Kind = "url"
	KindFiles      Kind = "files"
)

// Kind selects one ImageInput arm. Empty is invalid.
type Kind string

// AttachmentRef is the durable identity of one verified original image.
// Object identity is the SHA-256 of the original bytes (Content.Digest).
type AttachmentRef struct {
	Version     int                `json:"v"`
	Content     sessioncontent.Ref `json:"content"`
	Width       int                `json:"width,omitempty"`
	Height      int                `json:"height,omitempty"`
	DisplayName string             `json:"name,omitempty"`
}

// ImageInput is an ordered union stored on new messages. A message must not
// combine this field with legacy Images []string.
type ImageInput struct {
	Kind       Kind           `json:"kind"`
	Attachment *AttachmentRef `json:"attachment,omitempty"`
	URL        string         `json:"url,omitempty"`
	FilesID    string         `json:"filesId,omitempty"`
}

// PreparedImage is one verified original, held only for the current submit.
type PreparedImage struct {
	DisplayName string
	MIME        string
	Width       int
	Height      int
	Bytes       []byte
	Existing    *AttachmentRef
}

// PreparedImages is an ordered, all-or-nothing batch. Commit publishes every
// object or none of the returned refs.
type PreparedImages struct {
	Items []PreparedImage
}

// DraftCredential is a host-issued handle. Clients cannot mint one from a digest.
type DraftCredential struct {
	ID          string        `json:"id"`
	Ref         AttachmentRef `json:"-"`
	DisplayName string        `json:"displayName"`
	MIME        string        `json:"mime"`
	Width       int           `json:"width"`
	Height      int           `json:"height"`
	Bytes       int64         `json:"bytes"`
}

// Variant is a deterministic request encoding of one original.
type Variant struct {
	Bytes         []byte
	MIME          string
	Width         int
	Height        int
	SourceWidth   int
	SourceHeight  int
	PolicyVersion int
	Digest        string
}

func (r AttachmentRef) Digest() string {
	return r.Content.Digest
}

func (r AttachmentRef) MIME() string {
	return r.Content.MediaType
}

func (r AttachmentRef) Validate() error {
	if r.Version != RefVersion {
		return Error{Code: CodeUnsupported, Message: "unsupported attachment reference version"}
	}
	if r.Content.Digest == "" || r.Content.Bytes <= 0 {
		return Error{Code: CodeCorrupt, Message: "attachment reference is incomplete"}
	}
	if r.Width <= 0 || r.Height <= 0 {
		return Error{Code: CodeCorrupt, Message: "attachment reference is missing dimensions"}
	}
	return nil
}

func (in ImageInput) Validate() error {
	switch in.Kind {
	case KindAttachment:
		if in.Attachment == nil || in.URL != "" || in.FilesID != "" {
			return Error{Code: CodeUnsupported, Message: "attachment image input is malformed"}
		}
		return in.Attachment.Validate()
	case KindURL:
		if in.Attachment != nil || in.FilesID != "" || in.URL == "" {
			return Error{Code: CodeUnsupported, Message: "url image input is malformed"}
		}
		return nil
	case KindFiles:
		if in.Attachment != nil || in.URL != "" || in.FilesID == "" {
			return Error{Code: CodeUnsupported, Message: "files image input is malformed"}
		}
		return nil
	default:
		return Error{Code: CodeUnsupported, Message: "unknown image input kind"}
	}
}

func CloneImageInputs(in []ImageInput) []ImageInput {
	if len(in) == 0 {
		return nil
	}
	out := make([]ImageInput, len(in))
	copy(out, in)
	for i := range out {
		if out[i].Attachment != nil {
			ref := *out[i].Attachment
			out[i].Attachment = &ref
		}
	}
	return out
}

func CollectContentRefs(inputs []ImageInput) []sessioncontent.Ref {
	var refs []sessioncontent.Ref
	for _, in := range inputs {
		if in.Kind == KindAttachment && in.Attachment != nil && in.Attachment.Content.Digest != "" {
			refs = append(refs, in.Attachment.Content)
		}
	}
	return refs
}
