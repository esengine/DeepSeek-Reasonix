package eventwire

import "reasonix/internal/contract/event"

// Attachment is the JSON form of a stored image a message references.
type Attachment struct {
	Path string `json:"path"`
	// Ref is the opaque external-folder token an in-place external image arrived
	// as, present only for images the session was handed outside the workspace.
	Ref string `json:"ref,omitempty"`
}

// ToWireAttachments converts the host's resolution of a message's attachments.
func ToWireAttachments(in []event.Attachment) []Attachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]Attachment, len(in))
	for i, a := range in {
		out[i] = Attachment{Path: a.Path, Ref: a.Ref}
	}
	return out
}
