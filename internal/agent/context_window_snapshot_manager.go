package agent
import "sync"

// OPT-238: ContextWindowSnapshotManager — 上下文窗口快照管理器
// ContextWindowSnapshotManager manages historical snapshots of the context window.
// It allows taking snapshots at specific points and restoring to a previous state,
// enabling rollback capabilities for context window management.
type CWSnapshot struct {
	ID         int   // 快照唯一标识符 snapshot unique identifier
	TokenCount int   // 快照时的token数量 token count at snapshot time
	Timestamp  int64 // 快照时间戳 snapshot timestamp
}

// ContextWindowSnapshotManager manages context window snapshots for rollback and inspection.
// ContextWindowSnapshotManager 管理上下文窗口的历史快照。
type ContextWindowSnapshotManager struct {
	mu            sync.RWMutex
	snapshots     []CWSnapshot // 快照列表 list of snapshots
	maxSnapshots  int          // 最大快照数量 maximum number of snapshots to retain
	snapshotCount int          // 累计拍摄的快照总数 total snapshots ever taken
	restoreCount  int          // 累计恢复次数 total restore operations performed
}

// NewContextWindowSnapshotManager creates a new ContextWindowSnapshotManager with the given max snapshots.
// NewContextWindowSnapshotManager 使用给定的最大快照数量创建新的ContextWindowSnapshotManager。
func NewContextWindowSnapshotManager(maxSnapshots int) *ContextWindowSnapshotManager {
	return &ContextWindowSnapshotManager{
		snapshots:     make([]CWSnapshot, 0, maxSnapshots),
		maxSnapshots:  maxSnapshots,
		snapshotCount: 0,
		restoreCount:  0,
	}
}

// TakeSnapshot takes a snapshot with the given token count and timestamp, returning the snapshot ID.
// TakeSnapshot 拍摄快照，返回快照ID。
func (m *ContextWindowSnapshotManager) TakeSnapshot(tokenCount int, timestamp int64) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.snapshotCount++
	id := m.snapshotCount
	snapshot := CWSnapshot{
		ID:         id,
		TokenCount: tokenCount,
		Timestamp:  timestamp,
	}
	m.snapshots = append(m.snapshots, snapshot)

	// Evict oldest snapshots if exceeding max capacity.
	// 超过最大容量时淘汰最旧的快照。
	if len(m.snapshots) > m.maxSnapshots {
		m.snapshots = m.snapshots[len(m.snapshots)-m.maxSnapshots:]
	}
	return id
}

// Restore restores the context window to the snapshot with the given ID.
// Returns the snapshot and true if found, or a zero snapshot and false otherwise.
// Restore 恢复到指定快照。
func (m *ContextWindowSnapshotManager) Restore(id int) (CWSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot, found := cwsmFindSnapshot(m.snapshots, id)
	if found {
		m.restoreCount++
		return snapshot, true
	}
	return CWSnapshot{}, false
}

// GetLatestSnapshot returns the most recently taken snapshot.
// GetLatestSnapshot 获取最新快照。
func (m *ContextWindowSnapshotManager) GetLatestSnapshot() (CWSnapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.snapshots) == 0 {
		return CWSnapshot{}, false
	}
	return m.snapshots[len(m.snapshots)-1], true
}

// GetSnapshotCount returns the number of currently stored snapshots.
// GetSnapshotCount 返回快照数量。
func (m *ContextWindowSnapshotManager) GetSnapshotCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.snapshots)
}

// GetStats returns statistics about the snapshot manager.
// GetStats 返回快照管理器的统计信息。
func (m *ContextWindowSnapshotManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	latestTokenCount := 0
	if len(m.snapshots) > 0 {
		latestTokenCount = m.snapshots[len(m.snapshots)-1].TokenCount
	}
	return map[string]interface{}{
		"maxSnapshots":     m.maxSnapshots,
		"snapshotCount":    len(m.snapshots),
		"restoreCount":     m.restoreCount,
		"latestTokenCount": latestTokenCount,
	}
}

// Reset resets the snapshot manager to its initial state (preserving max snapshots config).
// Reset 重置快照管理器到初始状态（保留最大快照数量配置）。
func (m *ContextWindowSnapshotManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.snapshots = make([]CWSnapshot, 0, m.maxSnapshots)
	m.snapshotCount = 0
	m.restoreCount = 0
}

// cwsmFindSnapshot searches for a snapshot by ID in the given slice.
// cwsmFindSnapshot 在快照列表中查找指定ID的快照。
func cwsmFindSnapshot(snapshots []CWSnapshot, id int) (CWSnapshot, bool) {
	for _, s := range snapshots {
		if s.ID == id {
			return s, true
		}
	}
	return CWSnapshot{}, false
}
