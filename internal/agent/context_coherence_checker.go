package agent

import (
	"strings"
	"sync"

	"reasonix/internal/provider"
)

// ── OPT-89: 上下文连贯性检查器 (Context Coherence Checker) ──
// 检查压缩后的上下文是否仍然逻辑连贯。
//
// 原理：上下文压缩可能破坏对话的逻辑链条，例如：出现孤立的工具结果
// （其对应的助手工具调用已被移除）、助手引用了已不存在的历史内容、
// 或相邻用户消息出现突兀的话题跳变。本模块在压缩后检测这些问题。
//
// 效果：在压缩后量化上下文连贯性，避免因关键上下文缺失导致的幻觉
// 与逻辑断裂。

// ContextCoherenceChecker 上下文连贯性检查器
type ContextCoherenceChecker struct {
	mu                sync.RWMutex
	totalChecks       int
	coherenceIssues   int
	avgCoherenceScore float64
}

// CoherenceIssue 连贯性问题
type CoherenceIssue struct {
	Type         string
	Description  string
	MessageIndex int
}

// CoherenceReport 连贯性检查报告
type CoherenceReport struct {
	Score      float64
	Issues     []CoherenceIssue
	IsCoherent bool
}

// CoherenceStats 连贯性检查统计
type CoherenceStats struct {
	TotalChecks       int
	CoherenceIssues   int
	AvgCoherenceScore float64
}

// NewContextCoherenceChecker 创建上下文连贯性检查器
func NewContextCoherenceChecker() *ContextCoherenceChecker {
	return &ContextCoherenceChecker{}
}

// CheckCoherence 检查消息序列是否构成连贯的对话。
// 检测项：孤立工具结果、缺失的上下文引用、突兀的话题跳变。
func (c *ContextCoherenceChecker) CheckCoherence(messages []provider.Message) CoherenceReport {
	// 收集所有助手工具调用 ID，用于判定工具结果是否孤立
	toolCallIDs := make(map[string]bool)
	for _, m := range messages {
		if m.Role == provider.RoleAssistant {
			for _, tc := range m.ToolCalls {
				toolCallIDs[tc.ID] = true
			}
		}
	}

	var issues []CoherenceIssue

	for i, msg := range messages {
		// 1. 孤立工具结果：工具消息没有对应的助手工具调用
		if msg.Role == provider.RoleTool {
			if msg.ToolCallID == "" || !toolCallIDs[msg.ToolCallID] {
				issues = append(issues, CoherenceIssue{
					Type:         "orphaned_tool_result",
					Description:  "tool result without a matching preceding assistant tool call",
					MessageIndex: i,
				})
			}
		}

		// 2. 缺失的上下文引用：助手引用了不存在的上文
		if msg.Role == provider.RoleAssistant {
			lc := strings.ToLower(msg.Content)
			if coherenceRefPhrase(lc) && !coherenceHasPriorContext(messages, i) {
				issues = append(issues, CoherenceIssue{
					Type:         "missing_context_reference",
					Description:  "assistant references prior context that is absent from history",
					MessageIndex: i,
				})
			}
		}

		// 3. 突兀的话题跳变：相邻用户消息无任何话题词重叠
		if msg.Role == provider.RoleUser && i > 0 && messages[i-1].Role == provider.RoleUser {
			if !coherenceShareTopicWord(msg.Content, messages[i-1].Content) {
				issues = append(issues, CoherenceIssue{
					Type:         "abrupt_topic_change",
					Description:  "consecutive user messages switch topics with no lexical overlap",
					MessageIndex: i,
				})
			}
		}
	}

	score := 1.0
	for _, iss := range issues {
		switch iss.Type {
		case "orphaned_tool_result":
			score -= 0.30
		case "missing_context_reference":
			score -= 0.20
		case "abrupt_topic_change":
			score -= 0.10
		}
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	hasCritical := false
	for _, iss := range issues {
		if iss.Type == "orphaned_tool_result" {
			hasCritical = true
			break
		}
	}
	isCoherent := score >= 0.7 && !hasCritical

	// 更新统计（运行期均值）
	c.mu.Lock()
	c.totalChecks++
	c.coherenceIssues += len(issues)
	if c.totalChecks == 1 {
		c.avgCoherenceScore = score
	} else {
		c.avgCoherenceScore = c.avgCoherenceScore + (score-c.avgCoherenceScore)/float64(c.totalChecks)
	}
	c.mu.Unlock()

	return CoherenceReport{
		Score:      score,
		Issues:     issues,
		IsCoherent: isCoherent,
	}
}

// GetStats 返回连贯性检查统计
func (c *ContextCoherenceChecker) GetStats() CoherenceStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CoherenceStats{
		TotalChecks:       c.totalChecks,
		CoherenceIssues:   c.coherenceIssues,
		AvgCoherenceScore: c.avgCoherenceScore,
	}
}

// Reset 重置检查器
func (c *ContextCoherenceChecker) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalChecks = 0
	c.coherenceIssues = 0
	c.avgCoherenceScore = 0
}

// coherenceRefPhrase 检测是否包含对上文的引用短语
func coherenceRefPhrase(s string) bool {
	phrases := []string{
		"as mentioned", "as previously", "the above", "earlier result",
		"that result", "the previous", "as you can see", "as discussed",
		"as noted", "per the above",
	}
	for _, p := range phrases {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// coherenceHasPriorContext 判断 idx 之前是否存在非系统的实质上下文
func coherenceHasPriorContext(messages []provider.Message, idx int) bool {
	for j := 0; j < idx; j++ {
		m := messages[j]
		if m.Role == provider.RoleSystem {
			continue
		}
		if m.Content != "" || len(m.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

// coherenceShareTopicWord 判断两段文本是否共享至少一个话题词
func coherenceShareTopicWord(a, b string) bool {
	aw := coherenceTopicWords(a)
	bw := coherenceTopicWords(b)
	for w := range aw {
		if bw[w] {
			return true
		}
	}
	return false
}

// coherenceTopicWords 提取文本中的话题词（去除短词与停用词）
func coherenceTopicWords(s string) map[string]bool {
	out := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,!?;:\"'()[]{}")
		if len(w) < 4 {
			continue
		}
		switch w {
		case "that", "this", "with", "from", "have", "your",
			"what", "please", "would", "could", "should", "about",
			"they", "them", "their", "there", "then", "than":
			continue
		}
		out[w] = true
	}
	return out
}
