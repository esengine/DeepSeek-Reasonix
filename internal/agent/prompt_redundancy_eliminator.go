package agent
import "sync"

// ── OPT-180: PromptRedundancyEliminator (提示冗余消除器) ──
// 检测并消除提示中的冗余内容。基于 n-gram 滑动窗口识别重复出现的
// 词组片段，移除后续重复出现的内容以减少 token 开销，并累计消除计数
// 与节省 token。seenNgrams 跨调用累积所有出现过的 n-gram。

// PromptRedundancyEliminator 提示冗余消除器，检测并消除提示中的冗余内容。
type PromptRedundancyEliminator struct {
	mu              sync.RWMutex
	ngramSize       int
	eliminatedCount int
	tokensSaved     int
	seenNgrams      map[string]bool
}

// NewPromptRedundancyEliminator 创建一个新的提示冗余消除器。
// ngramSize 指定 n-gram 窗口大小（词数）。
func NewPromptRedundancyEliminator(ngramSize int) *PromptRedundancyEliminator {
	return &PromptRedundancyEliminator{
		ngramSize:  ngramSize,
		seenNgrams: make(map[string]bool),
	}
}

// Eliminate 检测并移除文本中重复出现的 n-gram 段落。
// 保留每个 n-gram 的首次出现，移除后续重复，并累计消除计数与节省 token。
// 返回消除冗余后的文本。
func (e *PromptRedundancyEliminator) Eliminate(text string) string {
	e.mu.Lock()
	defer e.mu.Unlock()

	tokens := preTokenize(text)
	if e.ngramSize <= 0 || len(tokens) < e.ngramSize {
		return text
	}

	keep := make([]bool, len(tokens))
	for i := range keep {
		keep[i] = true
	}

	localSeen := make(map[string]bool)
	eliminatedWindows := 0
	var removedText string

	i := 0
	for i <= len(tokens)-e.ngramSize {
		ngram := preExtractNgrams(tokens, i, e.ngramSize)
		if localSeen[ngram] {
			for j := i; j < i+e.ngramSize && j < len(tokens); j++ {
				if keep[j] {
					keep[j] = false
					if len(removedText) > 0 {
						removedText += " "
					}
					removedText += tokens[j]
				}
			}
			eliminatedWindows++
			e.seenNgrams[ngram] = true
			i += e.ngramSize
		} else {
			localSeen[ngram] = true
			e.seenNgrams[ngram] = true
			i++
		}
	}

	var result string
	for j, tok := range tokens {
		if keep[j] {
			if len(result) > 0 {
				result += " "
			}
			result += tok
		}
	}

	e.eliminatedCount += eliminatedWindows
	e.tokensSaved += preEstimateTokens(removedText)

	return result
}

// FindRedundancy 查找文本中重复出现的 n-gram 片段列表。
// 返回所有出现次数大于 1 的 n-gram 字符串（不修改内部状态）。
func (e *PromptRedundancyEliminator) FindRedundancy(text string) []string {
	e.mu.RLock()
	ngramSize := e.ngramSize
	e.mu.RUnlock()

	tokens := preTokenize(text)
	if ngramSize <= 0 || len(tokens) < ngramSize {
		return nil
	}

	counts := make(map[string]int)
	for i := 0; i <= len(tokens)-ngramSize; i++ {
		ngram := preExtractNgrams(tokens, i, ngramSize)
		counts[ngram]++
	}

	var redundant []string
	for ngram, count := range counts {
		if count > 1 {
			redundant = append(redundant, ngram)
		}
	}
	return redundant
}

// GetStats 返回消除器的统计信息，包括 ngramSize、eliminatedCount、
// tokensSaved 和 trackedNgrams。
func (e *PromptRedundancyEliminator) GetStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return map[string]interface{}{
		"ngramSize":       e.ngramSize,
		"eliminatedCount": e.eliminatedCount,
		"tokensSaved":     e.tokensSaved,
		"trackedNgrams":   len(e.seenNgrams),
	}
}

// Reset 重置消除器的所有状态，清空已跟踪的 n-gram 与计数，保留 ngramSize 配置。
func (e *PromptRedundancyEliminator) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.eliminatedCount = 0
	e.tokensSaved = 0
	e.seenNgrams = make(map[string]bool)
}

// ── 辅助函数（pre 前缀）──

// preTokenize 将文本按空白字符分词，返回词列表。
func preTokenize(text string) []string {
	var tokens []string
	start := -1
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if start >= 0 {
				tokens = append(tokens, text[start:i])
				start = -1
			}
		} else {
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		tokens = append(tokens, text[start:])
	}
	return tokens
}

// preExtractNgrams 从 tokens 的 start 位置开始提取大小为 size 的 n-gram 字符串。
func preExtractNgrams(tokens []string, start int, size int) string {
	ngram := tokens[start]
	for j := start + 1; j < start+size && j < len(tokens); j++ {
		ngram += " " + tokens[j]
	}
	return ngram
}

// preEstimateTokens 使用 len(text)/4 启发式估算文本的 token 数量。
func preEstimateTokens(text string) int {
	return len(text) / 4
}
