package agent

import (
	"strings"
	"sync"
)

// ── OPT-55: ConversationFlowOptimizer — 对话流优化器 ──
// 检测冗余查询和不必要的冗长回复，减少 token 浪费

// FlowAnalysis 对话流分析结果
type FlowAnalysis struct {
	IsRedundant     bool
	VerbosityLevel  string // "low", "medium", "high"
	SuggestedAction string
}

// FlowOptimizerStats 流优化器统计
type FlowOptimizerStats struct {
	TotalTurns       int
	UnnecessaryTurns int
	RedundantQueries int
	TokensSaved      int
}

// ConversationFlowOptimizer 对话流优化器
type ConversationFlowOptimizer struct {
	mu               sync.RWMutex
	totalTurns       int
	unnecessaryTurns int
	redundantQueries int
	flowPatterns     map[string]int
	tokensSaved      int
	lastUserMessage  string
}

// NewConversationFlowOptimizer 创建对话流优化器
func NewConversationFlowOptimizer() *ConversationFlowOptimizer {
	return &ConversationFlowOptimizer{
		flowPatterns: make(map[string]int),
	}
}

// AnalyzeTurn 分析一轮对话
func (f *ConversationFlowOptimizer) AnalyzeTurn(userMessage, assistantMessage string, hasToolCalls bool) FlowAnalysis {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.totalTurns++

	analysis := FlowAnalysis{}

	// 检测冗余查询
	if f.lastUserMessage != "" {
		analysis.IsRedundant = f.detectRedundantQueryUnsafe(userMessage, f.lastUserMessage)
		if analysis.IsRedundant {
			f.redundantQueries++
		}
	}
	f.lastUserMessage = userMessage

	// 评估冗余度
	analysis.VerbosityLevel = f.estimateVerbosityUnsafe(assistantMessage)

	// 建议操作
	if analysis.IsRedundant {
		analysis.SuggestedAction = "skip: duplicate query detected"
	} else if analysis.VerbosityLevel == "high" && !hasToolCalls {
		analysis.SuggestedAction = "compress: verbose response without tool calls"
	} else if analysis.VerbosityLevel == "medium" && hasToolCalls {
		analysis.SuggestedAction = "ok: moderate response with tool usage"
	} else {
		analysis.SuggestedAction = "ok"
	}

	// 记录流程模式
	pattern := analysis.VerbosityLevel
	f.flowPatterns[pattern]++

	return analysis
}

// DetectRedundantQuery 检测两个查询是否相似
func (f *ConversationFlowOptimizer) DetectRedundantQuery(current, previous string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.detectRedundantQueryUnsafe(current, previous)
}

func (f *ConversationFlowOptimizer) detectRedundantQueryUnsafe(current, previous string) bool {
	if current == "" || previous == "" {
		return false
	}
	// 简单词重叠检测
	currentWords := strings.Fields(strings.ToLower(current))
	previousWords := strings.Fields(strings.ToLower(previous))
	if len(currentWords) == 0 || len(previousWords) == 0 {
		return false
	}

	prevSet := make(map[string]bool, len(previousWords))
	for _, w := range previousWords {
		prevSet[w] = true
	}

	overlap := 0
	for _, w := range currentWords {
		if prevSet[w] {
			overlap++
		}
	}

	overlapRatio := float64(overlap) / float64(len(currentWords))
	return overlapRatio > 0.7
}

// EstimateVerbosity 评估消息冗余度
func (f *ConversationFlowOptimizer) EstimateVerbosity(message string) string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.estimateVerbosityUnsafe(message)
}

func (f *ConversationFlowOptimizer) estimateVerbosityUnsafe(message string) string {
	n := len(message)
	if n < 100 {
		return "low"
	}
	if n <= 500 {
		return "medium"
	}
	return "high"
}

// SuggestFlowOptimization 根据分析结果提供建议
func (f *ConversationFlowOptimizer) SuggestFlowOptimization(analysis FlowAnalysis) string {
	if analysis.IsRedundant {
		return "skip redundant query — use cached response"
	}
	switch analysis.VerbosityLevel {
	case "high":
		return "apply aggressive compression to reduce response tokens"
	case "medium":
		return "consider moderate compression for efficiency"
	default:
		return "no optimization needed"
	}
}

// RecordTurn 记录一轮对话
func (f *ConversationFlowOptimizer) RecordTurn(userMessage string, isRedundant bool, tokensWasted int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.totalTurns++
	if isRedundant {
		f.redundantQueries++
		f.unnecessaryTurns++
	}
	if tokensWasted > 0 {
		f.tokensSaved += tokensWasted
	}
}

// GetStats 获取统计信息
func (f *ConversationFlowOptimizer) GetStats() FlowOptimizerStats {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return FlowOptimizerStats{
		TotalTurns:       f.totalTurns,
		UnnecessaryTurns: f.unnecessaryTurns,
		RedundantQueries: f.redundantQueries,
		TokensSaved:      f.tokensSaved,
	}
}

// Reset 重置状态
func (f *ConversationFlowOptimizer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.totalTurns = 0
	f.unnecessaryTurns = 0
	f.redundantQueries = 0
	f.tokensSaved = 0
	f.lastUserMessage = ""
	f.flowPatterns = make(map[string]int)
}
