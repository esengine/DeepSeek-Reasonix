package agent
import "sync"

// ── OPT-220: PromptCacheOptimizationAdvisor (提示缓存优化顾问) ──
// 根据缓存命中率、未命中率与平均延迟分析缓存表现，生成优化建议并按类别归档。
// 支持标记建议已实施，累计建议数、已实施数与预估影响，便于跟踪优化进展。

// PromptCacheOptimizationAdvisor 提示缓存优化顾问，提供缓存优化建议。
type PromptCacheOptimizationAdvisor struct {
	mu               sync.RWMutex
	advice           map[string]string // category → advice
	adviceCount      int               // 建议总数
	implementedCount int               // 已实施建议数
	totalImpact      float64           // 累计预估影响
}

// NewPromptCacheOptimizationAdvisor 创建一个新的提示缓存优化顾问。
func NewPromptCacheOptimizationAdvisor() *PromptCacheOptimizationAdvisor {
	return &PromptCacheOptimizationAdvisor{
		advice: make(map[string]string),
	}
}

// Analyze 分析缓存表现，根据 hitRate、missRate 与 avgLatency 生成优化建议，
// 将建议归档到对应类别，累加预估影响，并返回建议文本。
func (a *PromptCacheOptimizationAdvisor) Analyze(hitRate float64, missRate float64, avgLatency int) string {
	category, advice, impact := pcoaGenerateAdvice(hitRate, missRate, avgLatency)

	a.mu.Lock()
	defer a.mu.Unlock()

	// 仅在新增类别时递增 adviceCount，避免重复统计。
	if _, exists := a.advice[category]; !exists {
		a.adviceCount++
	}
	a.advice[category] = advice
	a.totalImpact += impact
	return advice
}

// GetAdvice 获取指定类别的建议。若类别不存在则返回空字符串。
func (a *PromptCacheOptimizationAdvisor) GetAdvice(category string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.advice[category]
}

// MarkImplemented 标记指定类别的建议已实施。
// 仅当该类别存在建议时递增 implementedCount。
func (a *PromptCacheOptimizationAdvisor) MarkImplemented(category string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, ok := a.advice[category]; !ok {
		return
	}
	a.implementedCount++
}

// GetStats 返回顾问的统计信息。
// 包含: adviceCount, implementedCount, totalImpact, categories。
func (a *PromptCacheOptimizationAdvisor) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	categories := make([]string, 0, len(a.advice))
	for c := range a.advice {
		categories = append(categories, c)
	}
	return map[string]interface{}{
		"adviceCount":      a.adviceCount,
		"implementedCount": a.implementedCount,
		"totalImpact":      a.totalImpact,
		"categories":       categories,
	}
}

// Reset 重置顾问，清空所有建议与统计。
func (a *PromptCacheOptimizationAdvisor) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.advice = make(map[string]string)
	a.adviceCount = 0
	a.implementedCount = 0
	a.totalImpact = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 pcoa 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// pcoaGenerateAdvice 根据缓存表现生成优化建议。
// 返回 (类别, 建议文本, 预估影响)。优先处理最严重的问题维度。
func pcoaGenerateAdvice(hitRate float64, missRate float64, avgLatency int) (string, string, float64) {
	// 低命中率优先：建议优化缓存键与前缀稳定性。
	if hitRate < 0.3 {
		impact := (0.6 - hitRate) * 100
		if impact < 0 {
			impact = 0
		}
		return "hitRate", "命中率过低，建议优化缓存键设计并稳定提示前缀以提升复用", impact
	}
	// 高未命中率：建议排查未命中原因并预热缓存。
	if missRate > 0.5 {
		impact := (missRate - 0.5) * 100
		if impact < 0 {
			impact = 0
		}
		return "missRate", "未命中率偏高，建议排查未命中模式并对高频提示进行预热", impact
	}
	// 高延迟：建议增大缓存容量或缩短失效窗口。
	if avgLatency > 500 {
		impact := float64(avgLatency-500) / 10.0
		return "latency", "平均延迟较高，建议增大缓存容量并收紧失效窗口", impact
	}
	// 表现良好：维持现状。
	return "healthy", "缓存表现良好，建议维持现有策略并持续监控", 0
}
