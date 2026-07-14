package agent

import (
	"sync"

	"reasonix/internal/provider"
)

// ── OPT-88: 消息重要性评分器 (Message Importance Scorer) ──
// 为每条消息计算重要性评分，用于在压缩时决定保留哪些消息。
//
// 原理：对话历史中各消息价值不同。系统提示、带工具调用的助手消息、
// 用户请求通常价值更高；纯文本助手回复价值较低。近期消息因提供
// 直接上下文而获得额外加成；过长消息因占用预算而受到惩罚。
//
// 效果：在上下文压缩时优先保留高价值消息，丢弃低价值消息，从而在
// 有限预算内最大化有效信息密度。

// MessageImportanceScorer 消息重要性评分器
type MessageImportanceScorer struct {
	mu              sync.RWMutex
	totalScored     int
	messagesDropped int
	tokensSaved     int
}

// ImportanceScore 单条消息的重要性评分结果。
//
// 注意：本结构体命名为 ImportanceScore 而非 MessageScore，是为了避免与
// semantic_pruner.go (OPT-03) 中已存在的 MessageScore 类型产生命名冲突。
// 二者字段不同：此处用于压缩期保留决策（Score/Role/Keep/Reason）。
type ImportanceScore struct {
	Score  float64
	Role   string
	Keep   bool
	Reason string
}

// MessageScorerStats 消息评分统计
type MessageScorerStats struct {
	TotalScored     int
	MessagesDropped int
	TokensSaved     int
}

// NewMessageImportanceScorer 创建消息重要性评分器
func NewMessageImportanceScorer() *MessageImportanceScorer {
	return &MessageImportanceScorer{}
}

// ScoreMessage 为单条消息计算重要性评分。
// 评分依据：角色基线（system=1.0、assistant 带工具=0.8、user=0.7、
// tool=0.6、assistant 无工具=0.5）、近期加成（最后 3 条消息）、
// 超长内容惩罚（>4000 字符扣分，>8000 字符加倍扣分）。
func (s *MessageImportanceScorer) ScoreMessage(msg provider.Message, position int, total int, hasToolCalls bool) ImportanceScore {
	role := string(msg.Role)
	var base float64
	var reason string

	switch msg.Role {
	case provider.RoleSystem:
		base = 1.0
		reason = "system prompt"
	case provider.RoleAssistant:
		if hasToolCalls || len(msg.ToolCalls) > 0 {
			base = 0.8
			reason = "assistant with tool calls"
		} else {
			base = 0.5
			reason = "assistant without tools"
		}
	case provider.RoleUser:
		base = 0.7
		reason = "user message"
	case provider.RoleTool:
		base = 0.6
		reason = "tool result"
	default:
		base = 0.5
		reason = "unknown role"
	}

	score := base

	// 近期加成：最后 3 条消息获得递减加成
	if total > 0 && position >= total-3 {
		fromEnd := total - 1 - position // 0 = 最后一条
		switch fromEnd {
		case 0:
			score += 0.20
		case 1:
			score += 0.15
		default:
			score += 0.10
		}
		reason += "; recency bonus"
	}

	// 超长内容惩罚
	contentLen := len(msg.Content)
	if contentLen > 8000 {
		score -= 0.20
		reason += "; severe length penalty"
	} else if contentLen > 4000 {
		score -= 0.10
		reason += "; length penalty"
	}

	if score > 1.0 {
		score = 1.0
	}
	if score < 0 {
		score = 0
	}

	keep := score >= 0.4

	s.mu.Lock()
	s.totalScored++
	if !keep {
		s.messagesDropped++
		s.tokensSaved += contentLen / 4
	}
	s.mu.Unlock()

	return ImportanceScore{
		Score:  score,
		Role:   role,
		Keep:   keep,
		Reason: reason,
	}
}

// ScoreMessages 为全部消息计算评分
func (s *MessageImportanceScorer) ScoreMessages(messages []provider.Message) []ImportanceScore {
	total := len(messages)
	scores := make([]ImportanceScore, total)
	for i, msg := range messages {
		hasToolCalls := len(msg.ToolCalls) > 0
		scores[i] = s.ScoreMessage(msg, i, total, hasToolCalls)
	}
	return scores
}

// GetDropCandidates 返回可丢弃消息的索引（评分 < 0.4）
func (s *MessageImportanceScorer) GetDropCandidates(scores []ImportanceScore) []int {
	var candidates []int
	for i, sc := range scores {
		if sc.Score < 0.4 {
			candidates = append(candidates, i)
		}
	}
	return candidates
}

// GetStats 返回评分统计
func (s *MessageImportanceScorer) GetStats() MessageScorerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return MessageScorerStats{
		TotalScored:     s.totalScored,
		MessagesDropped: s.messagesDropped,
		TokensSaved:     s.tokensSaved,
	}
}

// Reset 重置评分器
func (s *MessageImportanceScorer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalScored = 0
	s.messagesDropped = 0
	s.tokensSaved = 0
}
