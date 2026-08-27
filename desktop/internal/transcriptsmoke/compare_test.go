package transcriptsmoke

import (
	"image"
	"image/color"
	"testing"
)

func TestComparePixelContract(t *testing.T) {
	options := CompareOptions{
		ChannelThreshold: 12, MaxChangedRatio: 0.0005, MaxConnectedComponent: 128,
		ViewportWidth: 100, ViewportHeight: 100,
	}
	crop := Rect{Left: 0, Top: 0, Right: 100, Bottom: 100}

	t.Run("identical", func(t *testing.T) {
		base := solidImage(100, 100)
		result, _, err := Compare(base, base, crop, nil, options)
		if err != nil || !result.Passed || result.ChangedPixels != 0 {
			t.Fatalf("unexpected result: %+v err=%v", result, err)
		}
	})

	t.Run("masked selection", func(t *testing.T) {
		base := solidImage(100, 100)
		current := solidImage(100, 100)
		paint(current, image.Rect(10, 10, 30, 30))
		result, _, err := Compare(base, current, crop, []Rect{{Left: 9, Top: 9, Right: 31, Bottom: 31}}, options)
		if err != nil || !result.Passed || result.ChangedPixels != 0 {
			t.Fatalf("masked pixels changed: %+v err=%v", result, err)
		}
	})

	t.Run("ratio", func(t *testing.T) {
		base := solidImage(100, 100)
		current := solidImage(100, 100)
		for index := range 6 {
			current.SetRGBA(index*10, index*10, color.RGBA{R: 255, A: 255})
		}
		result, _, err := Compare(base, current, crop, nil, options)
		if err != nil || result.Passed || result.ChangedRatio <= options.MaxChangedRatio {
			t.Fatalf("ratio gate did not fail: %+v err=%v", result, err)
		}
	})

	t.Run("connected component", func(t *testing.T) {
		base := solidImage(100, 100)
		current := solidImage(100, 100)
		paint(current, image.Rect(10, 10, 23, 20))
		wideRatio := options
		wideRatio.MaxChangedRatio = 1
		result, _, err := Compare(base, current, crop, nil, wideRatio)
		if err != nil || result.Passed || result.LargestComponent != 130 {
			t.Fatalf("component gate did not fail: %+v err=%v", result, err)
		}
	})
}

func solidImage(width, height int) *image.RGBA {
	value := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			value.SetRGBA(x, y, color.RGBA{R: 20, G: 30, B: 40, A: 255})
		}
	}
	return value
}

func paint(target *image.RGBA, bounds image.Rectangle) {
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			target.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
		}
	}
}
