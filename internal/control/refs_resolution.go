package control

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/attachment"
)

// ResolveRefs resolves the @references in a line into a single tagged context
// block (file/dir contents, MCP resource bodies), plus per-reference errors.
func (c *Controller) ResolveRefs(ctx context.Context, line string) (block string, errs []string) {
	resolved := c.resolveRefsForTurn(ctx, line, false)
	return resolved.block, resolved.errs
}

// ResolveScopedRefs is the HTTP/frontend variant: file references are honored
// only when they can be resolved under the controller workspace root.
func (c *Controller) ResolveScopedRefs(ctx context.Context, line string) (block string, errs []string) {
	resolved := c.resolveRefsForTurn(ctx, line, true)
	return resolved.block, resolved.errs
}

type resolvedReferences struct {
	block     string
	errs      []string
	images    []string
	imageErrs []ImageReferenceFailure
}

type preparedImageReferences struct {
	byPath  map[string]string
	ordered []string
	inputs  []attachment.ImageInput
}

type preparedImageReferencesContextKey struct{}

func isAttachmentRef(token string) bool {
	return strings.HasPrefix(filepath.ToSlash(token), ".reasonix/attachments/")
}

func isImageAttachmentRef(token string) bool {
	switch strings.ToLower(filepath.Ext(token)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".tif", ".tiff":
		return true
	}
	return false
}

func statAttachmentImage(root, path string) error {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	joined := filepath.Join(absRoot, filepath.FromSlash(path))
	confine := filepath.Join(absRoot, ".reasonix", "attachments")
	rel, err := filepath.Rel(confine, joined)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("image path is outside .reasonix/attachments")
	}
	info, err := os.Lstat(joined)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("image path must not be a symlink")
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxImageAttachmentBytes {
		return fmt.Errorf("pasted image must be between 1 byte and 64 MB")
	}
	return nil
}

func normalizedImageReferencePath(path string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
}

func contextWithPreparedImageReferences(ctx context.Context, prepared preparedImageReferences) context.Context {
	if len(prepared.byPath) == 0 && len(prepared.inputs) == 0 {
		return ctx
	}
	return context.WithValue(ctx, preparedImageReferencesContextKey{}, prepared)
}

func preparedImageReference(ctx context.Context, path string) (string, bool) {
	prepared, _ := ctx.Value(preparedImageReferencesContextKey{}).(preparedImageReferences)
	value, ok := prepared.byPath[normalizedImageReferencePath(path)]
	return value, ok
}

func (c *Controller) prepareExplicitImageReferences(line string) (preparedImageReferences, []ImageReferenceFailure) {
	return c.prepareExplicitImageReferencesContext(c.attachmentContext(), line)
}

func (c *Controller) prepareExplicitImageReferencesContext(ctx context.Context, line string) (preparedImageReferences, []ImageReferenceFailure) {
	return c.prepareSubmissionImagesContext(ctx, SubmissionRequest{Input: line})
}

func (c *Controller) explicitImageSources(line string) []attachment.Source {
	var sources []attachment.Source
	seen := map[string]bool{}
	for _, token := range parseRefTokens(line) {
		if !isAttachmentRef(token) || !isImageAttachmentRef(token) || strings.HasPrefix(token, "draft:") {
			continue
		}
		key := normalizedImageReferencePath(token)
		if seen[key] {
			continue
		}
		seen[key] = true
		sources = append(sources, attachment.Source{DisplayName: filepath.Base(filepath.FromSlash(token)), Path: token, WorkspaceRoot: c.workspaceRoot, Confine: ".reasonix/attachments"})
	}
	return sources
}

func imageFailuresFromAttachment(err error) []ImageReferenceFailure {
	var batch attachment.BatchError
	if errors.As(err, &batch) {
		out := make([]ImageReferenceFailure, 0, len(batch))
		for _, item := range batch {
			out = append(out, ImageReferenceFailure{Code: mapAttachmentCode(item.Code), Name: attachment.NormalizeDisplayName(item.Name), Index: item.Index, Cause: item})
		}
		return out
	}
	var item attachment.Error
	if errors.As(err, &item) {
		return []ImageReferenceFailure{{Code: mapAttachmentCode(item.Code), Name: attachment.NormalizeDisplayName(item.Name), Index: item.Index, Cause: item}}
	}
	return []ImageReferenceFailure{imageReferenceFailure("image", err)}
}

