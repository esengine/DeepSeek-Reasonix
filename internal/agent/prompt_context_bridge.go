package agent
import "sync"

// ── OPT-185: PromptContextBridge (提示上下文桥接器 / Prompt-Context Bridge) ──
// 在提示（prompt）与上下文（context）之间建立双向映射关系。
// 正向映射 promptID→contextIDs，反向映射 contextID→promptIDs，
// 支持链接、解链接与按任一方向查询关联项。

// PromptContextBridge 提示上下文桥接器。
type PromptContextBridge struct {
	mu              sync.RWMutex
	mappings        map[string][]string // promptID → contextIDs
	reverseMappings map[string][]string // contextID → promptIDs
	bridgeCount     int
}

// NewPromptContextBridge 创建提示上下文桥接器。
func NewPromptContextBridge() *PromptContextBridge {
	return &PromptContextBridge{
		mappings:        make(map[string][]string),
		reverseMappings: make(map[string][]string),
	}
}

// Link 在 promptID 与 contextID 之间建立映射关系。
// 若该映射已存在则不重复计数。
func (b *PromptContextBridge) Link(promptID string, contextID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 正向：避免重复添加
	if !pcbContains(b.mappings[promptID], contextID) {
		b.mappings[promptID] = append(b.mappings[promptID], contextID)
		b.bridgeCount++
	}
	// 反向：避免重复添加
	if !pcbContains(b.reverseMappings[contextID], promptID) {
		b.reverseMappings[contextID] = append(b.reverseMappings[contextID], promptID)
	}
}

// Unlink 解除 promptID 与 contextID 之间的映射关系。
// 若映射不存在则不做任何操作。
func (b *PromptContextBridge) Unlink(promptID string, contextID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	before := len(b.mappings[promptID])
	b.mappings[promptID] = pcbRemoveFromSlice(b.mappings[promptID], contextID)
	if len(b.mappings[promptID]) < before {
		b.bridgeCount--
		if len(b.mappings[promptID]) == 0 {
			delete(b.mappings, promptID)
		}
	}
	b.reverseMappings[contextID] = pcbRemoveFromSlice(b.reverseMappings[contextID], promptID)
	if len(b.reverseMappings[contextID]) == 0 {
		delete(b.reverseMappings, contextID)
	}
}

// GetContexts 返回 promptID 关联的所有上下文 ID。
// 返回切片的副本，调用方可安全修改。
func (b *PromptContextBridge) GetContexts(promptID string) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	src := b.mappings[promptID]
	if len(src) == 0 {
		return []string{}
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

// GetPrompts 返回 contextID 关联的所有提示 ID。
// 返回切片的副本，调用方可安全修改。
func (b *PromptContextBridge) GetPrompts(contextID string) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	src := b.reverseMappings[contextID]
	if len(src) == 0 {
		return []string{}
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

// GetStats 返回桥接器统计信息，包括 promptCount、contextCount 与 bridgeCount。
func (b *PromptContextBridge) GetStats() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return map[string]interface{}{
		"promptCount":  len(b.mappings),
		"contextCount": len(b.reverseMappings),
		"bridgeCount":  b.bridgeCount,
	}
}

// Reset 重置桥接器状态，清空所有映射与计数。
func (b *PromptContextBridge) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.mappings = make(map[string][]string)
	b.reverseMappings = make(map[string][]string)
	b.bridgeCount = 0
}

// pcbRemoveFromSlice 从字符串切片中移除第一个等于 val 的元素。
// 若不存在则原样返回。
func pcbRemoveFromSlice(slice []string, val string) []string {
	for i, s := range slice {
		if s == val {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

// pcbContains 判断字符串切片中是否包含 val。
func pcbContains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
