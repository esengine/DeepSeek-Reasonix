package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"reasonix/internal/provider"
)

// ── OPT-12: 缓存前缀稳定性强制器 ──
// 检测并阻止破坏 prompt cache 前缀的变更。
// 根据 "Don't Break the Cache" 论文 (arXiv:2601.06007)，策略性缓存断点控制
// 比全量缓存效果更稳定，动态内容应排除在缓存断点之外。
//
// 原理：Provider 的 prompt cache 按 prefix 匹配，前缀中任何一个 token 变化
// 都会使该断点之后的所有缓存失效。本模块在发送请求前检测前缀变化，
// 在变化发生时发出警告并记录，帮助开发者保持前缀稳定。

// CachePrefixEnforcer 监控缓存前缀稳定性
type CachePrefixEnforcer struct {
	mu sync.RWMutex

	// 上一次请求的前缀指纹
	lastFingerprint PrefixFingerprint

	// 前缀变化历史（用于诊断）
	changeHistory []PrefixChange

	// 缓存命中率追踪
	hitRateTracker *CacheHitRateTracker

	// 配置
	warnOnSystemChange  bool // system prompt 变化时警告
	warnOnToolsChange   bool // tools 定义变化时警告
	warnOnLowHitRate    bool // 缓存命中率低于阈值时警告
	lowHitRateThreshold float64

	// 统计
	totalRequests   int
	cacheHits       int
	cacheMisses     int
	cacheWriteTokens int
	cacheReadTokens  int
}

// PrefixFingerprint 前缀指纹
type PrefixFingerprint struct {
	SystemHash     string // system prompt 的 SHA256
	ToolsHash      string // tools schema 的 SHA256
	SystemTokens   int    // system prompt 估算 token 数
	ToolsTokens    int    // tools schema 估算 token 数
	CapturedAt     time.Time
}

// PrefixChange 前缀变更记录
type PrefixChange struct {
	Timestamp    time.Time
	ChangedField string // "system" | "tools" | "both"
	PrevHash     string
	NewHash      string
	TurnNumber   int
	Impact       string // "full_invalidation" | "partial" | "none"
}

// CacheHitRateTracker 缓存命中率追踪器
type CacheHitRateTracker struct {
	mu          sync.Mutex
	recentHits  []bool // 最近 N 次请求的命中情况
	windowSize  int
}

// NewCacheHitRateTracker 创建命中率追踪器
func NewCacheHitRateTracker(windowSize int) *CacheHitRateTracker {
	if windowSize <= 0 {
		windowSize = 20
	}
	return &CacheHitRateTracker{
		recentHits: make([]bool, 0, windowSize),
		windowSize: windowSize,
	}
}

// Record 记录一次请求的缓存命中情况
func (t *CacheHitRateTracker) Record(hit bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.recentHits) >= t.windowSize {
		t.recentHits = t.recentHits[1:]
	}
	t.recentHits = append(t.recentHits, hit)
}

// HitRate 返回最近的缓存命中率
func (t *CacheHitRateTracker) HitRate() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.recentHits) == 0 {
		return 0
	}
	hits := 0
	for _, h := range t.recentHits {
		if h {
			hits++
		}
	}
	return float64(hits) / float64(len(t.recentHits))
}

// NewCachePrefixEnforcer 创建缓存前缀稳定性强制器
func NewCachePrefixEnforcer() *CachePrefixEnforcer {
	return &CachePrefixEnforcer{
		warnOnSystemChange:  true,
		warnOnToolsChange:   true,
		warnOnLowHitRate:    true,
		lowHitRateThreshold: 0.5, // 低于 50% 命中率时警告
		hitRateTracker:      NewCacheHitRateTracker(20),
	}
}

// CaptureFingerprint 捕获当前请求的前缀指纹
func (e *CachePrefixEnforcer) CaptureFingerprint(systemPrompt string, tools []provider.ToolSchema) PrefixFingerprint {
	normalizedTools := normalizeToolSchemas(tools)
	toolsJSON, _ := json.Marshal(normalizedTools)

	return PrefixFingerprint{
		SystemHash:   hashStr(systemPrompt),
		ToolsHash:    hashStr(string(toolsJSON)),
		SystemTokens: estimateTokens(systemPrompt),
		ToolsTokens:  estimateTokens(string(toolsJSON)),
		CapturedAt:   time.Now(),
	}
}

