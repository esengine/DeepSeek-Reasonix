// Package visionimage is the single judgement on whether an image may enter a
// model request. Every path that puts pixels on the wire — a user's @-reference,
// a pasted attachment, an MCP tool result — asks here, because a host rejects an
// oversized image as an unsupported *format*, and the rejected message then
// fails every later turn in that session.
package visionimage

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // register gif decoder
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register webp decoder
)

// MaxDim caps the longest image side sent to a model. OpenAI and Anthropic
// downscale to roughly this server-side anyway, so a larger upload only wastes
// request bytes and image tokens without adding fidelity.
const MaxDim = 1568

// maxDecodePixels guards against decompression-bomb attachments: a tiny file can
// declare enormous dimensions. Beyond this the image is refused rather than
// decoded, because scaling it is what would allocate the bomb.
const maxDecodePixels = 50_000_000

// ErrUnfit reports that an image cannot be proven to fit the vision budget, so
// it is never put on the wire.
var ErrUnfit = errors.New("image does not fit the vision budget")

// DetectMime returns the media type the bytes actually are, or "" when they are
// not one of the four formats every vision host accepts. It reads the content,
// never the file name: an extension is a claim, and a wrong one reaches the
// provider as a media_type that does not match its payload.
func DetectMime(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	mime := http.DetectContentType(raw[:min(len(raw), 512)])
	if Ext(mime) == "" {
		return ""
	}
	return mime
}

// Ext returns the file extension for a media type this package accepts, or ""
// for anything else.
func Ext(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ""
}

// Fit returns the bytes to send, guaranteeing both sides are within MaxDim.
// PNG/GIF stay lossless (screenshots, text, transparency), JPEG/WebP go to
// JPEG. An image whose dimensions cannot be established or reduced is refused
// with ErrUnfit rather than forwarded unchanged.
func Fit(raw []byte, mime string) ([]byte, string, error) {
	if Ext(mime) == "" {
		return nil, "", fmt.Errorf("%w: no decoder for %s here, so its dimensions cannot be checked", ErrUnfit, mime)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("%w: the bytes do not decode as %s, so its dimensions cannot be checked", ErrUnfit, mime)
	}
	if cfg.Width <= MaxDim && cfg.Height <= MaxDim {
		return raw, mime, nil // within budget — no point re-encoding
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxDecodePixels {
		return nil, "", fmt.Errorf("%w: %dx%d is past the %d-pixel decode limit, so it cannot be downscaled — shrink it first",
			ErrUnfit, cfg.Width, cfg.Height, maxDecodePixels)
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %dx%d failed to decode past its header", ErrUnfit, cfg.Width, cfg.Height)
	}
	w, h := scaledDims(cfg.Width, cfg.Height, MaxDim)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)

	var buf bytes.Buffer
	if mime == "image/png" || mime == "image/gif" {
		if err := png.Encode(&buf, dst); err != nil {
			return nil, "", fmt.Errorf("%w: re-encoding %dx%d as PNG failed", ErrUnfit, w, h)
		}
		return buf.Bytes(), "image/png", nil
	}
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", fmt.Errorf("%w: re-encoding %dx%d as JPEG failed", ErrUnfit, w, h)
	}
	return buf.Bytes(), "image/jpeg", nil
}

// scaledDims returns dimensions with the longest side clamped to m, preserving
// aspect ratio (each side at least 1px).
func scaledDims(w, h, m int) (int, int) {
	if w >= h {
		nh := max(h*m/w, 1)
		return m, nh
	}
	nw := max(w*m/h, 1)
	return nw, m
}
