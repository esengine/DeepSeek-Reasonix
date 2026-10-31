package control

import (
	"os"
	"path"
	"path/filepath"
	"slices"

	"reasonix/internal/contract/event"
)

// StoredAttachments lists the images a message references that the host stored or
// was handed this session: workspace attachments under .reasonix/attachments, and
// images the session was given outside the workspace. Each is returned once,
// normalized to the spelling the model resolves — a workspace image by its
// workspace-relative Path and an empty Ref, an external one by its absolute Path plus the opaque Ref the surface drops.
func (c *Controller) StoredAttachments(text string) []event.Attachment {
	var out []event.Attachment
	for _, r := range c.detectRefs(text) {
		if !isImageAttachmentRef(r.path) {
			continue
		}
		if r.baseDir != "" {
			// An image handed to this session outside the workspace. Its absolute
			// path is what a copy reads back, and the opaque token it arrived as
			// is what the surface hides; only a file that is still there is named.
			abs := filepath.ToSlash(filepath.Join(r.baseDir, filepath.FromSlash(r.path)))
			if info, err := os.Stat(filepath.FromSlash(abs)); err != nil || !info.Mode().IsRegular() {
				continue
			}
			if !slices.ContainsFunc(out, func(a event.Attachment) bool { return a.Path == abs }) {
				out = append(out, event.Attachment{Path: abs, Ref: r.raw})
			}
			continue
		}
		rel := path.Clean(filepath.ToSlash(r.path))
		if !isAttachmentRef(rel) || !storedAttachmentExists(c.workspaceRoot, rel) {
			continue
		}
		if !slices.ContainsFunc(out, func(a event.Attachment) bool { return a.Path == rel }) {
			out = append(out, event.Attachment{Path: rel})
		}
	}
	return out
}

// storedAttachmentExists is the read-only half of cleanAttachmentPath: a
// history read must never create the attachment directory it is looking for.
func storedAttachmentExists(base, rel string) bool {
	if base == "" {
		base = "."
	}
	root := filepath.Join(base, ".reasonix", "attachments")
	if info, err := os.Lstat(root); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false
	}
	file := filepath.Join(base, filepath.FromSlash(rel))
	if rejectSymlinkComponents(file, root) != nil {
		return false
	}
	info, err := os.Lstat(file)
	return err == nil && info.Mode().IsRegular()
}
