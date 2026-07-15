package agent
import "sync"

// ── OPT-226: TokenAwareSlotManager (Token感知槽位管理器) ──
// TokenAwareSlotManager 管理处理槽位的分配和释放，
// 在 token 压力下协调并发槽位的使用。
type TokenAwareSlotManager struct {
	mu               sync.RWMutex
	slots            map[int]bool // slotID -> occupied
	totalSlots       int
	allocatedSlots   int
	totalAllocations int
}

// NewTokenAwareSlotManager 创建 Token 感知槽位管理器。
func NewTokenAwareSlotManager(totalSlots int) *TokenAwareSlotManager {
	if totalSlots < 0 {
		totalSlots = 0
	}
	return &TokenAwareSlotManager{
		slots:            make(map[int]bool, totalSlots),
		totalSlots:       totalSlots,
		allocatedSlots:   0,
		totalAllocations: 0,
	}
}

// Acquire 获取一个空闲槽位，返回槽位 ID 与是否成功。
// 当所有槽位均被占用时返回 (-1, false)。
func (m *TokenAwareSlotManager) Acquire() (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	slotID := tasmFindFreeSlot(m.slots, m.totalSlots)
	if slotID < 0 {
		return -1, false
	}
	m.slots[slotID] = true
	m.allocatedSlots++
	m.totalAllocations++
	return slotID, true
}

// Release 释放指定槽位，返回是否释放成功。
// 若槽位未被占用则返回 false。
func (m *TokenAwareSlotManager) Release(slotID int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.slots[slotID] {
		return false
	}
	delete(m.slots, slotID)
	m.allocatedSlots--
	return true
}

// IsOccupied 检查指定槽位是否被占用。
func (m *TokenAwareSlotManager) IsOccupied(slotID int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.slots[slotID]
}

// GetFreeSlotCount 返回当前空闲槽位数量。
func (m *TokenAwareSlotManager) GetFreeSlotCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalSlots - m.allocatedSlots
}

// GetStats 返回槽位管理器的统计信息。
func (m *TokenAwareSlotManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]interface{}{
		"totalSlots":       m.totalSlots,
		"allocatedSlots":   m.allocatedSlots,
		"freeSlots":         m.totalSlots - m.allocatedSlots,
		"totalAllocations": m.totalAllocations,
	}
}

// Reset 重置槽位管理器，清空所有占用并归零统计。
func (m *TokenAwareSlotManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slots = make(map[int]bool, m.totalSlots)
	m.allocatedSlots = 0
	m.totalAllocations = 0
}

// tasmFindFreeSlot 查找第一个空闲槽位，返回槽位 ID；无空闲时返回 -1。
func tasmFindFreeSlot(slots map[int]bool, totalSlots int) int {
	for i := 0; i < totalSlots; i++ {
		if !slots[i] {
			return i
		}
	}
	return -1
}
