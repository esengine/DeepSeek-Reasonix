package agent

import "sync"

// ── OPT-133: ConversationTopicTracker (对话话题追踪器) ──
// 追踪对话中的话题变化，通过关键词匹配检测消息所属话题。
// 当话题发生转换时记录转换并更新话题历史，为上下文管理提供话题级别的洞察。
//
// 支持的话题: database, code, ui, config, test, api, error, performance 等。
// 每个话题关联一组关键词，消息中匹配到关键词即归属该话题。

// ConversationTopicTracker 对话话题追踪器，追踪对话中的话题变化。
type ConversationTopicTracker struct {
	mu               sync.RWMutex
	topics           []string
	currentTopic     string
	totalTransitions int
	topicDurations   map[string]int
	maxTopics        int
}

// cttTopicKeywords 定义话题与其关联的关键词列表。
var cttTopicKeywords = []struct {
	topic    string
	keywords []string
}{
	{"database", []string{"database", "sql", "query", "table", "schema", "index", "db"}},
	{"code", []string{"code", "function", "method", "class", "struct", "variable", "refactor"}},
	{"ui", []string{"ui", "button", "layout", "css", "frontend", "component", "widget"}},
	{"config", []string{"config", "setting", "env", "environment", "yaml", "toml"}},
	{"test", []string{"test", "testing", "mock", "assert", "coverage", "fixture"}},
	{"api", []string{"api", "endpoint", "request", "response", "http", "rest", "graphql"}},
	{"error", []string{"error", "exception", "bug", "crash", "fail", "panic", "trace"}},
	{"performance", []string{"performance", "optimization", "latency", "throughput", "benchmark"}},
}

// NewConversationTopicTracker 创建一个新的对话话题追踪器实例。
// maxTopics 指定话题历史列表的最大长度。
func NewConversationTopicTracker(maxTopics int) *ConversationTopicTracker {
	return &ConversationTopicTracker{
		topicDurations: make(map[string]int),
		maxTopics:      maxTopics,
	}
}

// DetectTopic 检测消息所属话题，通过关键词匹配实现。
// 若无关键词匹配，返回 "general"。
func (t *ConversationTopicTracker) DetectTopic(message string) string {
	return cttDetectTopic(message)
}

// UpdateTopic 更新当前话题。如果检测到的话题与当前话题不同，
// 返回 true 并记录话题转换，同时更新话题历史。
func (t *ConversationTopicTracker) UpdateTopic(message string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	detected := cttDetectTopic(message)

	// Track topic duration (message count per topic)
	t.topicDurations[detected]++

	changed := detected != t.currentTopic
	if changed {
		t.totalTransitions++
		t.currentTopic = detected
		t.topics = append(t.topics, detected)
		// Trim to maxTopics, keeping most recent
		if len(t.topics) > t.maxTopics {
			t.topics = t.topics[len(t.topics)-t.maxTopics:]
		}
	}

	return changed
}

// GetCurrentTopic 返回当前话题。
func (t *ConversationTopicTracker) GetCurrentTopic() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.currentTopic
}

// GetTopicHistory 返回话题历史列表的副本。
func (t *ConversationTopicTracker) GetTopicHistory() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]string, len(t.topics))
	copy(result, t.topics)
	return result
}

// GetStats 返回话题追踪器的统计信息。
func (t *ConversationTopicTracker) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return map[string]interface{}{
		"totalTransitions": t.totalTransitions,
		"topicCount":       len(t.topics),
		"currentTopic":     t.currentTopic,
		"uniqueTopics":     len(t.topicDurations),
		"maxTopics":        t.maxTopics,
	}
}

// Reset 重置话题追踪器的所有状态和统计数据。
func (t *ConversationTopicTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.topics = nil
	t.currentTopic = ""
	t.totalTransitions = 0
	t.topicDurations = make(map[string]int)
}

// ---------------------------------------------------------------------------
// 辅助函数 (ctt 前缀)
// ---------------------------------------------------------------------------

// cttDetectTopic 通过关键词匹配检测消息所属话题。
// 依次检查预定义话题的关键词，返回第一个匹配的话题。
// 若无匹配，返回 "general"。
func cttDetectTopic(message string) string {
	for _, tk := range cttTopicKeywords {
		for _, kw := range tk.keywords {
			if cttContainsLower(message, kw) {
				return tk.topic
			}
		}
	}
	return "general"
}

// cttContainsLower 检查 message 中是否包含 keyword (大小写不敏感)。
// 仅比较 ASCII 字符，适用于英文关键词匹配。
func cttContainsLower(message, keyword string) bool {
	if len(keyword) == 0 {
		return true
	}
	if len(message) < len(keyword) {
		return false
	}
	for i := 0; i <= len(message)-len(keyword); i++ {
		match := true
		for j := 0; j < len(keyword); j++ {
			c1 := message[i+j]
			c2 := keyword[j]
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 32
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 32
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
