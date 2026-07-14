package agent

import (
	"sync"

	"reasonix/internal/provider"
)

// ── OPT-108: ContextSnapshotManager (上下文快照管理器) ──
// 定期快照上下文消息列表，用于在异常或回滚场景下恢复上下文。
//
// 原理：按 turn 编号存储 []provider.Message 快照。当快照数量超过
// maxSnapshots 时自动淘汰最旧（turn 最小）的快照，保证内存占用
// 可控。Restore 可按 turn 精确恢复指定快照。
//
// 效果：在上下文损坏、压缩失败或需要回退时提供可靠的恢复点，
// 避免丢失全部对话历史。

// ContextSnapshotManager 上下文快照管理器。
type ContextSnapshotManager struct {
	mu               sync.RWMutex
	snapshots        map[int][]provider.Message
	maxSnapshots     int
	totalSnapshots   int
	restored         int
	lastSnapshotTurn int
}

// NewContextSnapshotManager 创建新的上下文快照管理器。
// maxSnapshots 为保留的最大快照数量。
func NewContextSnapshotManager(maxSnapshots int) *ContextSnapshotManager {
	return &ContextSnapshotManager{
		snapshots:    make(map[int][]provider.Message),
		maxSnapshots: maxSnapshots,
	}
}

// TakeSnapshot 存储指定 turn 的消息快照。
// 当快照数量超过 maxSnapshots 时删除最旧（turn 最小）的快照。
func (m *ContextSnapshotManager) TakeSnapshot(turn int, messages []provider.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 复制消息切片，避免外部修改影响快照
	copied := make([]provider.Message, len(messages))
	copy(copied, messages)

	m.snapshots[turn] = copied
	m.totalSnapshots++
	m.lastSnapshotTurn = turn

	// 超过上限时淘汰最旧的快照
	for len(m.snapshots) > m.maxSnapshots {
		minTurn := csmFindMinTurn(m.snapshots)
		delete(m.snapshots, minTurn)
	}
}

// Restore 恢复指定 turn 的快照。若找不到则返回 nil。
// 每次成功恢复递增 restored 计数。
func (m *ContextSnapshotManager) Restore(turn int) []provider.Message {
	m.mu.Lock()
	defer m.mu.Unlock()

	msgs, ok := m.snapshots[turn]
	if !ok {
		return nil
	}
	m.restored++

	// 返回副本，避免外部修改影响存储的快照
	copied := make([]provider.Message, len(msgs))
	copy(copied, msgs)
	return copied
}

// GetLatestTurn 返回最新快照的 turn 编号。
// 若没有任何快照则返回 -1。
func (m *ContextSnapshotManager) GetLatestTurn() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.snapshots) == 0 {
		return -1
	}
	maxTurn := -1
	for turn := range m.snapshots {
		if turn > maxTurn {
			maxTurn = turn
		}
	}
	return maxTurn
}

// GetStats 返回快照管理器统计信息，包括 totalSnapshots、restored、
// maxSnapshots、currentSnapshots 和 lastSnapshotTurn。
func (m *ContextSnapshotManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"totalSnapshots":   m.totalSnapshots,
		"restored":         m.restored,
		"maxSnapshots":     m.maxSnapshots,
		"currentSnapshots": len(m.snapshots),
		"lastSnapshotTurn": m.lastSnapshotTurn,
	}
}

// Reset 重置快照管理器状态，清空所有快照和计数。
func (m *ContextSnapshotManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshots = make(map[int][]provider.Message)
	m.totalSnapshots = 0
	m.restored = 0
	m.lastSnapshotTurn = 0
}

// csmFindMinTurn 在快照 map 中查找最小的 turn 编号。
func csmFindMinTurn(snapshots map[int][]provider.Message) int {
	minTurn := -1
	first := true
	for turn := range snapshots {
		if first || turn < minTurn {
			minTurn = turn
			first = false
		}
	}
	return minTurn
}
