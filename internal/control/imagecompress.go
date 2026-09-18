package control

import (
	"bytes"
	"image"
	_ "image/gif" // register gif decoder
	"image/jpeg"
	"image/png"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register webp decoder

	"reasonix/internal/provider"
)

const (
	jpegVisionQuality    = 85
	maxRequestImageBytes = 2 << 20
)

var jpegQualityLadder = []int{jpegVisionQuality, 75, 60}

// maxDecodePixels guards against decompression-bomb attachments: a tiny file can
// declare enormous dimensions. Beyond this we skip decoding and send as-is (still
// bounded by the 64 MB file cap).
const maxDecodePixels = 50_000_000

// compressForVision downscales oversized images to the DeepSeek v41 request
// grid. PNG/GIF stay lossless until they exceed 2 MiB, then JPEG 85/75/60 is
// used. Undecodable or in-budget input is returned unchanged.
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
	targetW, targetH := provider.DeepSeekRequestImageDimensions(cfg.Width, cfg.Height)
	needsScale := cfg.Width > targetW || cfg.Height > targetH
	if !needsScale && len(raw) <= maxRequestImageBytes {
		return raw, mime
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw, mime
	}
	outImg := image.Image(src)
	if needsScale {
		dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
		outImg = dst
	}
	encoded, outMIME := encodeVisionImage(outImg, mime)
	if encoded == nil {
		return raw, mime
	}
	return encoded, outMIME
}

func encodeVisionImage(img image.Image, mime string) ([]byte, string) {
	if mime == "image/png" || mime == "image/gif" {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, mime
		}
		if buf.Len() <= maxRequestImageBytes {
			return buf.Bytes(), "image/png"
		}
	}
	data, ok := encodeJPEGLadder(img)
	if !ok {
		return nil, mime
	}
	return data, "image/jpeg"
}

func encodeJPEGLadder(img image.Image) ([]byte, bool) {
	var best []byte
	for _, quality := range jpegQualityLadder {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, false
		}
		out := buf.Bytes()
		if best == nil || len(out) < len(best) {
			best = out
		}
		if len(out) <= maxRequestImageBytes {
			return out, true
		}
	}
	return best, best != nil
}
