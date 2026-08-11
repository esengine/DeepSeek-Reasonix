package bot

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"reasonix/internal/control"
)

const maxBotMediaBytes = 25 * 1024 * 1024

var botMediaHTTPClient = &http.Client{Timeout: 30 * time.Second}

func saveInboundMedia(ctx context.Context, workspaceRoot string, mediaURLs []string) (refs []string, errs []error) {
	for _, rawURL := range mediaURLs {
		ref, err := saveOneInboundMedia(ctx, workspaceRoot, rawURL)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		refs = append(refs, ref)
	}
	return refs, errs
}

func saveOneInboundMedia(ctx context.Context, workspaceRoot, rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("empty media URL")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported media URL scheme %q", u.Scheme)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := botMediaHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download media: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBotMediaBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw) > maxBotMediaBytes {
		return "", fmt.Errorf("media must be between 1 byte and 25 MB")
	}
	contentType := resp.Header.Get("Content-Type")
	if semi := strings.Index(contentType, ";"); semi >= 0 {
		contentType = strings.TrimSpace(contentType[:semi])
	}
	if strings.TrimSpace(contentType) == "" || strings.EqualFold(contentType, "application/octet-stream") {
		contentType = http.DetectContentType(raw[:min(len(raw), 512)])
	}
	name := mediaFilename(u, contentType)
	if strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return control.SaveImageBytesInRoot(workspaceRoot, contentType, raw)
	}
	return control.SaveAttachmentBytesInRoot(workspaceRoot, name, raw)
}

func saveInboundMediaItems(ctx context.Context, workspaceRoot string, items []InboundMedia) (refs, fallbacks []string, errs []error) {
	for _, item := range items {
		ref, err := saveOneInboundMediaItem(ctx, workspaceRoot, item)
		if err != nil {
			errs = append(errs, err)
			if fallback := strings.TrimSpace(item.FailureText); fallback != "" {
				fallbacks = append(fallbacks, fallback)
			}
			continue
		}
		refs = append(refs, ref)
	}
	return refs, fallbacks, errs
}

func saveOneInboundMediaItem(ctx context.Context, workspaceRoot string, item InboundMedia) (string, error) {
	if item.Load != nil {
		data, name, err := item.Load(ctx)
		if err != nil {
			return "", err
		}
		item.Data = data
		if strings.TrimSpace(item.Name) == "" {
			item.Name = name
		}
	}
	if len(item.Data) == 0 || len(item.Data) > maxBotMediaBytes {
		return "", fmt.Errorf("media must be between 1 byte and 25 MB")
	}
	contentType := strings.TrimSpace(item.MIME)
	if contentType == "" || strings.EqualFold(contentType, "application/octet-stream") {
		contentType = http.DetectContentType(item.Data[:min(len(item.Data), 512)])
	}
	if strings.HasPrefix(strings.ToLower(contentType), "image/") {
		if ref, err := control.SaveImageBytesInRoot(workspaceRoot, contentType, item.Data); err == nil {
			return ref, nil
		}
		// 图片超过 10MB 上限或签名不被识别时，降级存为普通附件而不是丢弃。
	}
	name := strings.TrimSpace(item.Name)
	if name == "" {
		if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
			name = "media" + exts[0]
		} else {
			name = "media.bin"
		}
	}
	return control.SaveAttachmentBytesInRoot(workspaceRoot, name, item.Data)
}

func appendMediaFallbacks(text string, fallbacks []string) string {
	for _, fallback := range fallbacks {
		fallback = strings.TrimSpace(fallback)
		if fallback == "" {
			continue
		}
		if strings.TrimSpace(text) != "" {
			text += "\n"
		}
		text += fallback
	}
	return text
}

func mediaFilename(u *url.URL, contentType string) string {
	base := path.Base(u.Path)
	if base == "." || base == "/" || strings.TrimSpace(base) == "" {
		if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
			return "media" + exts[0]
		}
		return "media.bin"
	}
	return base
}

func appendMediaRefs(text string, refs []string) string {
	if len(refs) == 0 {
		return text
	}
	var b strings.Builder
	if strings.TrimSpace(text) != "" {
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	b.WriteString("Attachments:")
	for _, ref := range refs {
		b.WriteString("\n@")
		b.WriteString(ref)
	}
	return b.String()
}
