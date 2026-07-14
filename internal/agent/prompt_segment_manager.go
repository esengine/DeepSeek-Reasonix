package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
)

// ── OPT-94: PromptSegmentManager (提示词分段管理器) ──
// 将提示词拆分为可独立管理的段（segment），以实现细粒度的缓存控制。
//
// 原理：prompt 缓存以稳定前缀为粒度。若能将提示词拆分为多个段，
// 并将可缓存、高优先级的段排列到前缀位置，就能最大化缓存命中率。
// 当某个段内容变化时，只需更新该段的哈希，而无需重建整个提示词。
//
// 效果：通过段级哈希追踪与缓存友好排序，减少因无关段变化导致的
// 缓存失效，提升前缀稳定性与缓存命中率。

// PromptSegmentV2 提示词的一个可管理段（V2）
type PromptSegmentV2 struct {
	Name          string
	Content       string
	Hash          string
	TokenEstimate int
	Cacheable     bool
	Priority      int
}

// SegmentManagerStats 分段管理器统计信息
type SegmentManagerStats struct {
	TotalReorders   int // 缓存重排序总次数
	TokensSaved     int // 累计因缓存友好排序节省的 token 估算
	SegmentsTracked int // 当前追踪的段数量
}

// PromptSegmentManager 提示词分段管理器
// 管理提示词段的注册、更新与缓存友好排序。
type PromptSegmentManager struct {
	mu             sync.RWMutex
	segments       map[string]*PromptSegmentV2
	totalReorders  int
	tokensSaved    int
	insertionOrder []string // 段的注册顺序，用于稳定排序
}

// NewPromptSegmentManager 创建提示词分段管理器
func NewPromptSegmentManager() *PromptSegmentManager {
	return &PromptSegmentManager{
		segments: make(map[string]*PromptSegmentV2),
	}
}

// RegisterSegment 注册一个提示词段。
// 计算 content 的 SHA-256 哈希并估算 token 数（~4 字符/token）。
// 若同名段已存在，则覆盖其内容与属性，但不改变注册顺序。
func (m *PromptSegmentManager) RegisterSegment(name string, content string, cacheable bool, priority int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.segments[name]; !exists {
		m.insertionOrder = append(m.insertionOrder, name)
	}
	m.segments[name] = &PromptSegmentV2{
		Name:          name,
		Content:       content,
		Hash:          psmComputeHash(content),
		TokenEstimate: len(content) / 4,
		Cacheable:     cacheable,
		Priority:      priority,
	}
}

// GetSegment 返回指定名称段的副本；若不存在返回 nil。
func (m *PromptSegmentManager) GetSegment(name string) *PromptSegmentV2 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	seg, ok := m.segments[name]
	if !ok {
		return nil
	}
	cp := *seg
	return &cp
}

// ReorderForCache 返回按缓存友好顺序排列的段名称列表。
//
// 排序规则：
//  1. 可缓存（Cacheable）的段排在前面，不可缓存的排在后面。
//  2. 同一可缓存状态下，按 Priority 降序（高优先级在前）。
//  3. Priority 相同的段保持注册相对顺序（稳定排序）。
//
// 每次调用递增重排序计数，并将可缓存段的 token 估算累加到 tokensSaved
// （代表因置于稳定前缀而可被缓存复用的 token）。
func (m *PromptSegmentManager) ReorderForCache() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalReorders++

	// 按注册顺序收集段，确保稳定排序基准
	ordered := make([]*PromptSegmentV2, 0, len(m.insertionOrder))
	for _, name := range m.insertionOrder {
		if seg, ok := m.segments[name]; ok {
			ordered = append(ordered, seg)
		}
	}

	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Cacheable != ordered[j].Cacheable {
			return ordered[i].Cacheable // 可缓存段在前
		}
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority > ordered[j].Priority // 高优先级在前
		}
		return false // 保持注册相对顺序
	})

	cacheableTokens := 0
	names := make([]string, 0, len(ordered))
	for _, seg := range ordered {
		if seg.Cacheable {
			cacheableTokens += seg.TokenEstimate
		}
		names = append(names, seg.Name)
	}
	m.tokensSaved += cacheableTokens

	return names
}

// UpdateSegment 更新指定段的内容并重新计算哈希与 token 估算。
// 若段不存在则返回 false。
func (m *PromptSegmentManager) UpdateSegment(name string, content string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	seg, ok := m.segments[name]
	if !ok {
		return false
	}
	seg.Content = content
	seg.Hash = psmComputeHash(content)
	seg.TokenEstimate = len(content) / 4
	return true
}

// GetStats 返回分段管理器统计信息
func (m *PromptSegmentManager) GetStats() SegmentManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return SegmentManagerStats{
		TotalReorders:   m.totalReorders,
		TokensSaved:     m.tokensSaved,
		SegmentsTracked: len(m.segments),
	}
}

// Reset 重置管理器，清除所有段与统计
func (m *PromptSegmentManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.segments = make(map[string]*PromptSegmentV2)
	m.insertionOrder = nil
	m.totalReorders = 0
	m.tokensSaved = 0
}

// ---------------------------------------------------------------------------
// 内部辅助函数（以 psm 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// psmComputeHash 计算 content 的 SHA-256 哈希并返回十六进制编码字符串。
func psmComputeHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}
