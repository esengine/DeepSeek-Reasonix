package agent

import "sync"

// ── OPT-125: ContextBoundaryDetector (上下文边界检测器) ──
// 检测上下文消息序列中的自然分割点，为上下文压缩、分段摘要
// 和消息保留决策提供边界信息。
//
// 边界判定依据：
//   - 话题转换：相邻消息的 Jaccard 词集相似度 < 0.3
//   - 长度突变：相邻消息长度差超过 3 倍
//
// 满足任一条件即判定为边界。

// ContextBoundaryDetector 上下文边界检测器，检测上下文中的自然分割点。
type ContextBoundaryDetector struct {
	mu                   sync.RWMutex
	totalDetections      int
	totalBoundaries      int
	avgBoundaryDistance  float64
	lastBoundaryPosition int
}

// NewContextBoundaryDetector 创建一个新的上下文边界检测器实例。
func NewContextBoundaryDetector() *ContextBoundaryDetector {
	return &ContextBoundaryDetector{
		lastBoundaryPosition: -1,
	}
}

// DetectBoundaries 检测消息间的自然边界位置。
// 遍历相邻消息对，依据话题转换（Jaccard 相似度 < 0.3）
// 和长度突变（长度差 > 3 倍）判定是否存在边界。
// 返回边界索引列表，其中每个索引表示该位置的消息与前一条消息之间存在边界。
func (d *ContextBoundaryDetector) DetectBoundaries(messages []string) []int {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.totalDetections++

	var boundaries []int
	for i := 1; i < len(messages); i++ {
		if cbdIsBoundary(messages[i-1], messages[i]) {
			boundaries = append(boundaries, i)
		}
	}

	boundaryCount := len(boundaries)
	d.totalBoundaries += boundaryCount

	// 计算本次检测的平均边界距离（消息总数 / (边界数 + 1)）
	var localAvg float64
	if boundaryCount > 0 {
		localAvg = float64(len(messages)) / float64(boundaryCount+1)
	}

	// 更新跨检测的运行平均边界距离
	if d.totalDetections == 1 {
		d.avgBoundaryDistance = localAvg
	} else {
		d.avgBoundaryDistance = (d.avgBoundaryDistance*float64(d.totalDetections-1) + localAvg) / float64(d.totalDetections)
	}

	// 更新最后边界位置
	if boundaryCount > 0 {
		d.lastBoundaryPosition = boundaries[boundaryCount-1]
	} else {
		d.lastBoundaryPosition = -1
	}

	return boundaries
}

// IsBoundary 判断两消息间是否存在自然边界。
// 判定条件：Jaccard 相似度 < 0.3 或长度差超过 3 倍。
func (d *ContextBoundaryDetector) IsBoundary(prevMsg string, currMsg string) bool {
	return cbdIsBoundary(prevMsg, currMsg)
}

// GetStats 返回检测器的统计信息，包括总检测次数、总边界数、
// 平均边界距离和最后边界位置。
func (d *ContextBoundaryDetector) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["totalDetections"] = d.totalDetections
	stats["totalBoundaries"] = d.totalBoundaries
	stats["avgBoundaryDistance"] = d.avgBoundaryDistance
	stats["lastBoundaryPosition"] = d.lastBoundaryPosition

	return stats
}

// Reset 重置检测器的所有统计数据。
func (d *ContextBoundaryDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.totalDetections = 0
	d.totalBoundaries = 0
	d.avgBoundaryDistance = 0
	d.lastBoundaryPosition = -1
}

// ── 辅助函数（cbd 前缀）──

// cbdIsBoundary 判断两消息间是否存在边界。
// Jaccard 相似度 < 0.3 或长度差 > 3 倍时返回 true。
func cbdIsBoundary(prevMsg string, currMsg string) bool {
	// 长度突变检测
	if cbdHasLengthMutation(prevMsg, currMsg) {
		return true
	}

	// 话题转换检测（Jaccard 词集相似度）
	similarity := cbdJaccardSimilarity(cbdTokenize(prevMsg), cbdTokenize(currMsg))
	if similarity < 0.3 {
		return true
	}

	return false
}

// cbdTokenize 将字符串分词为小写单词集合（用于 Jaccard 相似度计算）。
func cbdTokenize(s string) map[string]bool {
	set := make(map[string]bool)
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if start >= 0 {
				word := cbdCleanWord(s[start:i])
				if word != "" {
					set[cbdToLower(word)] = true
				}
				start = -1
			}
		} else {
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		word := cbdCleanWord(s[start:])
		if word != "" {
			set[cbdToLower(word)] = true
		}
	}
	return set
}

// cbdJaccardSimilarity 计算两个词集合的 Jaccard 相似度。
// Jaccard = |交集| / |并集|。两个空集的相似度为 1.0。
func cbdJaccardSimilarity(set1, set2 map[string]bool) float64 {
	if len(set1) == 0 && len(set2) == 0 {
		return 1.0
	}

	intersection := 0
	for k := range set1 {
		if set2[k] {
			intersection++
		}
	}

	union := len(set1) + len(set2) - intersection
	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

// cbdHasLengthMutation 判断两消息长度差是否超过 3 倍。
func cbdHasLengthMutation(prevMsg string, currMsg string) bool {
	prevLen := len(prevMsg)
	currLen := len(currMsg)

	if prevLen == 0 && currLen == 0 {
		return false
	}
	if prevLen == 0 || currLen == 0 {
		return true
	}
	if prevLen > currLen*3 {
		return true
	}
	if currLen > prevLen*3 {
		return true
	}
	return false
}

// cbdCleanWord 去除单词首尾的非字母数字字符。
func cbdCleanWord(s string) string {
	start := 0
	end := len(s)
	for start < end && !cbdIsAlnum(s[start]) {
		start++
	}
	for end > start && !cbdIsAlnum(s[end-1]) {
		end--
	}
	return s[start:end]
}

// cbdIsAlnum 判断字节是否为 ASCII 字母或数字。
func cbdIsAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// cbdToLower 将字符串转换为小写（仅处理 ASCII 字母）。
func cbdToLower(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}
