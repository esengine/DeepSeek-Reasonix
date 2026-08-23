package provider

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	// MaxInlineImageBytes is the official DeepSeek base64 / URL image cap (32 MiB).
	MaxInlineImageBytes = 32 << 20
	// MaxFileAPIImageBytes is the official DeepSeek Files API image cap (64 MiB).
	MaxFileAPIImageBytes = 64 << 20
	// MaxImageURLRunes is the official DeepSeek external image URL length cap.
	MaxImageURLRunes = 8192
)

// ImageKind classifies a Message.Images entry for vision serializers.
type ImageKind int

const (
	ImageNone ImageKind = iota
	ImageDataURL
	ImageHTTPURL
	ImageFileID
)

// ClassifyImage reports how a stored image reference should appear on the wire.
// Older sessions only stored data URLs; HTTP URLs and Files API ids are additive.
func ClassifyImage(s string) ImageKind {
	s = strings.TrimSpace(s)
	if s == "" {
		return ImageNone
	}
	if _, _, ok := ParseImageDataURL(s); ok {
		return ImageDataURL
	}
	if IsImageFileID(s) {
		return ImageFileID
	}
	if IsImageHTTPURL(s) {
		return ImageHTTPURL
	}
	return ImageNone
}

// IsImageFileID reports a DeepSeek Files API id (file-api-…).
func IsImageFileID(s string) bool {
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, "file-api-")
	if !ok || rest == "" || len(rest) > 128 {
		return false
	}
	for _, r := range rest {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// IsImageHTTPURL reports a public http(s) image URL that official DeepSeek
// can fetch (extension-gated so arbitrary links are not treated as images).
func IsImageHTTPURL(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > MaxImageURLRunes {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.User != nil || u.Host == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return false
	}
	path := u.EscapedPath()
	if i := strings.LastIndex(path, "."); i >= 0 {
		path = path[i:]
	} else {
		return false
	}
	switch strings.ToLower(path) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	default:
		return false
	}
}
