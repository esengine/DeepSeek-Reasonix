package agent

import "sync"

// ── OPT-109: TokenWasteDetector (Token 浪费检测器) ──
// 检测消息列表中多种 token 浪费模式，帮助识别可优化的上下文。
//
// 原理：对消息进行分词并估算 token 数，检测以下浪费类型：
//   - redundancy: 消息间重复内容
//   - verbosity: 过长消息（超过平均长度 2 倍的部分）
//   - boilerplate: 模板套话（常见礼貌/填充短语）
//   - whitespace: 多余空白字符
//
// 效果：量化浪费分布，为压缩与裁剪策略提供数据支撑。

// twdVerbosityBaselineTokens 是 DetectVerbosity 使用的基线平均 token 数。
// 单条消息 token 超过该基线 2 倍的部分视为浪费。
const twdVerbosityBaselineTokens = 50

// twdBoilerplatePhrases 是常见模板套话列表，命中即计入 boilerplate 浪费。
var twdBoilerplatePhrases = []string{
	"As an AI",
	"I'd be happy to help",
	"Please let me know",
	"If you have any questions",
	"Thank you for your question",
	"作为一个AI",
	"我很乐意帮助您",
	"如果您还有其他问题",
	"希望能帮到您",
	"感谢您的提问",
}

// TokenWasteDetector Token 浪费检测器。
type TokenWasteDetector struct {
	mu               sync.RWMutex
	totalChecks      int
	wasteDetected    int
	wasteByType      map[string]int
	totalWasteTokens int
}

// NewTokenWasteDetector 创建新的 Token 浪费检测器。
func NewTokenWasteDetector() *TokenWasteDetector {
	return &TokenWasteDetector{
		wasteByType: make(map[string]int),
	}
}

// Detect 检测多条消息中的各种浪费模式，返回各类型浪费的 token 数。
// 检测类型包括 redundancy、verbosity、boilerplate 和 whitespace。
// 每次调用递增 totalChecks，并更新累计统计。
func (d *TokenWasteDetector) Detect(messages []string) map[string]int {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.totalChecks++

	result := map[string]int{
		"redundancy":  0,
		"verbosity":   0,
		"boilerplate": 0,
		"whitespace":  0,
	}

	// redundancy: 消息间重复内容
	result["redundancy"] = twdDetectRedundancy(messages)

	// verbosity: 过长消息（超过平均长度 2 倍）
	if len(messages) > 0 {
		totalTokens := 0
		for _, msg := range messages {
			totalTokens += twdEstimateTokens(msg)
		}
		avg := totalTokens / len(messages)
		threshold := avg * 2
		if threshold < twdVerbosityBaselineTokens*2 {
			threshold = twdVerbosityBaselineTokens * 2
		}
		for _, msg := range messages {
			tokens := twdEstimateTokens(msg)
			if tokens > threshold {
				result["verbosity"] += tokens - threshold
			}
		}
	}

	// boilerplate: 模板套话
	for _, msg := range messages {
		result["boilerplate"] += twdDetectBoilerplate(msg)
	}

	// whitespace: 多余空白
	for _, msg := range messages {
		result["whitespace"] += twdDetectWhitespace(msg)
	}

	// 更新累计统计
	roundWaste := 0
	for _, v := range result {
		roundWaste += v
	}
	if roundWaste > 0 {
		d.wasteDetected++
	}
	for k, v := range result {
		d.wasteByType[k] += v
	}
	d.totalWasteTokens += roundWaste

	return result
}

// DetectRedundancy 检测消息间的重复内容，返回浪费的 token 数。
// 通过 Jaccard 相似度比较每对消息，高相似度的部分计入浪费。
func (d *TokenWasteDetector) DetectRedundancy(messages []string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return twdDetectRedundancy(messages)
}

// DetectVerbosity 检测单条消息是否过长。
// token 数超过基线平均 2 倍的部分视为浪费。
func (d *TokenWasteDetector) DetectVerbosity(message string) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	tokens := twdEstimateTokens(message)
	threshold := twdVerbosityBaselineTokens * 2
	if tokens > threshold {
		return tokens - threshold
	}
	return 0
}

