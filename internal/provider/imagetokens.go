package provider

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"

	_ "golang.org/x/image/webp"
)

const (
	visionPatchSize      = 14
	visionDownsample     = 3
	maxDeepSeekImageTok  = 1024
	minDeepSeekPixels    = 544 * 544
	visionCellSize       = visionPatchSize * visionDownsample
	maxRequestImageEdge  = 4096
	ImageOffloadedRef    = "image-offloaded"
	offloadedImageNotice = "[image omitted: dropped from the model request to stay within the vision budget]"
)

// OffloadedImageNotice is the model-visible placeholder for a dropped image.
func OffloadedImageNotice() string { return offloadedImageNotice }

type imageGrid struct {
	gridHeight int
	gridWidth  int
	bestHeight int
	bestWidth  int
	numTokens  int
}

func ceilDiv(value, divisor int) int {
	if divisor <= 0 {
		return 0
	}
	return (value + divisor - 1) / divisor
}

func gridTokens(gridHeight, gridWidth int) int {
	return gridHeight*(gridWidth+1) + 2
}

func gridCells(paddedLength int) int {
	return ceilDiv(paddedLength/visionPatchSize, visionDownsample)
}

func longEdgeDimensions(srcW, srcH, longEdge int) (int, int) {
	if srcW <= 0 || srcH <= 0 || longEdge <= 0 {
		return 1, 1
	}
	if srcW >= srcH {
		h := max(int(math.Round(float64(srcH)*float64(longEdge)/float64(srcW))), 1)
		return longEdge, h
	}
	w := max(int(math.Round(float64(srcW)*float64(longEdge)/float64(srcH))), 1)
	return w, longEdge
}

func solveResizeRatio(height, width, budget int) imageGrid {
	aspect := float64(height) / float64(width)
	idealGridWidth := math.Sqrt((float64(budget)-2)/aspect+0.25) - 0.5
	idealGridHeight := idealGridWidth * aspect
	var bestHeight, bestWidth int
	switch {
	case idealGridWidth < 1:
		solvedGridWidth := 1
		solvedGridHeight := (budget - 2) / (solvedGridWidth + 1)
		bestWidth = solvedGridWidth * visionCellSize
		bestHeight = solvedGridHeight * visionCellSize
	case idealGridHeight < 1:
		solvedGridHeight := 1
		solvedGridWidth := (budget-2)/solvedGridHeight - 1
		bestWidth = solvedGridWidth * visionCellSize
		bestHeight = solvedGridHeight * visionCellSize
	default:
		solvedGridWidth := int(idealGridWidth)
		solvedGridHeight := int(idealGridHeight)
		scale := min(float64(solvedGridWidth*visionCellSize)/float64(width), float64(solvedGridHeight*visionCellSize)/float64(height))
		bestWidth = int(float64(width)*scale/float64(visionPatchSize)) * visionPatchSize
		bestHeight = int(float64(height)*scale/float64(visionPatchSize)) * visionPatchSize
	}
	return imageGrid{
		gridHeight: gridCells(bestHeight),
		gridWidth:  gridCells(bestWidth),
		bestHeight: bestHeight,
		bestWidth:  bestWidth,
		numTokens:  gridTokens(gridCells(bestHeight), gridCells(bestWidth)),
	}
}

func safeResize(height, width, paddedHeight, paddedWidth int) imageGrid {
	direct := imageGrid{
		gridHeight: gridCells(paddedHeight),
		gridWidth:  gridCells(paddedWidth),
		bestHeight: paddedHeight,
		bestWidth:  paddedWidth,
		numTokens:  gridTokens(gridCells(paddedHeight), gridCells(paddedWidth)),
	}
	if direct.numTokens <= maxDeepSeekImageTok {
		return direct
	}
	return solveResizeRatio(height, width, maxDeepSeekImageTok)
}