// CheckPrefixStability 检查前缀稳定性，返回变化详情
// 在发送 API 请求前调用，如果前缀发生变化会记录并警告
func (e *CachePrefixEnforcer) CheckPrefixStability(fp PrefixFingerprint, turnNumber int) *PrefixChange {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.totalRequests++

	var change *PrefixChange
	changedFields := []string{}

	if e.lastFingerprint.SystemHash != "" && e.lastFingerprint.SystemHash != fp.SystemHash {
		changedFields = append(changedFields, "system")
		if e.warnOnSystemChange {
			slog.Warn("OPT-12: system prompt changed — cache prefix invalidated",
				"turn", turnNumber,
				"prev_hash", e.lastFingerprint.SystemHash,
				"new_hash", fp.SystemHash,
				"system_tokens", fp.SystemTokens,
				"impact", "full_invalidation: all subsequent cache breakpoints miss",
			)
		}
	}

	if e.lastFingerprint.ToolsHash != "" && e.lastFingerprint.ToolsHash != fp.ToolsHash {
		changedFields = append(changedFields, "tools")
		if e.warnOnToolsChange {
			slog.Warn("OPT-12: tools definition changed — cache prefix invalidated",
				"turn", turnNumber,
				"prev_hash", e.lastFingerprint.ToolsHash,
				"new_hash", fp.ToolsHash,
				"tools_tokens", fp.ToolsTokens,
				"impact", "full_invalidation: tools are the first prefix segment",
			)
		}
	}

	if len(changedFields) > 0 {
		change = &PrefixChange{
			Timestamp:    time.Now(),
			ChangedField: strings.Join(changedFields, ","),
			PrevHash:     e.lastFingerprint.SystemHash + "|" + e.lastFingerprint.ToolsHash,
			NewHash:      fp.SystemHash + "|" + fp.ToolsHash,
			TurnNumber:   turnNumber,
			Impact:       "full_invalidation",
		}
		e.changeHistory = append(e.changeHistory, *change)
		// 保留最近 50 条变更记录
		if len(e.changeHistory) > 50 {
			e.changeHistory = e.changeHistory[1:]
		}
	}

	e.lastFingerprint = fp
	return change
}

// RecordCacheUsage 记录缓存使用情况
func (e *CachePrefixEnforcer) RecordCacheUsage(usage *provider.Usage) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if usage == nil {
		return
	}

	e.cacheWriteTokens += usage.CacheMissTokens
	e.cacheReadTokens += usage.CacheHitTokens

	totalCacheTokens := usage.CacheHitTokens + usage.CacheMissTokens
	if totalCacheTokens > 0 {
		hit := usage.CacheHitTokens > 0
		e.hitRateTracker.Record(hit)
		if hit {
			e.cacheHits++
		} else {
			e.cacheMisses++
		}

		// 低命中率警告
		if e.warnOnLowHitRate {
			rate := e.hitRateTracker.HitRate()
			if rate < e.lowHitRateThreshold && e.totalRequests > 5 {
				slog.Warn("OPT-12: cache hit rate below threshold",
					"hit_rate", fmt.Sprintf("%.1f%%", rate*100),
					"threshold", fmt.Sprintf("%.1f%%", e.lowHitRateThreshold*100),
					"total_requests", e.totalRequests,
					"cache_hits", e.cacheHits,
					"cache_misses", e.cacheMisses,
				)
			}
		}
	}
}

// GetStats 返回缓存统计
func (e *CachePrefixEnforcer) GetStats() CachePrefixStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var rate float64
	if e.totalRequests > 0 {
		rate = float64(e.cacheHits) / float64(e.totalRequests)
	}

	return CachePrefixStats{
		TotalRequests:      e.totalRequests,
		CacheHits:          e.cacheHits,
		CacheMisses:        e.cacheMisses,
		HitRate:            rate,
		RecentHitRate:      e.hitRateTracker.HitRate(),
		CacheWriteTokens:   e.cacheWriteTokens,
		CacheReadTokens:    e.cacheReadTokens,
		TokenSavings:       e.cacheReadTokens * 9, // 缓存读取比标准输入便宜 10 倍
		ChangeHistoryCount: len(e.changeHistory),
	}
}

