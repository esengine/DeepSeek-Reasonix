package agent
import "sync"

// OPT-193: ContextSimilarityDetector / 上下文相似度检测器
// 检测上下文片段间的相似度以避免重复，使用Jaccard相似度算法。

// ContextSimilarityDetector 上下文相似度检测器，检测上下文片段间的相似度以避免重复
type ContextSimilarityDetector struct {
	mu              sync.RWMutex
	threshold       float64
	comparisons     int
	duplicatesFound int
	lastSimilarity  float64
}

// NewContextSimilarityDetector 创建一个新的上下文相似度检测器
func NewContextSimilarityDetector(threshold float64) *ContextSimilarityDetector {
	return &ContextSimilarityDetector{
		threshold: threshold,
	}
}

// Compare 计算两个字符串的相似度（使用Jaccard相似度）
func (d *ContextSimilarityDetector) Compare(a string, b string) float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.comparisons++
	tokensA := csdTokenize(a)
	tokensB := csdTokenize(b)
	similarity := csdJaccard(tokensA, tokensB)
	d.lastSimilarity = similarity
	return similarity
}

// IsDuplicate 相似度超过阈值时返回true
func (d *ContextSimilarityDetector) IsDuplicate(a string, b string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.comparisons++
	tokensA := csdTokenize(a)
	tokensB := csdTokenize(b)
	similarity := csdJaccard(tokensA, tokensB)
	d.lastSimilarity = similarity
	if similarity >= d.threshold {
		d.duplicatesFound++
		return true
	}
	return false
}

// FindDuplicates 找到所有重复对，返回索引对列表
func (d *ContextSimilarityDetector) FindDuplicates(items []string) [][]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	var pairs [][]int
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			d.comparisons++
			tokensA := csdTokenize(items[i])
			tokensB := csdTokenize(items[j])
			similarity := csdJaccard(tokensA, tokensB)
			d.lastSimilarity = similarity
			if similarity >= d.threshold {
				d.duplicatesFound++
				pairs = append(pairs, []int{i, j})
			}
		}
	}
	return pairs
}

// GetStats 返回统计信息，包括 threshold, comparisons, duplicatesFound, lastSimilarity
func (d *ContextSimilarityDetector) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return map[string]interface{}{
		"threshold":       d.threshold,
		"comparisons":     d.comparisons,
		"duplicatesFound": d.duplicatesFound,
		"lastSimilarity":  d.lastSimilarity,
	}
}

// Reset 重置检测器，清空所有统计
func (d *ContextSimilarityDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.comparisons = 0
	d.duplicatesFound = 0
	d.lastSimilarity = 0
}

// csdTokenize 辅助函数，将字符串分词为小写token列表
func csdTokenize(s string) []string {
	var tokens []string
	var word []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if len(word) > 0 {
				tokens = append(tokens, string(word))
				word = word[:0]
			}
		} else {
			// 大写转小写
			if c >= 'A' && c <= 'Z' {
				c = c + 32
			}
			word = append(word, c)
		}
	}
	if len(word) > 0 {
		tokens = append(tokens, string(word))
	}
	return tokens
}

// csdJaccard 辅助函数，计算两个token列表的Jaccard相似度
func csdJaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	setA := make(map[string]bool)
	for _, t := range a {
		setA[t] = true
	}
	setB := make(map[string]bool)
	for _, t := range b {
		setB[t] = true
	}
	intersection := 0
	for t := range setA {
		if setB[t] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}
