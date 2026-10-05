package visionimage

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
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

func TestFitDownscalesOversizedPNG(t *testing.T) {
	raw := makeTestPNG(t, 3000, 1500)
	out, mime, err := Fit(raw, "image/png")
	if err != nil {
		t.Fatalf("fitForVision: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("mime = %q, want image/png", mime)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode out: %v", err)
	}
	// Pixel count is what governs vision token cost; assert the reduction there
	// (byte size isn't a robust invariant for synthetic, highly-compressible input).
	if cfg.Width != MaxDim || cfg.Height != 1500*MaxDim/3000 {
		t.Errorf("dims = %dx%d, want %dx%d", cfg.Width, cfg.Height, MaxDim, 1500*MaxDim/3000)
	}
	if cfg.Width*cfg.Height >= 3000*1500 {
		t.Errorf("pixel count %d not reduced from %d", cfg.Width*cfg.Height, 3000*1500)
	}
}

func TestFitKeepsSmallImageVerbatim(t *testing.T) {
	raw := makeTestPNG(t, 100, 80)
	out, mime, err := Fit(raw, "image/png")
	if err != nil {
		t.Fatalf("fitForVision: %v", err)
	}
	if mime != "image/png" || !bytes.Equal(out, raw) {
		t.Errorf("an in-budget image must pass through unchanged (got %d bytes, mime %q)", len(out), mime)
	}
}

func TestFitJPEGStaysJPEG(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2400, 1200)), nil); err != nil {
		t.Fatal(err)
	}
	out, mime, err := Fit(buf.Bytes(), "image/jpeg")
	if err != nil {
		t.Fatalf("fitForVision: %v", err)
	}
	if mime != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg", mime)
	}
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(out)); cfg.Width != MaxDim {
		t.Errorf("width = %d, want %d", cfg.Width, MaxDim)
	}
}

func TestFitRefusesUndecodable(t *testing.T) {
	if _, _, err := Fit([]byte("<svg xmlns='...'></svg>"), "image/svg+xml"); !errors.Is(err, ErrUnfit) {
		t.Errorf("a mime with no decoder must be refused, got %v", err)
	}
	if _, _, err := Fit([]byte("\x89PNG\r\n\x1a\n truncated"), "image/png"); !errors.Is(err, ErrUnfit) {
		t.Errorf("bytes that do not decode must be refused, got %v", err)
	}
}

// A host rejects an oversized image as an unsupported *format*, and the rejected
// message then fails every later turn — so nothing may leave here above budget.
func TestFitRefusesWhatItCannotDownscale(t *testing.T) {
	var buf bytes.Buffer
	// A 1x1 image whose header claims more pixels than the decode budget allows
	// is the shape this guards: the file is tiny, the declared canvas is not.
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	raw = append([]byte(nil), raw...)
	// IHDR width/height live at bytes 16..23; claim 40000x40000 (1.6e9 pixels).
	copy(raw[16:24], []byte{0, 0, 0x9c, 0x40, 0, 0, 0x9c, 0x40})
	if _, _, err := Fit(raw, "image/png"); !errors.Is(err, ErrUnfit) {
		t.Errorf("an image past the decode budget must be refused, got %v", err)
	}
}

// The invariant every caller depends on: what leaves here is within budget.
func TestFitOutputAlwaysWithinBudget(t *testing.T) {
	for _, dims := range [][2]int{{100, 80}, {3000, 1500}, {1500, 3000}, {20000, 300}, {300, 20000}} {
		raw := makeTestPNG(t, dims[0], dims[1])
		out, _, err := Fit(raw, "image/png")
		if err != nil {
			t.Fatalf("%dx%d: %v", dims[0], dims[1], err)
		}
		cfg, _, decErr := image.DecodeConfig(bytes.NewReader(out))
		if decErr != nil {
			t.Fatalf("%dx%d: decode result: %v", dims[0], dims[1], decErr)
		}
		if cfg.Width > MaxDim || cfg.Height > MaxDim {
			t.Errorf("%dx%d left as %dx%d, past the %d budget", dims[0], dims[1], cfg.Width, cfg.Height, MaxDim)
		}
	}
}
