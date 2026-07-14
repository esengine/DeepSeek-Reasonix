package agent

import "sync"

// ── OPT-160: PromptSegmentIndexer (提示分段索引器 / Prompt Segment Indexer) ──
// 索引提示段落以支持快速检索。将段落内容分词后建立倒排索引（word → segmentIDs），
// 支持按关键词快速查找包含特定词汇的段落 ID。
//
// 原理：在 LLM 提示工程中，经常需要从大量提示段落中检索相关内容。
// 通过建立倒排索引，将每个词映射到包含该词的段落 ID 列表，
// 可以在 O(1) 时间内找到包含特定词的段落，大幅提升检索效率。
//
// 效果：支持提示段落的快速全文检索，统计索引的段落数和索引大小，
// 为提示管理和检索优化提供数据支撑。

// PromptSegmentIndexer 提示分段索引器
type PromptSegmentIndexer struct {
	mu           sync.RWMutex
	segments     map[string]string   // id → 段落内容
	index        map[string][]string // word → segmentIDs（倒排索引）
	totalIndexed int                 // 累计索引次数
}

// NewPromptSegmentIndexer 创建提示分段索引器。
func NewPromptSegmentIndexer() *PromptSegmentIndexer {
	return &PromptSegmentIndexer{
		segments: make(map[string]string),
		index:    make(map[string][]string),
	}
}

// Index 索引一个提示段落。
// 将 content 分词后建立倒排索引，每个词映射到该段落的 ID。
// 若 id 已存在则先移除旧索引再重新建立。
func (p *PromptSegmentIndexer) Index(id string, content string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 若 id 已存在，先移除旧索引
	if _, exists := p.segments[id]; exists {
		psiRemoveFromIndex(p.index, id)
	}

	p.segments[id] = content
	p.totalIndexed++

	// 分词并建立倒排索引
	words := psiTokenize(content)
	for _, word := range words {
		// 避免重复添加同一个 segmentID
		alreadyExists := false
		for _, sid := range p.index[word] {
			if sid == id {
				alreadyExists = true
				break
			}
		}
		if !alreadyExists {
			p.index[word] = append(p.index[word], id)
		}
	}
}

// Search 搜索包含查询词的段落 ID 列表。
// 将 query 分词后，返回包含任意查询词的段落 ID（去重）。
func (p *PromptSegmentIndexer) Search(query string) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	words := psiTokenize(query)
	seen := make(map[string]bool)
	var result []string

	for _, word := range words {
		for _, sid := range p.index[word] {
			if !seen[sid] {
				seen[sid] = true
				result = append(result, sid)
			}
		}
	}
	return result
}

// GetSegment 获取指定 ID 的段落内容。
// 若 id 存在则返回内容和 true，否则返回空字符串和 false。
func (p *PromptSegmentIndexer) GetSegment(id string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	content, ok := p.segments[id]
	return content, ok
}

// Remove 移除指定 ID 的段落及其索引。
// 若 id 不存在则不做任何操作。
func (p *PromptSegmentIndexer) Remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.segments[id]; !exists {
		return
	}
	delete(p.segments, id)
	psiRemoveFromIndex(p.index, id)
}

// GetStats 返回索引器的统计信息。
// 包含 segmentCount（段落数）、indexSize（索引词数）和 totalIndexed（累计索引次数）。
func (p *PromptSegmentIndexer) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return map[string]interface{}{
		"segmentCount": len(p.segments),
		"indexSize":    len(p.index),
		"totalIndexed": p.totalIndexed,
	}
}

// Reset 重置索引器的所有状态和统计信息。
func (p *PromptSegmentIndexer) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.segments = make(map[string]string)
	p.index = make(map[string][]string)
	p.totalIndexed = 0
}

// psiTokenize 将文本按空格分词并转为小写。
// 返回分词后的词列表（已转为小写）。
func psiTokenize(text string) []string {
	if len(text) == 0 {
		return nil
	}
	var words []string
	start := 0
	for i := 0; i <= len(text); i++ {
		if i == len(text) || text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r' {
			if i > start {
				// 转为小写
				word := make([]byte, i-start)
				for j := start; j < i; j++ {
					c := text[j]
					if c >= 'A' && c <= 'Z' {
						word[j-start] = c + 32
					} else {
						word[j-start] = c
					}
				}
				words = append(words, string(word))
			}
			start = i + 1
		}
	}
	return words
}

// psiRemoveFromIndex 从倒排索引中移除指定段落 ID 的所有引用。
// 遍历所有词的 segmentID 列表，移除匹配的 ID，若列表为空则删除该词的索引项。
func psiRemoveFromIndex(index map[string][]string, id string) {
	for word, ids := range index {
		var newIDs []string
		for _, sid := range ids {
			if sid != id {
				newIDs = append(newIDs, sid)
			}
		}
		if len(newIDs) == 0 {
			delete(index, word)
		} else {
			index[word] = newIDs
		}
	}
}
