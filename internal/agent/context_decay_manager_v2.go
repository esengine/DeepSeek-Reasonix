package agent
import "sync"

// ── OPT-183: ContextDecayManagerV2 (上下文衰减管理器 V2 / Context Decay Manager V2) ──
// 管理上下文信息的时效性衰减。每个上下文项携带重要度，随时间（Tick）
// 递增 age 并按 decayRate 衰减重要度；当 age 超过 maxAge 或重要度降至 0
// 时移除该项。
//
// 与 OPT-73 ContextDecayManager（基于消息年龄的消息级衰减）不同，V2 以
// key→DecayItem 的形式管理上下文项，并提供显式的衰减与过期语义。
//
// 注：因包内已存在 OPT-73 的 ContextDecayManager 类型，本模块以 V2 后缀
// 命名以避免命名冲突，二者可共存。

// DecayItem 上下文衰减项。
type DecayItem struct {
	Key        string  // 上下文项键
	Value      string  // 上下文项值
	Age        int     // 年龄（Tick 次数）
	Importance float64 // 重要度（随衰减递减）
}

// ContextDecayManagerV2 上下文衰减管理器 V2。
type ContextDecayManagerV2 struct {
	mu           sync.RWMutex
	items        map[string]DecayItem
	decayRate    float64
	decayedCount int
	maxAge       int
}

// NewContextDecayManagerV2 创建上下文衰减管理器 V2。
// decayRate 为每次 Tick 衰减的重要度，maxAge 为最大年龄阈值。
func NewContextDecayManagerV2(decayRate float64, maxAge int) *ContextDecayManagerV2 {
	return &ContextDecayManagerV2{
		items:     make(map[string]DecayItem),
		decayRate: decayRate,
		maxAge:    maxAge,
	}
}

// Add 添加一个上下文项，初始 age 为 0。
// key 为项键，value 为项值，importance 为初始重要度。
func (m *ContextDecayManagerV2) Add(key string, value string, importance float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items[key] = DecayItem{
		Key:        key,
		Value:      value,
		Age:        0,
		Importance: importance,
	}
}

// Tick 推进一个时间步：将所有项的 age 递增并按 decayRate 衰减重要度，
// 随后移除 age 超过 maxAge 或重要度降至 0 及以下的项，并累加 decayedCount。
func (m *ContextDecayManagerV2) Tick() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, item := range m.items {
		item.Age++
		item.Importance = cdmApplyDecay(item.Importance, m.decayRate)

		if item.Age > m.maxAge || item.Importance <= 0 {
			delete(m.items, key)
			m.decayedCount++
			continue
		}
		m.items[key] = item
	}
}

// Get 获取指定键的上下文项。
// 若项不存在或已过期（被 Tick 移除）则返回零值与 false。
func (m *ContextDecayManagerV2) Get(key string) (DecayItem, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	item, ok := m.items[key]
	return item, ok
}

// GetStats 返回管理器统计信息，包括 itemCount、decayRate、maxAge 与 decayedCount。
func (m *ContextDecayManagerV2) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"itemCount":    len(m.items),
		"decayRate":    m.decayRate,
		"maxAge":       m.maxAge,
		"decayedCount": m.decayedCount,
	}
}

// Reset 重置管理器状态，清空所有项与计数。
func (m *ContextDecayManagerV2) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items = make(map[string]DecayItem)
	m.decayedCount = 0
}

// cdmApplyDecay 对重要度应用一次衰减。
// 返回 importance - decayRate，且不小于 0。
func cdmApplyDecay(importance float64, decayRate float64) float64 {
	v := importance - decayRate
	if v < 0 {
		return 0
	}
	return v
}