func mapAttachmentCode(code attachment.Code) ImageReferenceFailureCode {
	switch code {
	case attachment.CodeMissing:
		return ImageReferenceMissing
	case attachment.CodeUnreadable:
		return ImageReferenceUnreadable
	case attachment.CodeUnsafe:
		return ImageReferenceUnsafe
	case attachment.CodeUnsupported:
		return ImageReferenceUnsupported
	case attachment.CodeCorrupt:
		return ImageReferenceCorrupt
	case attachment.CodeSize, attachment.CodeTooMany, attachment.CodeBatchSize:
		return ImageReferenceTooLarge
	case attachment.CodeChanged:
		return ImageReferenceChanged
	case attachment.CodeCanceled:
		return ImageReferenceCanceled
	default:
		return ImageReferenceUnreadable
	}
}

type ImageReferenceFailureCode string

const (
	ImageReferenceMissing     ImageReferenceFailureCode = "missing"
	ImageReferenceUnreadable  ImageReferenceFailureCode = "unreadable"
	ImageReferenceUnsafe      ImageReferenceFailureCode = "unsafe_path"
	ImageReferenceUnsupported ImageReferenceFailureCode = "unsupported_format"
	ImageReferenceTooLarge    ImageReferenceFailureCode = "size_limit"
	ImageReferenceChanged     ImageReferenceFailureCode = "changed"
	ImageReferenceCorrupt     ImageReferenceFailureCode = "corrupt"
	ImageReferenceCanceled    ImageReferenceFailureCode = "canceled"
)

// ImageReferenceFailure is safe to return across UI/RPC boundaries: Name is
// only the attachment basename and Cause is available to local diagnostics.
type ImageReferenceFailure struct {
	Code  ImageReferenceFailureCode
	Name  string
	Index int
	Cause error
}

func (e ImageReferenceFailure) Error() string {
	name := strings.TrimSpace(e.Name)
	if name == "" {
		name = "image"
	}
	detail := "could not be read"
	switch e.Code {
	case ImageReferenceMissing:
		detail = "does not exist"
	case ImageReferenceUnsafe:
		detail = "has an unsafe path"
	case ImageReferenceUnsupported:
		detail = "is not an image or uses an unsupported format"
	case ImageReferenceTooLarge:
		detail = "must be between 1 byte and 64 MB"
	case ImageReferenceChanged:
		detail = "changed while it was being read"
	case ImageReferenceCorrupt:
		detail = "is damaged"
	case ImageReferenceCanceled:
		detail = "was canceled"
	}
	return fmt.Sprintf("image attachment %q %s; remove and re-add it, then retry (%s)", name, detail, e.Code)
}

func (e ImageReferenceFailure) Unwrap() error { return e.Cause }

type ImageReferenceFailures []ImageReferenceFailure

func (e ImageReferenceFailures) Error() string {
	if len(e) == 0 {
		return "image attachments could not be read"
	}
	parts := make([]string, 0, len(e))
	for _, failure := range e {
		parts = append(parts, failure.Error())
	}
	return strings.Join(parts, "; ")
}

func imageReferenceFailure(path string, err error) ImageReferenceFailure {
	code := ImageReferenceUnreadable
	message := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, os.ErrNotExist):
		code = ImageReferenceMissing
	case errors.Is(err, os.ErrPermission):
		code = ImageReferenceUnreadable
	case strings.Contains(message, "outside .reasonix/attachments") || strings.Contains(message, "symlink") || strings.Contains(message, "must be relative"):
		code = ImageReferenceUnsafe
	case strings.Contains(message, "between 1 byte") || strings.Contains(message, "too large"):
		code = ImageReferenceTooLarge
	case strings.Contains(message, "not an image") || strings.Contains(message, "unsupported"):
		code = ImageReferenceUnsupported
	case strings.Contains(message, "changed while"):
		code = ImageReferenceChanged
	}
	return ImageReferenceFailure{Code: code, Name: filepath.Base(filepath.FromSlash(path)), Cause: err}
}

// ImageReferenceFailureForPath converts a local read failure into the safe,
// display-name-only error shape used at submission boundaries.
func ImageReferenceFailureForPath(path string, err error) ImageReferenceFailure {
	return imageReferenceFailure(path, err)
}

