package agent

import "sync"

// ── OPT-144: CacheAdmissionController (缓存准入控制器) ──
// 决定哪些内容值得缓存。默认准入逻辑为:
//   estimatedSavings >= minTokenSavings AND frequency >= 2
// 可通过 AddRule 为特定 key 设置显式规则，显式规则优先于默认逻辑。

// CacheAdmissionController 缓存准入控制器，决定哪些内容值得缓存。
type CacheAdmissionController struct {
	mu              sync.RWMutex
	totalRequests   int
	totalAdmitted   int
	totalRejected   int
	admissionRules  map[string]bool
	minTokenSavings int
}

// NewCacheAdmissionController 创建一个新的缓存准入控制器。
// minSavings 为准入所需的最小 token 节省量。
func NewCacheAdmissionController(minSavings int) *CacheAdmissionController {
	return &CacheAdmissionController{
		admissionRules:  make(map[string]bool),
		minTokenSavings: minSavings,
	}
}

// RequestAdmission 决定是否准入缓存。
// 若存在该 key 的显式规则，则以规则结果为准；
// 否则使用默认逻辑: estimatedSavings >= minTokenSavings AND frequency >= 2。
// 每次调用递增 totalRequests，并根据结果递增 totalAdmitted 或 totalRejected。
func (c *CacheAdmissionController) RequestAdmission(key string, estimatedSavings int, frequency int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalRequests++

	var admitted bool
	if allowed, ok := c.admissionRules[key]; ok {
		admitted = allowed
	} else {
		admitted = estimatedSavings >= c.minTokenSavings && frequency >= 2
	}

	if admitted {
		c.totalAdmitted++
	} else {
		c.totalRejected++
	}

	return admitted
}

// AddRule 为指定 key 添加准入规则。
// allowed 为 true 表示强制准入，false 表示强制拒绝。
func (c *CacheAdmissionController) AddRule(key string, allowed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.admissionRules[key] = allowed
}

// GetAdmissionRate 获取准入率 (totalAdmitted / totalRequests)。
// 若 totalRequests 为 0 则返回 0。
func (c *CacheAdmissionController) GetAdmissionRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.totalRequests == 0 {
		return 0
	}
	return float64(c.totalAdmitted) / float64(c.totalRequests)
}

// GetStats 返回控制器的统计信息。
// 包含: totalRequests, totalAdmitted, totalRejected, admissionRate, minTokenSavings, ruleCount。
func (c *CacheAdmissionController) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var admissionRate float64
	if c.totalRequests == 0 {
		admissionRate = 0
	} else {
		admissionRate = float64(c.totalAdmitted) / float64(c.totalRequests)
	}

	return map[string]interface{}{
		"totalRequests":   c.totalRequests,
		"totalAdmitted":   c.totalAdmitted,
		"totalRejected":   c.totalRejected,
		"admissionRate":   admissionRate,
		"minTokenSavings": c.minTokenSavings,
		"ruleCount":       len(c.admissionRules),
	}
}

// Reset 重置控制器，清空所有统计计数和准入规则。
func (c *CacheAdmissionController) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalRequests = 0
	c.totalAdmitted = 0
	c.totalRejected = 0
	c.admissionRules = make(map[string]bool)
}

// cacCheckAdmission 检查是否满足默认准入条件。
// 条件: estimatedSavings >= minSavings AND frequency >= 2。
func cacCheckAdmission(estimatedSavings, minSavings, frequency int) bool {
	return estimatedSavings >= minSavings && frequency >= 2
}
