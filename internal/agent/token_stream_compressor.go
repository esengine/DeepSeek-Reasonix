package agent

import "sync"

// TokenStreamCompressor (OPT-101) 实时流式 token 压缩器。
// 通过去重连续重复的 token 来减少流式输出中的冗余数据，降低 token 消耗。
type TokenStreamCompressor struct {
	mu               sync.RWMutex
	totalStreamed    int64
	compressedTokens int64
	activeStreams    int
	compressionRatio float64
	streamBuffer     map[string][]string
}

// NewTokenStreamCompressor 创建一个新的 TokenStreamCompressor 实例。
func NewTokenStreamCompressor() *TokenStreamCompressor {
	return &TokenStreamCompressor{
		streamBuffer: make(map[string][]string),
	}
}

// StartStream 开始一个新的流式传输会话。
func (c *TokenStreamCompressor) StartStream(streamID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streamBuffer[streamID] = []string{}
	c.activeStreams++
}

// PushChunk 推送一个流块到指定流，去除连续重复的 token 后返回处理结果。
// 重复的 token 仅保留第一次出现，后续连续出现将被压缩丢弃。
func (c *TokenStreamCompressor) PushChunk(streamID string, chunk string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	buffer, ok := c.streamBuffer[streamID]
	if !ok {
		// 流不存在时自动创建
		buffer = []string{}
		c.activeStreams++
	}

	tokens := tscSplitBySpace(chunk)
	var result []string
	var compressed int

	for _, token := range tokens {
		if len(buffer) > 0 && buffer[len(buffer)-1] == token {
			// 与上一个 token 相同，压缩
			compressed++
			continue
		}
		buffer = append(buffer, token)
		result = append(result, token)
	}

	c.streamBuffer[streamID] = buffer
	c.totalStreamed += int64(len(tokens))
	c.compressedTokens += int64(compressed)
	if c.totalStreamed > 0 {
		c.compressionRatio = float64(c.compressedTokens) / float64(c.totalStreamed)
	}

	return tscJoin(result)
}

// EndStream 结束指定流，返回累计压缩的 token 数。
func (c *TokenStreamCompressor) EndStream(streamID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.streamBuffer[streamID]; ok {
		delete(c.streamBuffer, streamID)
		if c.activeStreams > 0 {
			c.activeStreams--
		}
	}
	return int(c.compressedTokens)
}

// GetCompressionRatio 返回当前的压缩比率（0.0-1.0）。
func (c *TokenStreamCompressor) GetCompressionRatio() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.compressionRatio
}

// GetStats 返回压缩器的统计信息。
func (c *TokenStreamCompressor) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"totalStreamed":    c.totalStreamed,
		"compressedTokens": c.compressedTokens,
		"activeStreams":    c.activeStreams,
		"compressionRatio": c.compressionRatio,
		"savedTokens":      c.compressedTokens,
	}
}

// Reset 重置压缩器的所有状态。
func (c *TokenStreamCompressor) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalStreamed = 0
	c.compressedTokens = 0
	c.activeStreams = 0
	c.compressionRatio = 0
	c.streamBuffer = make(map[string][]string)
}

// tscSplitBySpace 按空白字符分割字符串为 token 列表。
func tscSplitBySpace(s string) []string {
	var tokens []string
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' {
			if start >= 0 {
				tokens = append(tokens, s[start:i])
				start = -1
			}
		} else {
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		tokens = append(tokens, s[start:])
	}
	return tokens
}

// tscJoin 用空格连接 token 列表为字符串。
func tscJoin(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	result := tokens[0]
	for i := 1; i < len(tokens); i++ {
		result += " " + tokens[i]
	}
	return result
}
