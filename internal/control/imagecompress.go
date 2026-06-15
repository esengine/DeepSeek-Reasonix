package control

import (
	"bytes"
	"image"
	_ "image/gif" // register gif decoder
	"image/jpeg"
	"image/png"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register webp decoder
)

// maxVisionDim caps the longest image side sent to a model. OpenAI and Anthropic
// downscale to roughly this server-side anyway, so a larger upload only wastes
// request bytes and image tokens without adding fidelity.
const maxVisionDim = 1568

// maxDecodePixels guards against decompression-bomb attachments: a tiny file can
// declare enormous dimensions. Beyond this we skip decoding and send as-is (still
// bounded by the 10 MB file cap).
const maxDecodePixels = 50_000_000

// compressForVision downscales an oversized image to maxVisionDim and re-encodes
// it — PNG/GIF stay lossless (screenshots, text, transparency), JPEG/WebP go to
// JPEG. Best-effort: an undecodable format, a decode/encode failure, or an image
// already within budget returns the original bytes and mime unchanged.
func compressForVision(raw []byte, mime string) ([]byte, string) {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return raw, mime // bmp/tiff/svg: no decoder wired, send original
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width*cfg.Height > maxDecodePixels {
		return raw, mime
	}
	if cfg.Width <= maxVisionDim && cfg.Height <= maxVisionDim {
		return raw, mime // within budget — no point re-encoding
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw, mime
	}
	w, h := scaledDims(cfg.Width, cfg.Height, maxVisionDim)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)

	var buf bytes.Buffer
	if mime == "image/png" || mime == "image/gif" {
		if err := png.Encode(&buf, dst); err != nil {
			return raw, mime
		}
		return buf.Bytes(), "image/png"
	}
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return raw, mime
	}
	return buf.Bytes(), "image/jpeg"
}

// scaledDims returns dimensions with the longest side clamped to m, preserving
// aspect ratio (each side at least 1px).
func scaledDims(w, h, m int) (int, int) {
	if w >= h {
		nh := h * m / w
		if nh < 1 {
			nh = 1
		}
		return m, nh
	}
	nw := w * m / h
	if nw < 1 {
		nw = 1
	}
	return nw, m
}
