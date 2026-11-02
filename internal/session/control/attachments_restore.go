package control

import (
	"path"
	"path/filepath"
	"strings"
)

// RestoredAttachment answers one clipboard path: the reference the composer can
// put back, or why this workspace cannot. Absent Ref is the failure, never an
// empty token the line would carry as text.
type RestoredAttachment struct {
	Path  string `json:"path"`
	Ref   string `json:"ref,omitempty"`
	Image bool   `json:"image"`
	Error string `json:"error,omitempty"`
}

// RestoreStoredAttachments turns the paths a copy carried back into attachment
// references for the workspace at base. It only reads: a restore must not
// create the attachment directory it is looking in, and a path outside it is
// refused rather than resolved against the process working directory.
func RestoreStoredAttachments(base string, paths []string) []RestoredAttachment {
	out := make([]RestoredAttachment, 0, len(paths))
	for _, raw := range paths {
		rel, err := cleanStoredAttachmentPath(raw)
		if err != nil {
			out = append(out, RestoredAttachment{Path: strings.TrimSpace(raw), Error: err.Error()})
			continue
		}
		if !storedAttachmentExists(base, rel) {
			out = append(out, RestoredAttachment{Path: rel, Error: "attachment is not in this workspace"})
			continue
		}
		out = append(out, RestoredAttachment{Path: rel, Ref: "@" + rel, Image: isImageAttachmentRef(rel)})
	}
	return out
}

// cleanStoredAttachmentPath accepts what a copy records — a workspace-relative
// token, with or without its leading '@' — and nothing else. It answers a
// refusal instead of a path when the input names anywhere else.
func cleanStoredAttachmentPath(raw string) (string, error) {
	p := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
	p = strings.TrimPrefix(filepath.ToSlash(p), "./")
	if p == "" || filepath.IsAbs(filepath.FromSlash(p)) || strings.HasPrefix(p, "/") {
		return "", errAttachmentPathOutside
	}
	clean := path.Clean(p)
	if clean == "." || !filepath.IsLocal(filepath.FromSlash(clean)) || !isAttachmentRef(clean) {
		return "", errAttachmentPathOutside
	}
	if path.Base(clean) == "attachments" {
		return "", errAttachmentPathOutside
	}
	return clean, nil
}

type attachmentPathError string

func (e attachmentPathError) Error() string { return string(e) }

const errAttachmentPathOutside = attachmentPathError("attachment path is outside .reasonix/attachments")
