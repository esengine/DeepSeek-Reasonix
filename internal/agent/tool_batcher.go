package agent

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ── OPT-21: 工具调用批处理 (Tool Call Batching) ──
// 合并多个只读工具调用为单次请求，减少 API 调用次数和 token 消耗。
//
// 原理：当模型在一轮中调用多个只读工具（如同时 read_file A、read_file B、
// grep pattern C），每个工具结果单独作为一条消息加入历史。通过批处理：
// 1. 将多个只读工具的结果合并为一条消息
// 2. 减少消息数量（从 N 条降到 1 条）
// 3. 减少结构化格式的 overhead token（每条消息约 20 token overhead）
//
// 效果：对于每轮 3-5 个工具调用的场景，节省 60-100 token/轮，
// 同时减少消息数量使对话历史更紧凑。

// ToolCallBatcher 工具调用批处理器
type ToolCallBatcher struct {
	mu sync.Mutex

	// 当前批次中的工具调用
	currentBatch []BatchedToolCall

	// 配置
	maxBatchSize     int           // 最大批处理大小
	batchTimeout     time.Duration // 批处理超时（超时后自动刷新）
	maxResultLength  int           // 单个结果最大长度（超过则截断）
	flushTimer       *time.Timer
}

// BatchedToolCall 批处理的工具调用
type BatchedToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Args      json.RawMessage `json:"args"`
	Result    string          `json:"result"`
	Timestamp time.Time       `json:"timestamp"`
	ReadOnly  bool            `json:"readOnly"`
}

// NewToolCallBatcher 创建批处理器
func NewToolCallBatcher() *ToolCallBatcher {
	return &ToolCallBatcher{
		maxBatchSize:    10,
		batchTimeout:    2 * time.Second,
		maxResultLength: 50000, // 50KB
	}
}

// Add 添加一个工具调用到批次
// 返回 true 表示批次已满，需要刷新
func (b *ToolCallBatcher) Add(call BatchedToolCall) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 只批处理只读工具
	if !call.ReadOnly {
		return false
	}

	// 截断过长的结果
	if len(call.Result) > b.maxResultLength {
		call.Result = call.Result[:b.maxResultLength] + "\n... [结果已截断]"
	}

	b.currentBatch = append(b.currentBatch, call)

	// 如果批次满了，返回 true
	return len(b.currentBatch) >= b.maxBatchSize
}

// Flush 刷新批次，返回合并后的消息
func (b *ToolCallBatcher) Flush() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.currentBatch) == 0 {
		return ""
	}

	// 如果只有一个调用，直接返回结果
	if len(b.currentBatch) == 1 {
		result := b.currentBatch[0].Result
		b.currentBatch = nil
		return result
	}

	// 合并多个结果为一条消息
	var sb []byte
	sb = append(sb, []byte("[批量工具结果 — 合并 "+fmt.Sprintf("%d", len(b.currentBatch))+" 个只读工具调用]\n\n")...)

	for i, call := range b.currentBatch {
		if i > 0 {
			sb = append(sb, []byte("\n---\n\n")...)
		}
		header := fmt.Sprintf("▸ %s (id=%s)\n", call.Name, call.ID)
		sb = append(sb, []byte(header)...)
		sb = append(sb, []byte(call.Result)...)
	}

	b.currentBatch = nil
	return string(sb)
}

// GetBatchCount 获取当前批次中的调用数
func (b *ToolCallBatcher) GetBatchCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.currentBatch)
}

// IsEmpty 检查批次是否为空
func (b *ToolCallBatcher) IsEmpty() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.currentBatch) == 0
}

// GetStats 获取统计
func (b *ToolCallBatcher) GetStats() BatcherStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return BatcherStats{
		CurrentBatchSize: len(b.currentBatch),
		MaxBatchSize:     b.maxBatchSize,
	}
}

// BatcherStats 批处理统计
type BatcherStats struct {
	CurrentBatchSize int `json:"currentBatchSize"`
	MaxBatchSize     int `json:"maxBatchSize"`
}

// Reset 重置批处理器
func (b *ToolCallBatcher) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentBatch = nil
}