func resizeOnce(width, height int) imageGrid {
	scaledWidth, scaledHeight := width, height
	pixels := scaledWidth * scaledHeight
	if pixels > 0 && pixels < minDeepSeekPixels {
		scale := math.Sqrt(float64(minDeepSeekPixels) / float64(pixels))
		scaledWidth = int(float64(scaledWidth) * scale)
		scaledHeight = int(float64(scaledHeight) * scale)
	}
	paddedWidth := ceilDiv(scaledWidth, visionPatchSize) * visionPatchSize
	paddedHeight := ceilDiv(scaledHeight, visionPatchSize) * visionPatchSize
	return safeResize(scaledHeight, scaledWidth, paddedHeight, paddedWidth)
}

func sameGrid(a, b imageGrid) bool {
	return a.gridHeight == b.gridHeight && a.gridWidth == b.gridWidth &&
		a.bestHeight == b.bestHeight && a.bestWidth == b.bestWidth && a.numTokens == b.numTokens
}

// DeepSeekImageTokens is the official v41 vision-token price for one request
// image of the given pixel size, capped at 1024.
func DeepSeekImageTokens(width, height int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	result := resizeOnce(width, height)
	for range 9 {
		next := resizeOnce(result.bestWidth, result.bestHeight)
		if sameGrid(next, result) {
			if result.numTokens > maxDeepSeekImageTok {
				return maxDeepSeekImageTok
			}
			return result.numTokens
		}
		result = next
	}
	if result.numTokens > maxDeepSeekImageTok {
		return maxDeepSeekImageTok
	}
	return result.numTokens
}

// DeepSeekRequestImageDimensions is the size Reasonix should encode so the
// provider keeps the whole image under the v41 token grid. Small images are
// not enlarged. The long edge is then clamped to 4096.
func DeepSeekRequestImageDimensions(width, height int) (int, int) {
	if width <= 0 || height <= 0 {
		return width, height
	}
	paddedWidth := ceilDiv(width, visionPatchSize) * visionPatchSize
	paddedHeight := ceilDiv(height, visionPatchSize) * visionPatchSize
	if gridTokens(gridCells(paddedHeight), gridCells(paddedWidth)) <= maxDeepSeekImageTok {
		return clampRequestEdge(width, height)
	}
	solved := solveResizeRatio(height, width, maxDeepSeekImageTok)
	long := solved.bestWidth
	if height > width {
		long = solved.bestHeight
	}
	outW, outH := longEdgeDimensions(width, height, long)
	return clampRequestEdge(outW, outH)
}

func clampRequestEdge(width, height int) (int, int) {
	if width <= maxRequestImageEdge && height <= maxRequestImageEdge {
		return width, height
	}
	return longEdgeDimensions(width, height, maxRequestImageEdge)
}

// ImagePixelSize reports decoded pixel size for a data-URL image reference.
func ImagePixelSize(ref string) (width, height int, ok bool) {
	media, payload, ok := ParseImageDataURL(ref)
	if !ok {
		return 0, 0, false
	}
	switch strings.ToLower(media) {
	case "image/png", "image/jpeg", "image/jpg", "image/gif", "image/webp":
	default:
		return 0, 0, false
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(raw) == 0 {
		return 0, 0, false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// EstimateImageTokens prices one stored image reference. Unknown dimensions
// use the 1024-token cap. Offloaded or empty slots are free.
func EstimateImageTokens(ref string) int {
	switch ClassifyImage(ref) {
	case ImageNone:
		return 0
	case ImageDataURL:
		w, h, ok := ImagePixelSize(ref)
		if !ok {
			return maxDeepSeekImageTok
		}
		return DeepSeekImageTokens(w, h)
	default:
		return maxDeepSeekImageTok
	}
}

// EstimateMessageImageTokens sums v41 vision tokens for a message.
func EstimateMessageImageTokens(msg Message) int {
	total := 0
	for _, ref := range msg.Images {
		total += EstimateImageTokens(ref)
	}
	return total
}
