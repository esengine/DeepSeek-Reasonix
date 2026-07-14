package agent

import (
	"strings"
	"sync"
	"time"

	"reasonix/internal/provider"
)

// ── OPT-10: 流式响应增量缓存 ──
// 在流式接收的同时，将已完成的 token 缓存在内存中。
// 当流式连接断开时（IsConnReset），可以用缓存的增量内容拼接恢复提示，
// 而不是从零开始重新请求。
// 同时用于实时计算部分响应的置信度（与 B3 协作）。

// IncrementalCache 缓存流式响应的增量内容
type IncrementalCache struct {
	mu             sync.Mutex
	content        strings.Builder
	reasoning      strings.Builder
	tokenCount     int // 已缓存的 token 估计数
	lastUpdate     time.Time
	maxCacheTokens int                // 最大缓存 token 数，超过后开始覆盖最旧内容
	logProbs       []provider.LogProb // 增量收集的 logprobs
}

// NewIncrementalCache 创建新的增量缓存
func NewIncrementalCache(maxTokens int) *IncrementalCache {
	if maxTokens <= 0 {
		maxTokens = 8192 // 默认缓存 8K token
	}
	return &IncrementalCache{
		maxCacheTokens: maxTokens,
		lastUpdate:     time.Now(),
	}
}

// AppendContent 追加内容 token
func (c *IncrementalCache) AppendContent(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.content.WriteString(text)
	c.lastUpdate = time.Now()
	// 粗略估算 token 数（~4 chars/token）
	c.tokenCount += len(text) / 4
	c.evictIfNeeded()
}

// AppendReasoning 追加推理 token
func (c *IncrementalCache) AppendReasoning(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reasoning.WriteString(text)
	c.lastUpdate = time.Now()
}

// AppendLogProbs 追加 logprobs
func (c *IncrementalCache) AppendLogProbs(lps []provider.LogProb) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logProbs = append(c.logProbs, lps...)
}

// Content 返回当前缓存的内容
func (c *IncrementalCache) Content() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.content.String()
}

// Reasoning 返回当前缓存的推理内容
func (c *IncrementalCache) Reasoning() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reasoning.String()
}

// LogProbs 返回当前缓存的 logprobs
func (c *IncrementalCache) LogProbs() []provider.LogProb {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]provider.LogProb(nil), c.logProbs...)
}

// TokenCount 返回当前缓存的估计 token 数
func (c *IncrementalCache) TokenCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokenCount
}

// IsPartial 检查是否有未完成的缓存内容（用于断线恢复判断）
func (c *IncrementalCache) IsPartial() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.content.Len() > 0
}

// RecoveryPrompt 生成断线恢复提示（追加到会话末尾，让模型从断点继续）
func (c *IncrementalCache) RecoveryPrompt() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.content.Len() == 0 {
		return ""
	}
	// 取最后 500 字符作为恢复上下文
	content := c.content.String()
	if len(content) > 500 {
		content = content[len(content)-500:]
	}
	return "[流式连接中断，已生成部分内容: ... " + content + "]\n请从上述内容的断点继续输出。"
}

// Reset 清空缓存（新 turn 开始时调用）
func (c *IncrementalCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.content.Reset()
	c.reasoning.Reset()
	c.tokenCount = 0
	c.logProbs = nil
	c.lastUpdate = time.Now()
}

// LastUpdate 返回最后更新时间
func (c *IncrementalCache) LastUpdate() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastUpdate
}

// evictIfNeeded 当缓存超过最大 token 数时，丢弃最旧的内容
func (c *IncrementalCache) evictIfNeeded() {
	if c.tokenCount <= c.maxCacheTokens {
		return
	}
	// 保留后半部分内容
	content := c.content.String()
	keepLen := len(content) / 2
	c.content.Reset()
	c.content.WriteString(content[len(content)-keepLen:])
	c.tokenCount = c.content.Len() / 4
}
