package agent

import "sync"

// ── OPT-186: TokenAwarePipeline (Token 感知流水线) ──
// 将请求处理分为多个阶段以优化 token 使用。每个阶段携带预估的
// token 成本，Process 时按阶段顺序依次处理并累计总 token 消耗，
// 为 token 预算管控与流水线调优提供数据支撑。
//
// 原理：将一次完整的请求处理拆解为若干有序阶段（如解析、检索、
// 组装、压缩等），每阶段独立估算 token 成本，处理时逐阶段累加，
// 便于在预算不足时提前终止或跳过低收益阶段。
//
// 效果：细粒度掌控 token 消耗分布，为动态预算分配与阶段级
// 裁剪提供量化依据。

// PipelineStage 流水线处理阶段。
type PipelineStage struct {
	Name      string // 阶段名称
	TokenCost int    // 阶段预估 token 成本
}

// TokenAwarePipeline Token 感知流水线，将请求处理分为多个阶段以优化 token 使用。
type TokenAwarePipeline struct {
	mu                   sync.RWMutex
	stages               []PipelineStage
	processedCount       int
	totalTokensProcessed int
}

// NewTokenAwarePipeline 创建一个新的 Token 感知流水线。
func NewTokenAwarePipeline() *TokenAwarePipeline {
	return &TokenAwarePipeline{
		stages:               make([]PipelineStage, 0),
		processedCount:       0,
		totalTokensProcessed: 0,
	}
}

// AddStage 添加处理阶段。
// name 为阶段名称，tokenCost 为该阶段预估的 token 成本。
func (p *TokenAwarePipeline) AddStage(name string, tokenCost int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stages = append(p.stages, PipelineStage{
		Name:      name,
		TokenCost: tokenCost,
	})
}

// Process 按阶段顺序处理输入，累计 token 成本。
// 遍历所有阶段累加 token 成本，递增处理计数，并返回处理后的结果。
func (p *TokenAwarePipeline) Process(input string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := input
	for _, stage := range p.stages {
		p.totalTokensProcessed += stage.TokenCost
	}
	p.processedCount++
	return result
}

// GetStages 获取阶段列表（返回副本，避免外部修改）。
func (p *TokenAwarePipeline) GetStages() []PipelineStage {
	p.mu.RLock()
	defer p.mu.RUnlock()
	stages := make([]PipelineStage, len(p.stages))
	copy(stages, p.stages)
	return stages
}

// GetStats 返回统计信息，包含 stageCount、processedCount 和 totalTokensProcessed。
func (p *TokenAwarePipeline) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return map[string]interface{}{
		"stageCount":           len(p.stages),
		"processedCount":       p.processedCount,
		"totalTokensProcessed": p.totalTokensProcessed,
	}
}

// Reset 重置流水线状态，清空阶段列表和所有统计计数。
func (p *TokenAwarePipeline) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stages = make([]PipelineStage, 0)
	p.processedCount = 0
	p.totalTokensProcessed = 0
}

// tplEstimateTokens 估算输入文本的 token 数量（辅助函数）。
// 采用简单的分词估算：按空白字符切分，每个词约计 1 个 token。
func tplEstimateTokens(text string) int {
	count := 0
	inWord := false
	for _, ch := range text {
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			inWord = false
		} else if !inWord {
			inWord = true
			count++
		}
	}
	return count
}
