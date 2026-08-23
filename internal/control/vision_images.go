package control

import (
	"context"
	"encoding/base64"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
)

var (
	inlineImageLimit = provider.MaxInlineImageBytes
	uploadVisionFile = provider.UploadUserDataFile
)

func classifyVisionToken(tok string) (ref, bool) {
	tok = strings.TrimSpace(tok)
	if provider.IsImageHTTPURL(tok) {
		return ref{kind: refRemoteImage, path: tok, raw: tok}, true
	}
	if provider.IsImageFileID(tok) {
		return ref{kind: refFileID, path: tok, raw: tok}, true
	}
	return ref{}, false
}

func bareVisionRefs(line string) []ref {
	var refs []ref
	seen := map[string]bool{}
	for tok := range strings.FieldsSeq(line) {
		tok = strings.Trim(tok, `<>"'`)
		tok = strings.TrimRight(tok, ".,;!?)]}")
		if seen[tok] {
			continue
		}
		r, ok := classifyVisionToken(tok)
		if !ok {
			continue
		}
		seen[tok] = true
		refs = append(refs, r)
	}
	return refs
}

func (c *Controller) visionRefImageValue(r ref, baseDir string) (string, error) {
	switch r.kind {
	case refRemoteImage, refFileID:
		return r.path, nil
	case refImage, refFile:
		return c.visionLocalImageValue(r.path, baseDir)
	default:
		return "", fmt.Errorf("reference is not an image")
	}
}

func imageAttachmentNote(path string, vision bool) string {
	if vision {
		return "[The image at @" + path + " is attached as visual input. Look at the image directly; do not OCR or read the file unless the user asks.]"
	}
	return "[image attachment available at @" + path + "; sent as direct model image input only when the selected model supports vision. Text-only models can still use an available OCR/image/vision tool with this local path; image bytes are not inlined into prompt text.]"
}

func imageFileRefNote(displayPath, mime string, size int64, attached, vision bool) string {
	if attached && vision {
		return fmt.Sprintf("[image file %s, mime=%s, %d bytes — attached as visual input. Look at the image directly; do not OCR or read the file unless the user asks.]", displayPath, mime, size)
	}
	if attached {
		return fmt.Sprintf("[image file %s, mime=%s, %d bytes — sent as direct model image input only when the selected model supports vision. Text-only models can still use an available OCR/image/vision tool with this local path; image bytes are not inlined into prompt text.]", displayPath, mime, size)
	}
	return fmt.Sprintf("[image file %s, mime=%s, %d bytes — not sent as direct model image input because no workspace root is available. Use a workspace-scoped file reference, image attachment, or an available OCR/image/vision tool with a readable local path.]", displayPath, mime, size)
}

func (c *Controller) visionLocalImageValue(pathName, baseDir string) (string, error) {
	var (
		dataURL string
		err     error
	)
	if isAttachmentRef(filepath.ToSlash(pathName)) {
		dataURL, err = visionImageDataURL(pathName)
	} else {
		dataURL, err = visionFileImageDataURL(pathName, baseDir)
	}
	if err != nil {
		return "", err
	}
	return c.maybeUploadDataURL(pathName, dataURL)
}

func (c *Controller) maybeUploadDataURL(filename, dataURL string) (string, error) {
	_, payload, ok := provider.ParseImageDataURL(dataURL)
	if !ok {
		return dataURL, nil
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(raw) <= inlineImageLimit {
		return dataURL, nil
	}
	id, err := c.uploadOfficialVisionFile(filename, raw)
	if err == nil {
		return id, nil
	}
	if len(raw) > provider.MaxInlineImageBytes {
		return "", err
	}
	return dataURL, nil
}

func (c *Controller) uploadOfficialVisionFile(filename string, data []byte) (string, error) {
	if c == nil {
		return "", fmt.Errorf("files api: no session")
	}
	cfg, err := config.LoadForRoot(c.workspaceRoot)
	if err != nil {
		return "", err
	}
	ref := c.modelRef
	if ref == "" {
		ref = cfg.DefaultModel
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok || !openai.IsDeepSeek(entry.BaseURL) {
		return "", fmt.Errorf("files api requires official DeepSeek")
	}
	protocol := "openai"
	if strings.EqualFold(entry.Kind, "anthropic") {
		protocol = "anthropic"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return uploadVisionFile(ctx, provider.FileUpload{
		BaseURL:    entry.BaseURL,
		APIKey:     entry.APIKey(),
		AuthHeader: entry.AuthHeader,
		Protocol:   protocol,
		Filename:   path.Base(filename),
		Data:       data,
	})
}
