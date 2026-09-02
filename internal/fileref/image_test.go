package fileref

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	out, mime := CompressForVision(raw, "image/png")
	if mime != "image/png" {
		t.Errorf("mime = %q, want image/png", mime)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode out: %v", err)
	}
	// Pixel count is what governs vision token cost; assert the reduction there
	// (byte size isn't a robust invariant for synthetic, highly-compressible input).
	if cfg.Width != maxVisionDim || cfg.Height != 1500*maxVisionDim/3000 {
		t.Errorf("dims = %dx%d, want %dx%d", cfg.Width, cfg.Height, maxVisionDim, 1500*maxVisionDim/3000)
	}
	if cfg.Width*cfg.Height >= 3000*1500 {
		t.Errorf("pixel count %d not reduced from %d", cfg.Width*cfg.Height, 3000*1500)
	}
}

func TestCompressForVisionKeepsSmallImageVerbatim(t *testing.T) {
	raw := makeTestPNG(t, 100, 80)
	out, mime := CompressForVision(raw, "image/png")
	if mime != "image/png" || !bytes.Equal(out, raw) {
		t.Errorf("an in-budget image must pass through unchanged (got %d bytes, mime %q)", len(out), mime)
	}
}

func TestCompressForVisionJPEGStaysJPEG(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2400, 1200)), nil); err != nil {
		t.Fatal(err)
	}
	out, mime := CompressForVision(buf.Bytes(), "image/jpeg")
	if mime != "image/jpeg" {
		t.Errorf("mime = %q, want image/jpeg", mime)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode out: %v", err)
	}
	if cfg.Width != maxVisionDim {
		t.Errorf("width = %d, want %d", cfg.Width, maxVisionDim)
	}
}

func TestCompressForVisionUnsupportedMimePassthrough(t *testing.T) {
	raw := []byte("not really an image")
	out, mime := CompressForVision(raw, "image/svg+xml")
	if !bytes.Equal(out, raw) || mime != "image/svg+xml" {
		t.Errorf("unsupported format must pass through unchanged, got %d bytes %q", len(out), mime)
	}
}

func TestFileImageDataURLConvertsWorkspaceImage(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "shot.png"), makeTestPNG(t, 40, 30), 0o644); err != nil {
		t.Fatal(err)
	}
	url, err := FileImageDataURL("shot.png", ws)
	if err != nil {
		t.Fatalf("FileImageDataURL: %v", err)
	}
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("url = %q, want png data URL", url)
	}
}

func TestFileImageDataURLRejectsOutsideWorkspace(t *testing.T) {
	ws := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, makeTestPNG(t, 40, 30), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FileImageDataURL(outside, ws); err == nil {
		t.Fatal("expected confinement error for a path outside the workspace root")
	}
}

func TestFileImageDataURLRejectsSymlink(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.png")
	if err := os.WriteFile(target, makeTestPNG(t, 40, 30), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(ws, "link.png")); err != nil {
		t.Skipf("symlinks unavailable on this filesystem: %v", err)
	}
	if _, err := FileImageDataURL("link.png", ws); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err = %v, want symlink rejection", err)
	}
}

func TestFileImageDataURLRejectsMissingAndEmpty(t *testing.T) {
	ws := t.TempDir()
	if _, err := FileImageDataURL("missing.png", ws); err == nil {
		t.Fatal("expected error for missing file")
	}
	if err := os.WriteFile(filepath.Join(ws, "empty.png"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FileImageDataURL("empty.png", ws); err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestFileImageDataURLRejectsNonImage(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "text.png"), []byte("definitely not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FileImageDataURL("text.png", ws); err == nil || !strings.Contains(err.Error(), "not a supported image") {
		t.Fatalf("err = %v, want unsupported-image error", err)
	}
}

func TestFileImageDataURLRejectsEscape(t *testing.T) {
	ws := t.TempDir()
	if _, err := FileImageDataURL("../../../etc/passwd", ws); err == nil {
		t.Fatal("expected error for a relative path escaping the workspace")
	}
}

func TestFileImageDataURLRequiresWorkspaceRoot(t *testing.T) {
	if _, err := FileImageDataURL("shot.png", ""); err == nil {
		t.Fatal("expected error when no workspace root is configured")
	}
}
