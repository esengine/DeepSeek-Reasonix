package main

import (
	"image/png"
	"os"
	"testing"
)

func TestAppIconPNGUsesTransparentSafeArea(t *testing.T) {
	f, err := os.Open("build/appicon.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}

	bounds := img.Bounds()
	if bounds.Dx() != 1024 || bounds.Dy() != 1024 {
		t.Fatalf("app icon must be 1024x1024, got %dx%d", bounds.Dx(), bounds.Dy())
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
		if a != 0 {
			t.Fatalf("%s corner must be transparent, alpha=%d", corner.name, a)
		}
	}

	_, _, _, centerAlpha := img.At(bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()/2).RGBA()
	if centerAlpha == 0 {
		t.Fatal("app icon center must contain visible artwork")
	}

	minX, minY, maxX, maxY := bounds.Max.X, bounds.Max.Y, bounds.Min.X, bounds.Min.Y
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}

	const minPadding = 80
	if minX < minPadding || minY < minPadding || bounds.Max.X-1-maxX < minPadding || bounds.Max.Y-1-maxY < minPadding {
		t.Fatalf("app icon artwork must stay inside the macOS safe area, opaque bounds=(%d,%d)-(%d,%d)", minX, minY, maxX, maxY)
	}
}
