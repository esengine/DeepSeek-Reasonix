package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func TestAppIconPNGUsesBlueFullCanvasRoundedBackground(t *testing.T) {
	f, err := os.Open("build/appicon.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}

	assertFullCanvasRoundedIcon(t, img, 1024)
}

func TestWindowsICOUsesBlueFullCanvasRoundedBackground(t *testing.T) {
	for _, size := range []int{16, 24, 32, 48, 64, 256} {
		t.Run(fmt.Sprintf("%dx%d", size, size), func(t *testing.T) {
			img := decodeICOImage(t, "build/windows/icon.ico", size)
			assertFullCanvasRoundedIcon(t, img, size)
		})
	}
}

func assertFullCanvasRoundedIcon(t *testing.T, img image.Image, size int) {
	t.Helper()

	bounds := img.Bounds()
	if bounds.Dx() != size || bounds.Dy() != size {
		t.Fatalf("app icon must be square, got %dx%d", bounds.Dx(), bounds.Dy())
	}

	corners := []struct {
		name string
		x    int
		y    int
	}{
		{"top-left", bounds.Min.X, bounds.Min.Y},
		{"top-right", bounds.Max.X - 1, bounds.Min.Y},
		{"bottom-left", bounds.Min.X, bounds.Max.Y - 1},
		{"bottom-right", bounds.Max.X - 1, bounds.Max.Y - 1},
	}
	for _, corner := range corners {
		_, _, _, a := img.At(corner.x, corner.y).RGBA()
		if a > 0xff {
			t.Fatalf("%s corner must be transparent, alpha=%d", corner.name, a)
		}
	}

	_, _, _, centerAlpha := img.At(bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()/2).RGBA()
	if centerAlpha == 0 {
		t.Fatal("app icon center must contain visible artwork")
	}

	edgePoints := []struct {
		name string
		x    int
		y    int
	}{
		{"top", bounds.Min.X + bounds.Dx()/2, bounds.Min.Y},
		{"right", bounds.Max.X - 1, bounds.Min.Y + bounds.Dy()/2},
		{"bottom", bounds.Min.X + bounds.Dx()/2, bounds.Max.Y - 1},
		{"left", bounds.Min.X, bounds.Min.Y + bounds.Dy()/2},
	}
	for _, point := range edgePoints {
		_, _, _, a := img.At(point.x, point.y).RGBA()
		if a == 0 {
			t.Fatalf("%s edge must contain visible rounded-rect background", point.name)
		}
		assertReasonixBlue(t, point.name, img.At(point.x, point.y))
	}
}

func assertReasonixBlue(t *testing.T, name string, colorValue color.Color) {
	t.Helper()

	r16, g16, b16, _ := colorValue.RGBA()
	r, g, b := uint8(r16>>8), uint8(g16>>8), uint8(b16>>8)
	if !near(r, 0x01, 2) || !near(g, 0x53, 2) || !near(b, 0xe5, 2) {
		t.Fatalf("%s edge must use Reasonix blue background, got #%02x%02x%02x", name, r, g, b)
	}
}

func near(got, want uint8, tolerance uint8) bool {
	if got > want {
		return got-want <= tolerance
	}
	return want-got <= tolerance
}

func decodeICOImage(t *testing.T, path string, size int) image.Image {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(data)

	var header struct {
		Reserved uint16
		Type     uint16
		Count    uint16
	}
	if err := binary.Read(r, binary.LittleEndian, &header); err != nil {
		t.Fatal(err)
	}
	if header.Reserved != 0 || header.Type != 1 {
		t.Fatalf("invalid ICO header: reserved=%d type=%d", header.Reserved, header.Type)
	}

	type iconEntry struct {
		Width       uint8
		Height      uint8
		ColorCount  uint8
		Reserved    uint8
		Planes      uint16
		BitCount    uint16
		BytesInRes  uint32
		ImageOffset uint32
	}

	entries := make([]iconEntry, header.Count)
	for i := range entries {
		if err := binary.Read(r, binary.LittleEndian, &entries[i]); err != nil {
			t.Fatal(err)
		}
	}

	expectedSizes := map[int]bool{16: false, 24: false, 32: false, 48: false, 64: false, 256: false}
	targetIndex := -1
	for i, entry := range entries {
		width := int(entry.Width)
		height := int(entry.Height)
		if width == 0 {
			width = 256
		}
		if height == 0 {
			height = 256
		}
		if width != height {
			t.Fatalf("ICO image must be square, got %dx%d", width, height)
		}
		if _, ok := expectedSizes[width]; ok {
			expectedSizes[width] = true
		}
		if width == size {
			targetIndex = i
		}
	}
	for expectedSize, found := range expectedSizes {
		if !found {
			t.Fatalf("ICO is missing %dx%d image", expectedSize, expectedSize)
		}
	}
	if targetIndex < 0 {
		t.Fatalf("ICO is missing %dx%d image", size, size)
	}

	entry := entries[targetIndex]
	end := int(entry.ImageOffset + entry.BytesInRes)
	if end > len(data) {
		t.Fatalf("ICO image offset exceeds file size: offset=%d size=%d file=%d", entry.ImageOffset, entry.BytesInRes, len(data))
	}
	img, err := png.Decode(bytes.NewReader(data[entry.ImageOffset:end]))
	if err != nil {
		t.Fatal(err)
	}
	return img
}
