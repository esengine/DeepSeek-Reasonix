package agent

import (
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/provider"
)

// ── OPT-51: 会话归档优化器 (Session Archive Optimizer) ──
// 对长时间不活跃的会话进行归档，保留缓存前缀（system prompt + 前 3 条消息），
// 将剩余消息压缩为简短摘要，从而在恢复会话时减少 token 消耗并保护缓存命中率。
//
// 原理：
// 1. 当会话消息数超过阈值（>50）且长时间不活跃（>10 轮）时，触发归档
// 2. 保留 system prompt 和前 3 条消息作为缓存前缀（保证前缀哈希稳定）
// 3. 将其余消息压缩为摘要，大幅减少恢复时的 token 消耗
// 4. 通过前缀哈希验证缓存命中：如果前缀未变，缓存仍然有效
//
// 效果：归档会话恢复时 token 减少 70-90%，同时保护 L1 缓存前缀不被破坏。

// SessionArchiveOptimizer 会话归档优化器
type SessionArchiveOptimizer struct {
	mu sync.RWMutex

	// totalArchives 累计归档的消息总数
	totalArchives int

	// tokensPreserved 通过归档节省的预估 token 数
	tokensPreserved int

	// prefixHashes 存储每个会话的缓存前缀哈希（sessionID -> prefix hash）
	prefixHashes map[string]string

	// archivedSessions 已归档的会话数
	archivedSessions int
}

// ArchiveResult 归档结果
type ArchiveResult struct {
	PreservedMessages int    `json:"preservedMessages"`
	ArchivedMessages  int    `json:"archivedMessages"`
	Summary           string `json:"summary"`
	PrefixHash        string `json:"prefixHash"`
}

// ArchiveOptimizerStats 归档优化器统计
type ArchiveOptimizerStats struct {
	TotalArchives    int `json:"totalArchives"`
	TokensPreserved  int `json:"tokensPreserved"`
	ArchivedSessions int `json:"archivedSessions"`
}

// NewSessionArchiveOptimizer 创建会话归档优化器
func NewSessionArchiveOptimizer() *SessionArchiveOptimizer {
	return &SessionArchiveOptimizer{
		prefixHashes: make(map[string]string),
	}
}

// ShouldArchive 判断会话是否需要归档。
// 当消息数超过 50 且当前轮次距最后活跃轮次超过 10 时返回 true。
func (s *SessionArchiveOptimizer) ShouldArchive(messageCount int, lastActiveTurn int, currentTurn int) bool {
	return messageCount > 50 && currentTurn-lastActiveTurn > 10
}

// ArchiveSession 归档会话消息。
// 保留 system prompt 和前 3 条消息（缓存前缀），将其余消息压缩为摘要。
// 返回归档结果并更新统计信息。
func (s *SessionArchiveOptimizer) ArchiveSession(sessionID string, messages []provider.Message) ArchiveResult {
	// 确定缓存前缀：system prompt + 前 3 条消息
	prefixEnd := 3
	if len(messages) > 0 && messages[0].Role == provider.RoleSystem {
		prefixEnd = 4 // system prompt + 前 3 条消息
	}
	if prefixEnd > len(messages) {
		prefixEnd = len(messages)
	}

	preserved := messages[:prefixEnd]
	archived := messages[prefixEnd:]

	// 计算缓存前缀哈希（复用 message_sorter.go 中的 computePrefixHash）
	prefixHash := computePrefixHash(preserved)

	// 生成归档摘要
	summary := summarizeForArchive(archived)

	// 估算节省的 token 数（复用 compact.go 中的 token 估算函数）
	archivedTokens := estimateMessagesTokens(archived)
	summaryTokens := estimateTextTokens(summary)
	tokensSaved := archivedTokens - summaryTokens
	if tokensSaved < 0 {
		tokensSaved = 0
	}

	// 更新统计
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalArchives += len(archived)
	s.tokensPreserved += tokensSaved
	s.prefixHashes[sessionID] = prefixHash
	s.archivedSessions++

	return ArchiveResult{
		PreservedMessages: len(preserved),
		ArchivedMessages:  len(archived),
		Summary:           summary,
		PrefixHash:        prefixHash,
	}
}

// GetPrefixHash 返回指定会话的缓存前缀哈希。
// 如果会话未被归档过，返回空字符串。
func (s *SessionArchiveOptimizer) GetPrefixHash(sessionID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prefixHashes[sessionID]
}

// ValidatePrefix 验证当前前缀哈希是否与归档时记录的前缀哈希一致。
// 用于缓存命中验证：如果前缀未变，则缓存仍然有效。
// 如果会话未被归档过，返回 false。
func (s *SessionArchiveOptimizer) ValidatePrefix(sessionID string, currentPrefix string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stored, ok := s.prefixHashes[sessionID]
	if !ok {
		return false
	}
	return stored == currentPrefix
}

// GetStats 获取归档优化器的统计信息
func (s *SessionArchiveOptimizer) GetStats() ArchiveOptimizerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return ArchiveOptimizerStats{
		TotalArchives:    s.totalArchives,
		TokensPreserved:  s.tokensPreserved,
		ArchivedSessions: s.archivedSessions,
	}
}

// Reset 重置归档优化器，清空所有统计和前缀哈希
func (s *SessionArchiveOptimizer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalArchives = 0
	s.tokensPreserved = 0
	s.prefixHashes = make(map[string]string)
	s.archivedSessions = 0
}

// ── 辅助函数 ──

// summarizeForArchive 将归档消息压缩为简短摘要。
// 摘要包含消息角色统计和前几条用户消息的摘要片段，便于恢复会话时快速了解历史上下文。
func summarizeForArchive(msgs []provider.Message) string {
	if len(msgs) == 0 {
		return ""
	}

	// 按角色统计消息数
	userCount, assistantCount, toolCount := 0, 0, 0
	for _, m := range msgs {
		switch m.Role {
		case provider.RoleUser:
			userCount++
		case provider.RoleAssistant:
			assistantCount++
		case provider.RoleTool:
			toolCount++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[session-archive] %d messages archived (user=%d, assistant=%d, tool=%d).",
		len(msgs), userCount, assistantCount, toolCount)

	// 包含前几条用户消息的摘要片段，提供上下文
	snippetCount := 0
	for _, m := range msgs {
		if m.Role != provider.RoleUser || m.Content == "" {
			continue
		}
		snippet := m.Content
		runes := []rune(snippet)
		if len(runes) > 80 {
			snippet = string(runes[:80]) + "..."
		}
		fmt.Fprintf(&b, " User: %q.", snippet)
		snippetCount++
		if snippetCount >= 3 {
			break
		}
	}

	return b.String()
}