// GetChangeHistory 返回前缀变化历史
func (e *CachePrefixEnforcer) GetChangeHistory() []PrefixChange {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]PrefixChange, len(e.changeHistory))
	copy(out, e.changeHistory)
	return out
}

// CachePrefixStats 缓存前缀统计
type CachePrefixStats struct {
	TotalRequests      int     `json:"totalRequests"`
	CacheHits          int     `json:"cacheHits"`
	CacheMisses        int     `json:"cacheMisses"`
	HitRate            float64 `json:"hitRate"`
	RecentHitRate      float64 `json:"recentHitRate"`
	CacheWriteTokens   int     `json:"cacheWriteTokens"`
	CacheReadTokens    int     `json:"cacheReadTokens"`
	TokenSavings       int     `json:"tokenSavings"` // 相比不缓存节省的 token 数
	ChangeHistoryCount int     `json:"changeHistoryCount"`
}

// ── OPT-13: 工具结果缓存隔离 ──
// 动态工具结果（如 bash 输出、搜索结果）不应破坏缓存前缀。
// 本模块标记工具结果的稳定性级别，帮助 provider 正确放置缓存断点。

// ToolResultStability 工具结果稳定性级别
type ToolResultStability int

const (
	// StabilityStable — 稳定结果（如 read_file 读取的固定文件内容）
	// 可以放在缓存断点之前，不会破坏缓存
	StabilityStable ToolResultStability = iota
	// StabilityVolatile — 易变结果（如 bash 输出、搜索结果、时间戳）
	// 必须放在缓存断点之后，否则每次变化都会使缓存失效
	StabilityVolatile
	// StabilitySemiStable — 半稳定结果（如 git status，短时间内不变）
	// 可以放在早期断点之前，但不应放在最后一个断点之前
	StabilitySemiStable
)

// ClassifyToolResultStability 分类工具结果的稳定性
// 根据 "Don't Break the Cache" 论文，动态内容应排除在缓存断点之外
func ClassifyToolResultStability(toolName string, resultContent string) ToolResultStability {
	switch toolName {
	// 稳定工具：读取固定内容
	case "read_file", "read", "cat":
		return StabilityStable

	// 易变工具：每次执行结果不同
	case "bash", "shell", "execute", "run_command":
		// 检查是否是确定性命令（如 cat、ls 固定目录）
		if isDeterministicCommand(resultContent) {
			return StabilitySemiStable
		}
		return StabilityVolatile

	case "grep", "search", "find", "glob":
		// 搜索结果取决于文件系统当前状态，半稳定
		return StabilitySemiStable

	case "web_fetch", "web_search", "fetch":
		// 网络内容每次可能不同
		return StabilityVolatile

	case "write_file", "edit_file", "str_replace", "multi_edit", "move_file":
		// 写操作的确认结果通常包含时间戳或哈希
		return StabilityVolatile

	case "todo_write", "complete_step":
		// 状态更新结果
		return StabilityVolatile

	case "code_index", "ls":
		// 目录列表短时间内稳定
		return StabilitySemiStable

	default:
		// 未知工具默认为易变
		return StabilityVolatile
	}
}

// isDeterministicCommand 判断 bash 结果是否来自确定性命令
func isDeterministicCommand(content string) bool {
	// 确定性命令的输出通常包含这些特征
	deterministicIndicators := []string{
		"total ", // ls -l 输出
		"drw",    // 目录权限
		"-rw",    // 文件权限
	}
	for _, ind := range deterministicIndicators {
		if strings.Contains(content, ind) {
			return true
		}
	}
	return false
}

// IsCacheSafeForBreakpoint 判断工具结果是否可以安全放在缓存断点之前
func IsCacheSafeForBreakpoint(toolName string, resultContent string) bool {
	stability := ClassifyToolResultStability(toolName, resultContent)
	return stability == StabilityStable
}

// 辅助函数
func hashStr(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}