// GetStats 返回检测器统计信息，包括 totalChecks、wasteDetected、
// wasteByType、totalWasteTokens 和 avgWastePerCheck。
func (d *TokenWasteDetector) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var avgWaste float64
	if d.totalChecks > 0 {
		avgWaste = float64(d.totalWasteTokens) / float64(d.totalChecks)
	}

	// 复制 wasteByType 避免外部修改
	wasteByTypeCopy := make(map[string]int, len(d.wasteByType))
	for k, v := range d.wasteByType {
		wasteByTypeCopy[k] = v
	}

	return map[string]interface{}{
		"totalChecks":      d.totalChecks,
		"wasteDetected":    d.wasteDetected,
		"wasteByType":      wasteByTypeCopy,
		"totalWasteTokens": d.totalWasteTokens,
		"avgWastePerCheck": avgWaste,
	}
}

// Reset 重置检测器状态，清空所有计数。
func (d *TokenWasteDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.totalChecks = 0
	d.wasteDetected = 0
	d.wasteByType = make(map[string]int)
	d.totalWasteTokens = 0
}

// twdEstimateTokens 粗略估算字符串的 token 数（约 4 字符/token）。
func twdEstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return (len(s) + 3) / 4
}

// twdTokenize 将字符串按空白分词为小写 token 集合。
func twdTokenize(s string) map[string]struct{} {
	set := make(map[string]struct{})
	current := make([]rune, 0, len(s))
	for _, ch := range s {
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if len(current) > 0 {
				set[string(current)] = struct{}{}
				current = current[:0]
			}
		} else {
			current = append(current, ch)
		}
	}
	if len(current) > 0 {
		set[string(current)] = struct{}{}
	}
	return set
}

// twdDetectRedundancy 检测消息间的重复内容。
// 对每对消息计算 Jaccard 相似度，相似度 > 0.5 时将较短消息的 token 数计入浪费。
func twdDetectRedundancy(messages []string) int {
	if len(messages) < 2 {
		return 0
	}

	tokenSets := make([]map[string]struct{}, len(messages))
	for i, msg := range messages {
		tokenSets[i] = twdTokenize(msg)
	}

	waste := 0
	for i := 0; i < len(messages); i++ {
		for j := i + 1; j < len(messages); j++ {
			sim := twdJaccard(tokenSets[i], tokenSets[j])
			if sim > 0.5 {
				// 重复内容浪费：取较短消息的 token 数
				ti := twdEstimateTokens(messages[i])
				tj := twdEstimateTokens(messages[j])
				if ti < tj {
					waste += ti
				} else {
					waste += tj
				}
			}
		}
	}
	return waste
}

// twdJaccard 计算两个 token 集合的 Jaccard 相似度。
func twdJaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	intersection := 0
	for t := range a {
		if _, ok := b[t]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// twdDetectBoilerplate 检测消息中的模板套话，返回命中部分的 token 估算。
func twdDetectBoilerplate(message string) int {
	waste := 0
	for _, phrase := range twdBoilerplatePhrases {
		if twdContains(message, phrase) {
			waste += twdEstimateTokens(phrase)
		}
	}
	return waste
}

// twdDetectWhitespace 检测多余的空白字符（连续 2 个以上的空白视为浪费）。
func twdDetectWhitespace(message string) int {
	waste := 0
	consecutive := 0
	for _, ch := range message {
		if ch == ' ' || ch == '\t' {
			consecutive++
			if consecutive > 1 {
				// 每个多余的空白字符约 0.25 token，累加取整
				waste++
			}
		} else {
			consecutive = 0
		}
	}
	// 多余空白字符约 4 个折合 1 token
	return waste / 4
}

// twdContains 判断 s 是否包含子串 sub（大小写敏感）。
func twdContains(s, sub string) bool {
	if len(sub) == 0 {
		return false
	}
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
