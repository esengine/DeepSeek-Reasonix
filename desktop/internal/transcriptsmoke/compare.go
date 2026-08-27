package transcriptsmoke

import (
	"fmt"
	"image"
	"image/color"
	"slices"
)

type Rect struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

func (rect Rect) Expand(pixels float64) Rect {
	return Rect{
		Left: rect.Left - pixels, Top: rect.Top - pixels,
		Right: rect.Right + pixels, Bottom: rect.Bottom + pixels,
	}
}

type CompareOptions struct {
	ChannelThreshold      uint8
	MaxChangedRatio       float64
	MaxConnectedComponent int
	ViewportWidth         float64
	ViewportHeight        float64
}

type CompareResult struct {
	ChangedPixels    int     `json:"changedPixels"`
	ComparedPixels   int     `json:"comparedPixels"`
	ChangedRatio     float64 `json:"changedRatio"`
	LargestComponent int     `json:"largestComponent"`
	Passed           bool    `json:"passed"`
}

func Compare(base, current image.Image, crop Rect, masks []Rect, options CompareOptions) (CompareResult, *image.RGBA, error) {
	if base.Bounds() != current.Bounds() {
		return CompareResult{}, nil, fmt.Errorf("capture bounds changed: %v -> %v", base.Bounds(), current.Bounds())
	}
	if options.ViewportWidth <= 0 || options.ViewportHeight <= 0 {
		return CompareResult{}, nil, fmt.Errorf("invalid viewport %.1fx%.1f", options.ViewportWidth, options.ViewportHeight)
	}
	bounds := base.Bounds()
	scaleX := float64(bounds.Dx()) / options.ViewportWidth
	scaleY := float64(bounds.Dy()) / options.ViewportHeight
	cropBounds := image.Rect(
		bounds.Min.X+int(crop.Left*scaleX),
		bounds.Min.Y+int(crop.Top*scaleY),
		bounds.Min.X+int(crop.Right*scaleX+0.999),
		bounds.Min.Y+int(crop.Bottom*scaleY+0.999),
	).Intersect(bounds)
	if cropBounds.Empty() {
		return CompareResult{}, nil, fmt.Errorf("empty comparison crop: %+v in %v", crop, bounds)
	}
	maskBounds := make([]image.Rectangle, 0, len(masks))
	for _, mask := range masks {
		maskBounds = append(maskBounds, image.Rect(
			bounds.Min.X+int(mask.Left*scaleX),
			bounds.Min.Y+int(mask.Top*scaleY),
			bounds.Min.X+int(mask.Right*scaleX+0.999),
			bounds.Min.Y+int(mask.Bottom*scaleY+0.999),
		))
	}

	width := cropBounds.Dx()
	height := cropBounds.Dy()
	changed := make([]bool, width*height)
	diff := image.NewRGBA(image.Rect(0, 0, width, height))
	result := CompareResult{}
	for y := cropBounds.Min.Y; y < cropBounds.Max.Y; y++ {
		for x := cropBounds.Min.X; x < cropBounds.Max.X; x++ {
			masked := slices.ContainsFunc(maskBounds, func(mask image.Rectangle) bool {
				return image.Pt(x, y).In(mask)
			})
			if masked {
				continue
			}
			result.ComparedPixels++
			if pixelChanged(base.At(x, y), current.At(x, y), options.ChannelThreshold) {
				index := (y-cropBounds.Min.Y)*width + x - cropBounds.Min.X
				changed[index] = true
				result.ChangedPixels++
				diff.SetRGBA(x-cropBounds.Min.X, y-cropBounds.Min.Y, color.RGBA{R: 255, A: 255})
			}
		}
	}
	if result.ComparedPixels > 0 {
		result.ChangedRatio = float64(result.ChangedPixels) / float64(result.ComparedPixels)
	}
	result.LargestComponent = largestComponent(changed, width, height)
	result.Passed = result.ChangedRatio <= options.MaxChangedRatio &&
		result.LargestComponent <= options.MaxConnectedComponent
	return result, diff, nil
}

func pixelChanged(left, right color.Color, threshold uint8) bool {
	lr, lg, lb, _ := left.RGBA()
	rr, rg, rb, _ := right.RGBA()
	limit := uint32(threshold) * 257
	return channelDelta(lr, rr) > limit || channelDelta(lg, rg) > limit || channelDelta(lb, rb) > limit
}

func channelDelta(left, right uint32) uint32 {
	if left > right {
		return left - right
	}
	return right - left
}

func largestComponent(changed []bool, width, height int) int {
	visited := make([]bool, len(changed))
	largest := 0
	queue := make([]int, 0, 256)
	for start, value := range changed {
		if !value || visited[start] {
			continue
		}
		visited[start] = true
		queue = append(queue[:0], start)
		size := 0
		for len(queue) > 0 {
			index := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			size++
			x := index % width
			y := index / width
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					nx, ny := x+dx, y+dy
					if nx < 0 || nx >= width || ny < 0 || ny >= height {
						continue
					}
					next := ny*width + nx
					if changed[next] && !visited[next] {
						visited[next] = true
						queue = append(queue, next)
					}
				}
			}
		}
		if size > largest {
			largest = size
		}
	}
	return largest
}
