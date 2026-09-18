package control

import (
	"context"
	"path/filepath"
	"reasonix/internal/attachment"
	"slices"
	"strings"
)

func submissionSourceFailure(item SubmissionAttachment, index int, err error) []ImageReferenceFailure {
	failures := imageFailuresFromAttachment(err)
	name := "image"
	if item.Path != "" {
		name = filepath.Base(filepath.FromSlash(item.Path))
	} else if item.Reference != nil {
		name = item.Reference.DisplayName
	}
	for i := range failures {
		failures[i].Index = index + 1
		failures[i].Name = attachment.NormalizeDisplayName(name)
	}
	return failures
}

func withoutDraftID(ids []string, remove string) []string {
	var out []string
	for _, id := range ids {
		if id != remove {
			out = append(out, id)
		}
	}
	return out
}

func canonicalAttachmentToken(input, transport, logicalID string) string {
	if slices.Contains(parseRefTokens(input), transport) {
		return strings.ReplaceAll(input, "@"+EscapeRefPath(transport), "@attachment:"+logicalID)
	}
	return input
}

func legacyRemoteImageInputs(line string) []attachment.ImageInput {
	refs := bareVisionRefs(line)
	for _, token := range parseRefTokens(line) {
		if r, ok := classifyVisionToken(token); ok {
			refs = append(refs, r)
		}
	}
	seen := map[string]bool{}
	var inputs []attachment.ImageInput
	for _, r := range refs {
		if seen[r.path] {
			continue
		}
		seen[r.path] = true
		switch r.kind {
		case refRemoteImage:
			inputs = append(inputs, attachment.ImageInput{Kind: attachment.KindURL, URL: r.path})
		case refFileID:
			inputs = append(inputs, attachment.ImageInput{Kind: attachment.KindFiles, FilesID: r.path})
		}
	}
	return inputs
}

// Keep ordinary image references when a submission also uses the new explicit
// attachment channel. Without this merge, the frozen channel would mask them.
func (c *Controller) appendOrdinaryImageSources(line string, sources []attachment.Source) []attachment.Source {
	seen := make(map[string]bool)
	for _, source := range sources {
		seen[normalizedImageReferencePath(source.Path)] = true
	}
	for _, r := range c.detectRefsMode(line, true) {
		imageFile := r.kind == refFile && isImageAttachmentRef(r.path)
		if (r.kind != refImage && !imageFile) || isAttachmentRef(r.path) || seen[normalizedImageReferencePath(r.path)] {
			continue
		}
		root := c.workspaceRoot
		if r.baseDir != "" {
			root = r.baseDir
		}
		sources = append(sources, attachment.Source{Path: r.path, WorkspaceRoot: root, Confine: ".", DisplayName: filepath.Base(r.path)})
		seen[normalizedImageReferencePath(r.path)] = true
	}
	return sources
}

func (c *Controller) structuredImageSources(ctx context.Context, items []SubmissionAttachment) ([]attachment.Source, []ImageReferenceFailure) {
	var sources []attachment.Source
	svc := c.attachmentService()
	logical := map[string]bool{}
	for index, item := range items {
		if item.ClientAttachmentID == "" || logical[item.ClientAttachmentID] {
			return nil, submissionSourceFailure(item, index, attachment.Error{Code: attachment.CodeUnsupported, Message: "invalid logical attachment identity"})
		}
		logical[item.ClientAttachmentID] = true
		count := 0
		if item.DraftID != "" {
			count++
		}
		if item.Path != "" {
			count++
		}
		if item.Reference != nil {
			count++
		}
		if count != 1 {
			return nil, submissionSourceFailure(item, index, attachment.Error{Code: attachment.CodeUnsupported, Message: "invalid attachment source"})
		}
		if item.DraftID != "" {
			draft, ok := svc.Drafts().Lookup(c.attachmentScope(), item.DraftID)
			if !ok {
				return nil, submissionSourceFailure(item, index, attachment.Error{Code: attachment.CodeMissing, Message: "draft credential is not valid"})
			}
			sources = append(sources, attachment.Source{Existing: &draft.Ref, DisplayName: draft.DisplayName, Path: "draft:" + item.DraftID})
		} else if item.Path != "" {
			sources = append(sources, attachment.Source{Path: item.Path, WorkspaceRoot: c.workspaceRoot, Confine: ".reasonix/attachments"})
		} else {
			if _, _, err := c.ReadSessionAttachment(ctx, item.Reference.Content.Digest, 0, 1); err != nil {
				return nil, submissionSourceFailure(item, index, err)
			}
			sources = append(sources, attachment.Source{Existing: item.Reference, DisplayName: item.Reference.DisplayName})
		}
	}
	return sources, nil
}
