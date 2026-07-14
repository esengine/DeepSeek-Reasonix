package agent
import "sync"

// OPT-194: TokenAwareRateLimiter / Token感知速率限制器
// 基于token消耗速率限制请求，支持滑动窗口重置。

// TokenAwareRateLimiter Token感知速率限制器，基于token消耗速率限制请求
type TokenAwareRateLimiter struct {
	mu                    sync.RWMutex
	maxTokensPerWindow    int
	windowSize            int
	tokensInCurrentWindow int
	currentWindow         int
	allowedCount          int
	deniedCount           int
}

// NewTokenAwareRateLimiter 创建一个新的Token感知速率限制器
func NewTokenAwareRateLimiter(maxTokens int, windowSize int) *TokenAwareRateLimiter {
	return &TokenAwareRateLimiter{
		maxTokensPerWindow: maxTokens,
		windowSize:         windowSize,
	}
}

// TryAcquire 尝试获取token配额（窗口切换时重置）
func (r *TokenAwareRateLimiter) TryAcquire(tokens int, currentWindow int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	// 窗口切换时重置已使用token数
	if currentWindow != r.currentWindow {
		r.currentWindow = currentWindow
		r.tokensInCurrentWindow = 0
	}
	if r.tokensInCurrentWindow+tokens > r.maxTokensPerWindow {
		r.deniedCount++
		return false
	}
	r.tokensInCurrentWindow += tokens
	r.allowedCount++
	return true
}

// GetCurrentUsage 获取当前窗口使用量
func (r *TokenAwareRateLimiter) GetCurrentUsage() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tokensInCurrentWindow
}

// GetUtilization 获取当前窗口利用率
func (r *TokenAwareRateLimiter) GetUtilization() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return tarlComputeUtilization(r.tokensInCurrentWindow, r.maxTokensPerWindow)
}

// GetStats 返回统计信息
func (r *TokenAwareRateLimiter) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return map[string]interface{}{
		"maxTokensPerWindow":    r.maxTokensPerWindow,
		"windowSize":            r.windowSize,
		"tokensInCurrentWindow": r.tokensInCurrentWindow,
		"allowedCount":          r.allowedCount,
		"deniedCount":           r.deniedCount,
		"utilization":           tarlComputeUtilization(r.tokensInCurrentWindow, r.maxTokensPerWindow),
	}
}

// Reset 重置速率限制器
func (r *TokenAwareRateLimiter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokensInCurrentWindow = 0
	r.currentWindow = 0
	r.allowedCount = 0
	r.deniedCount = 0
}

// tarlComputeUtilization 辅助函数，计算利用率
func tarlComputeUtilization(used, max int) float64 {
	if max <= 0 {
		return 0.0
	}
	return float64(used) / float64(max)
}
