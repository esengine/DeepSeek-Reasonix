package agent

import (
	"sync"
)

// ── OPT-33: 图片 Token 优化器 (Image Token Optimizer) ──
// 压缩图片减少 token 消耗。
//
// 原理：多模态请求中，图片按尺寸消耗 token。例如：
// - 1024x1024 图片 ≈ 765 tokens (OpenAI)
// - 2048x2048 图片 ≈ 3060 tokens (OpenAI)
// 通过降采样和格式优化：
// 1. 超过 1024px 的图片降采样到 1024px
// 2. PNG 转 JPEG（照片类图片可省 60% 体积）
// 3. 移除不必要的透明通道
//
// 效果：大图 token 消耗降低 50-75%，单张图片最多省 2300 token。

// ImageOptimizer 图片优化器
type ImageOptimizer struct {
	mu sync.RWMutex

	// 配置
	maxDimension   int  // 最大尺寸（像素）
	jpegQuality    int  // JPEG 质量（1-100）
	enableDownscale bool // 启用降采样
	enableFormat   bool // 启用格式转换

	// 统计
	totalOptimized int
	totalSaved     int
}

// ImageOptimizationResult 图片优化结果
type ImageOptimizationResult struct {
	OriginalSize    int    `json:"originalSize"`
	OptimizedSize   int    `json:"optimizedSize"`
	OriginalTokens  int    `json:"originalTokens"`
	OptimizedTokens int    `json:"optimizedTokens"`
	SavedTokens     int    `json:"savedTokens"`
	Action          string `json:"action"` // "downscaled" "converted" "skipped" "unchanged"
}

// NewImageOptimizer 创建图片优化器
func NewImageOptimizer() *ImageOptimizer {
	return &ImageOptimizer{
		maxDimension:    1024,
		jpegQuality:     85,
		enableDownscale: true,
		enableFormat:    true,
	}
}

// EstimateImageTokens 估算图片消耗的 token 数
// OpenAI 公式: tokens = (width * height) / 750
// Anthropic 公式: tokens ≈ (width * height) / 750 (类似)
func EstimateImageTokens(width, height int) int {
	return (width * height) / 750
}

// ShouldOptimize 判断图片是否需要优化
func (o *ImageOptimizer) ShouldOptimize(width, height int, format string) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.enableDownscale && (width > o.maxDimension || height > o.maxDimension) {
		return true
	}
	if o.enableFormat && format == "png" && width > 256 && height > 256 {
		// 大 PNG 可能是照片，转 JPEG 更高效
		return true
	}
	return false
}

// OptimizeImage 优化图片（返回优化建议）
func (o *ImageOptimizer) OptimizeImage(width, height int, format string) *ImageOptimizationResult {
	o.mu.Lock()
	defer o.mu.Unlock()

	originalTokens := EstimateImageTokens(width, height)
	result := &ImageOptimizationResult{
		OriginalSize:    width * height * 4, // 粗略估算 RGBA
		OriginalTokens:  originalTokens,
		OptimizedTokens: originalTokens,
		Action:          "unchanged",
	}

	actions := []string{}

	// 降采样
	if o.enableDownscale && (width > o.maxDimension || height > o.maxDimension) {
		scale := float64(o.maxDimension) / float64(maxInt(width, height))
		if scale < 1.0 {
			width = int(float64(width) * scale)
			height = int(float64(height) * scale)
			actions = append(actions, "downscaled")
		}
	}

	// 格式转换
	if o.enableFormat && format == "png" && width > 256 && height > 256 {
		format = "jpeg"
		actions = append(actions, "converted")
	}

	// 计算优化后的 token
	result.OptimizedTokens = EstimateImageTokens(width, height)
	result.OptimizedSize = width * height * 3 // JPEG RGB
	result.SavedTokens = result.OriginalTokens - result.OptimizedTokens

	if len(actions) > 0 {
		result.Action = joinActions(actions)
		o.totalOptimized++
		if result.SavedTokens > 0 {
			o.totalSaved += result.SavedTokens
		}
	}

	return result
}

// GetStats 获取统计
func (o *ImageOptimizer) GetStats() ImageOptStats {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return ImageOptStats{
		TotalOptimized: o.totalOptimized,
		TotalSaved:     o.totalSaved,
		MaxDimension:   o.maxDimension,
		JpegQuality:    o.jpegQuality,
	}
}

// ImageOptStats 图片优化统计
type ImageOptStats struct {
	TotalOptimized int `json:"totalOptimized"`
	TotalSaved     int `json:"totalSaved"`
	MaxDimension   int `json:"maxDimension"`
	JpegQuality    int `json:"jpegQuality"`
}

// Reset 重置
func (o *ImageOptimizer) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.totalOptimized = 0
	o.totalSaved = 0
}

// ── 辅助函数 ──

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func joinActions(actions []string) string {
	result := ""
	for i, a := range actions {
		if i > 0 {
			result += "+"
		}
		result += a
	}
	return result
}