func (c *Controller) resolveUnscopedRefsForTurn(ctx context.Context, line string) resolvedReferences {
	return c.resolveRefsForTurn(ctx, line, false)
}

func (c *Controller) resolveScopedRefsForTurn(ctx context.Context, line string) resolvedReferences {
	return c.resolveRefsForTurn(ctx, line, true)
}

func (c *Controller) resolveRefsForTurn(ctx context.Context, line string, scopedOnly bool) resolvedReferences {
	refs := resolveBareNames(c.detectRefsMode(line, scopedOnly), c.workspaceRoot)
	var b strings.Builder
	var errs, images []string
	var imageErrs []ImageReferenceFailure
	seenImages := map[string]bool{}
	addImage := func(r ref) (string, bool) {
		if r.kind == refImage && isAttachmentRef(r.path) {
			if _, frozen := preparedImageReference(ctx, r.path); frozen {
				return r.path, true
			}
			root := c.workspaceRoot
			if r.baseDir != "" {
				root = r.baseDir
			}
			if err := statAttachmentImage(root, r.path); err != nil {
				failure := imageReferenceFailure(r.path, err)
				imageErrs = append(imageErrs, failure)
				errs = append(errs, "@"+r.raw+" — "+failure.Error())
				return "", false
			}
			return r.path, true
		}
		value, frozen := preparedImageReference(ctx, r.path)
		var err error
		if !frozen {
			value, err = c.resolveReferenceImage(r)
		}
		if err != nil {
			if r.kind == refImage {
				failure := imageReferenceFailure(r.path, err)
				imageErrs = append(imageErrs, failure)
				errs = append(errs, "@"+r.raw+" — "+failure.Error())
			} else {
				errs = append(errs, "@"+r.raw+" — "+err.Error())
			}
			return "", false
		}
		if value == "" {
			errs = append(errs, "@"+r.raw+" — image reference resolved to an empty input")
			return "", false
		}
		if !seenImages[value] {
			seenImages[value] = true
			images = append(images, value)
		}
		return value, true
	}
	includedInstructionPaths := map[string]bool{}
	includedInstructionBodies := map[string]bool{}
	if current := c.memory.current(); current != nil {
		for _, doc := range current.Docs {
			includedInstructionPaths[cleanAbsPath(doc.Path)] = true
			includedInstructionBodies[doc.Body] = true
		}
	}
	for _, r := range refs {
		switch r.kind {
		case refResource:
			text, err := c.mcp.readResource(ctx, r.server, r.uri)
			if err != nil {
				errs = append(errs, "@"+r.raw+" — "+err.Error())
				continue
			}
			appendRefBlock(&b, "resource", `ref="@`+r.raw+`"`, text)
		case refFile:
			baseDir := c.workspaceRoot
			if r.baseDir != "" {
				baseDir = r.baseDir
			}
			attached := false
			if isImageAttachmentRef(r.path) {
				_, attached = addImage(r)
			}
			text, isDir, err := readFileRefWithVision(r.path, baseDir, attached && c.imageInputEnabled())
			if err != nil {
				errs = append(errs, "@"+r.raw+" — "+err.Error())
				continue
			}
			pathInstructions, diagnostics := c.resolveReferencedInstructions(r, baseDir, includedInstructionPaths, includedInstructionBodies)
			if pathInstructions != "" {
				appendRefBlock(&b, "path-instructions", `target="`+html.EscapeString(displayPathForRef(r))+`"`, pathInstructions)
			}
			for _, diagnostic := range diagnostics {
				errs = append(errs, "@"+r.raw+" — "+diagnostic.Message)
			}
			tag := "file"
			if isDir {
				tag = "dir"
			}
			displayPath := r.path
			if r.displayPath != "" {
				displayPath = r.displayPath
			}
			appendRefBlock(&b, tag, `path="`+displayPath+`"`, text)
		case refImage, refRemoteImage, refFileID:
			if _, attached := addImage(r); attached {
				appendRefBlock(&b, "image", `path="`+r.path+`"`, imageAttachmentNote(r.path, c.imageInputEnabled()))
			}
		}
	}
	for _, r := range bareVisionRefs(line) {
		addImage(r)
	}
	return resolvedReferences{block: b.String(), errs: errs, images: images, imageErrs: imageErrs}
}
