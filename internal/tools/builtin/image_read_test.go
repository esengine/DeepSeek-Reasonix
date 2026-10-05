package builtin

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// Telling a model to hexdump a screenshot is telling it to spend a megabyte of
// context on nothing. An image has somewhere to go — say where.
func TestReadFileSendsImagesToTheVisionPathNotHexdump(t *testing.T) {
	dir := testenv.TempDir(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	if kind := imageKind(png); kind != "PNG" {
		t.Fatalf("imageKind = %q, want PNG", kind)
	}
	if kind := imageKind([]byte{0x00, 0x01, 0x02, 0x03}); kind != "" {
		t.Fatalf("imageKind = %q for arbitrary binary, want none", kind)
	}
	for _, tc := range []struct {
		head []byte
		want string
	}{
		{[]byte{0xFF, 0xD8, 0xFF, 0xE0}, "JPEG"},
		{[]byte("GIF89a"), "GIF"},
		{append([]byte("RIFF0000"), []byte("WEBPVP8 ")...), "WebP"},
	} {
		if got := imageKind(tc.head); got != tc.want {
			t.Errorf("imageKind(%q) = %q, want %q", tc.head[:4], got, tc.want)
		}
	}
}
