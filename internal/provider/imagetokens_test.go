package provider

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
)

func pngDataURL(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestDeepSeekImageTokensV41Vectors(t *testing.T) {
	cases := []struct {
		w, h, tokens int
	}{
		{100, 100, 184},
		{544, 544, 184},
		{640, 480, 206},
		{800, 800, 422},
		{1024, 768, 496},
		{1066, 600, 407},
		{1300, 1300, 994},
		{1920, 1080, 968},
		{2000, 2000, 994},
		{5000, 5000, 994},
		{300, 50, 200},
		{8192, 100, 593},
		{16, 8192, 590},
		{12, 1123, 380},
		{89, 2076, 254},
	}
	for _, tc := range cases {
		if got := DeepSeekImageTokens(tc.w, tc.h); got != tc.tokens {
			t.Errorf("%dx%d = %d, want %d", tc.w, tc.h, got, tc.tokens)
		}
	}
}

func TestDeepSeekImageTokensCapAndFloor(t *testing.T) {
	if DeepSeekImageTokens(100, 100) != DeepSeekImageTokens(544, 544) {
		t.Fatal("tiny squares must price at the 544x544 scale-up floor")
	}
	for _, dim := range [][2]int{{2000, 2000}, {5000, 5000}, {8192, 8192}, {16, 8192}, {9000, 1}, {1, 9000}} {
		if got := DeepSeekImageTokens(dim[0], dim[1]); got > 1024 {
			t.Errorf("%dx%d = %d, want <= 1024", dim[0], dim[1], got)
		}
	}
	if DeepSeekImageTokens(9000, 1) != 1024 || DeepSeekImageTokens(1, 9000) != 1024 {
		t.Fatal("extreme aspect ratios must hit the 1024 cap")
	}
}

func TestDeepSeekRequestImageDimensions(t *testing.T) {
	cases := []struct {
		w, h, outW, outH int
	}{
		{800, 800, 800, 800},
		{1302, 1302, 1302, 1302},
		{8192, 78, 4096, 39},
		{1, 9000, 1, 4096},
		{1303, 1303, 1302, 1302},
		{2048, 2048, 1302, 1302},
		{2048, 1024, 1848, 924},
		{3840, 2160, 1708, 961},
		{1080, 2400, 838, 1862},
		{1224, 1429, 1187, 1386},
	}
	for _, tc := range cases {
		gotW, gotH := DeepSeekRequestImageDimensions(tc.w, tc.h)
		if gotW != tc.outW || gotH != tc.outH {
			t.Errorf("%dx%d -> %dx%d, want %dx%d", tc.w, tc.h, gotW, gotH, tc.outW, tc.outH)
		}
	}
}

func TestEstimateImageTokensSkipsOffloaded(t *testing.T) {
	if EstimateImageTokens(ImageOffloadedRef) != 0 || EstimateImageTokens("") != 0 {
		t.Fatal("offloaded and empty slots must be free")
	}
}

func TestDeepSeekRequestImageDimensionsPricesSentPixels(t *testing.T) {
	gotW, gotH := DeepSeekRequestImageDimensions(1224, 1429)
	if gotW != 1187 || gotH != 1386 {
		t.Fatalf("sent %dx%d, want 1187x1386", gotW, gotH)
	}
	if DeepSeekImageTokens(1224, 1429) != 959 {
		t.Fatalf("source tokens = %d, want 959", DeepSeekImageTokens(1224, 1429))
	}
	if DeepSeekImageTokens(gotW, gotH) != 992 {
		t.Fatalf("sent tokens = %d, want 992", DeepSeekImageTokens(gotW, gotH))
	}
}

func TestEstimateImageTokensUsesDecodedPixels(t *testing.T) {
	ref := pngDataURL(t, 8, 8)
	if got, want := EstimateImageTokens(ref), DeepSeekImageTokens(8, 8); got != want {
		t.Fatalf("estimate = %d, want %d", got, want)
	}
	if EstimateImageTokens(ref) != 184 {
		t.Fatalf("8x8 must price at the 544x544 floor, got %d", EstimateImageTokens(ref))
	}
}
