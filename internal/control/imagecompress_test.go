package control

import (
	"bytes"
	"crypto/rand"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"reasonix/internal/provider"
)

func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestCompressForVisionDownscalesOversizedPNG(t *testing.T) {
	raw := makeTestPNG(t, 3000, 1500)
	out, mime := compressForVision(raw, "image/png")
	if mime != "image/png" {
		t.Errorf("mime = %q, want image/png", mime)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode out: %v", err)
	}
	// Pixel count is what governs vision token cost; assert the reduction there
	// (byte size isn't a robust invariant for synthetic, highly-compressible input).
	wantW, wantH := provider.DeepSeekRequestImageDimensions(3000, 1500)
	if cfg.Width != wantW || cfg.Height != wantH {
		t.Errorf("dims = %dx%d, want %dx%d", cfg.Width, cfg.Height, wantW, wantH)
	}
	if cfg.Width*cfg.Height >= 3000*1500 {
		t.Errorf("pixel count %d not reduced from %d", cfg.Width*cfg.Height, 3000*1500)
	}
}

func TestCompressForVisionKeepsSmallImageVerbatim(t *testing.T) {
	raw := makeTestPNG(t, 100, 80)
	out, mime := compressForVision(raw, "image/png")
	if mime != "image/png" || !bytes.Equal(out, raw) {
		t.Errorf("an in-budget image must pass through unchanged (got %d bytes, mime %q)", len(out), mime)
	}
}

func TestCompressForVisionJPEGStaysJPEG(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2400, 1200)), nil); err != nil {
		t.Fatal(err)
	}
	out, mime := compressForVision(buf.Bytes(), "image/jpeg")
	if mime != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg", mime)
	}
	wantW, _ := provider.DeepSeekRequestImageDimensions(2400, 1200)
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(out)); cfg.Width != wantW {
		t.Errorf("width = %d, want %d", cfg.Width, wantW)
	}
}

func TestCompressForVisionPassesThroughUndecodable(t *testing.T) {
	raw := []byte("<svg xmlns='...'></svg>")
	out, mime := compressForVision(raw, "image/svg+xml")
	if mime != "image/svg+xml" || !bytes.Equal(out, raw) {
		t.Error("an undecodable mime must pass through unchanged")
	}
}

func TestCompressForVisionJPEGLadderCapsEncodedBytes(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1600, 1600))
	if _, err := rand.Read(img.Pix); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if len(raw) <= maxRequestImageBytes {
		t.Skip("synthetic JPEG stayed under 2 MiB; ladder not exercised")
	}
	out, mime := compressForVision(raw, "image/jpeg")
	if mime != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg", mime)
	}
	if len(out) > maxRequestImageBytes {
		t.Fatalf("encoded %d bytes, want <= %d", len(out), maxRequestImageBytes)
	}
}
