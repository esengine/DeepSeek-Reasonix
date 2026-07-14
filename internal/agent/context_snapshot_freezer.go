package agent
import "sync"

// ── OPT-178: ContextSnapshotFreezer (上下文快照冻结器) ──
// 冻结上下文消息快照以支持回滚。按 ID 存储消息列表副本，
// 可在需要时恢复、移除或列举快照。当快照数量达到 maxSnapshots 时
// 拒绝新增，保证内存占用可控。

// ContextSnapshotFreezer 上下文快照冻结器，冻结上下文快照以支持回滚。
type ContextSnapshotFreezer struct {
	mu            sync.RWMutex
	snapshots     map[string][]string
	frozenCount   int
	restoredCount int
	maxSnapshots  int
}

// NewContextSnapshotFreezer 创建一个新的上下文快照冻结器。
// maxSnapshots 指定允许冻结的最大快照数量。
func NewContextSnapshotFreezer(maxSnapshots int) *ContextSnapshotFreezer {
	return &ContextSnapshotFreezer{
		snapshots:    make(map[string][]string),
		maxSnapshots: maxSnapshots,
	}
}

// Freeze 冻结指定 ID 的消息快照（存储副本）。
// 若快照数量已达 maxSnapshots 且 ID 不存在则拒绝并返回 false。
// 成功冻结新 ID 时递增 frozenCount 并返回 true；ID 已存在则覆盖旧快照。
func (f *ContextSnapshotFreezer) Freeze(id string, messages []string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	_, exists := f.snapshots[id]
	if !exists && len(f.snapshots) >= f.maxSnapshots {
		return false
	}

	f.snapshots[id] = csfCopyMessages(messages)
	if !exists {
		f.frozenCount++
	}
	return true
}

// Restore 恢复指定 ID 的快照，返回消息副本及是否成功。
// 成功恢复时递增 restoredCount。
func (f *ContextSnapshotFreezer) Restore(id string) ([]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	msgs, ok := f.snapshots[id]
	if !ok {
		return nil, false
	}
	f.restoredCount++
	return csfCopyMessages(msgs), true
}

// Remove 移除指定 ID 的快照。若 ID 不存在则不做任何操作。
func (f *ContextSnapshotFreezer) Remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.snapshots, id)
}

// ListSnapshots 返回所有已冻结快照的 ID 列表。
func (f *ContextSnapshotFreezer) ListSnapshots() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	ids := make([]string, 0, len(f.snapshots))
	for id := range f.snapshots {
		ids = append(ids, id)
	}
	return ids
}

// GetStats 返回冻结器的统计信息，包括 snapshotCount、maxSnapshots、
// frozenCount 和 restoredCount。
func (f *ContextSnapshotFreezer) GetStats() map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return map[string]interface{}{
		"snapshotCount": len(f.snapshots),
		"maxSnapshots":  f.maxSnapshots,
		"frozenCount":   f.frozenCount,
		"restoredCount": f.restoredCount,
	}
}

// Reset 重置冻结器的所有状态，清空快照并重置计数，保留 maxSnapshots 配置。
func (f *ContextSnapshotFreezer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshots = make(map[string][]string)
	f.frozenCount = 0
	f.restoredCount = 0
}

// ── 辅助函数（csf 前缀）──

// csfCopyMessages 返回消息切片的副本，避免外部修改影响存储的快照。
func csfCopyMessages(messages []string) []string {
	if messages == nil {
		return nil
	}
	copied := make([]string, len(messages))
	copy(copied, messages)
	return copied
}
