package agent

import "sync"

// OPT-197: CacheAdmissionControllerV2 / 缓存准入控制器V2
// 基于多维度评分（频率、新近度、大小）决定是否将数据准入缓存。

// CacheAdmissionControllerV2 是缓存准入控制器V2。
type CacheAdmissionControllerV2 struct {
	mu             sync.RWMutex
	maxSize        int
	currentSize    int
	admittedCount  int
	rejectedCount  int
	scoringWeights map[string]float64
}

// NewCacheAdmissionControllerV2 创建一个新的CacheAdmissionControllerV2实例。
// 默认权重：frequency=0.4, recency=0.3, size=0.3
func NewCacheAdmissionControllerV2(maxSize int) *CacheAdmissionControllerV2 {
	return &CacheAdmissionControllerV2{
		maxSize:       maxSize,
		currentSize:   0,
		admittedCount: 0,
		rejectedCount: 0,
		scoringWeights: map[string]float64{
			"frequency": 0.4,
			"recency":   0.3,
			"size":      0.3,
		},
	}
}

// Score 根据频率、新近度和大小计算准入评分（0.0~1.0）。
func (c *CacheAdmissionControllerV2) Score(frequency int, recency int, size int) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return cac2ComputeScore(frequency, recency, size, c.scoringWeights)
}

// Admit 根据评分和剩余空间决定是否准入缓存。
func (c *CacheAdmissionControllerV2) Admit(key string, frequency int, recency int, size int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	score := cac2ComputeScore(frequency, recency, size, c.scoringWeights)
	// 评分阈值：大于0.5且有足够空间（或可通过驱逐腾出空间）才准入
	if score < 0.5 {
		c.rejectedCount++
		return false
	}
	// 检查空间是否足够，不足则尝试驱逐
	if c.currentSize+size > c.maxSize {
		if !c.evictLocked(size) {
			c.rejectedCount++
			return false
		}
	}
	c.currentSize += size
	c.admittedCount++
	return true
}

// Evict 驱逐以腾出指定大小的空间。
func (c *CacheAdmissionControllerV2) Evict(size int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.evictLocked(size)
}

// evictLocked 是Evict的内部实现，调用时已持有写锁。
func (c *CacheAdmissionControllerV2) evictLocked(size int) bool {
	needed := c.currentSize + size - c.maxSize
	if needed <= 0 {
		return true
	}
	// 简单策略：从当前大小中驱逐needed大小
	if c.currentSize >= needed {
		c.currentSize -= needed
		return true
	}
	return false
}

// GetStats 返回控制器的统计信息。
func (c *CacheAdmissionControllerV2) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"maxSize":       c.maxSize,
		"currentSize":   c.currentSize,
		"admittedCount": c.admittedCount,
		"rejectedCount": c.rejectedCount,
		"admissionRate": cac2ComputeAdmissionRate(c.admittedCount, c.rejectedCount),
	}
}

// Reset 重置控制器为初始状态。
func (c *CacheAdmissionControllerV2) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentSize = 0
	c.admittedCount = 0
	c.rejectedCount = 0
}

// cac2ComputeScore 根据多维度指标和权重计算准入评分。
// 评分公式：weight_freq * norm(freq) + weight_rec * norm(rec) + weight_size * norm(size)
func cac2ComputeScore(frequency int, recency int, size int, weights map[string]float64) float64 {
	wFreq := weights["frequency"]
	wRec := weights["recency"]
	wSize := weights["size"]

	// 归一化各维度（简单归一化，避免除零）
	normFreq := float64(frequency) / (float64(frequency) + 10.0)
	normRec := 1.0 / (1.0 + float64(recency))
	normSize := 1.0 / (1.0 + float64(size)/100.0)

	score := wFreq*normFreq + wRec*normRec + wSize*normSize
	if score > 1.0 {
		score = 1.0
	}
	if score < 0.0 {
		score = 0.0
	}
	return score
}

// cac2ComputeAdmissionRate 计算准入率。
func cac2ComputeAdmissionRate(admitted int, rejected int) float64 {
	total := admitted + rejected
	if total == 0 {
		return 0.0
	}
	return float64(admitted) / float64(total)
}
