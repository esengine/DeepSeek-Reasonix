package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// ── OPT-16: 工具结果记忆化 (Tool Result Memoization) ──
// 缓存只读工具的执行结果，避免同一调用重复消耗 token。
//
// 原理：在 agent 循环中，模型经常重复调用相同的只读工具（如多次 read_file
// 同一文件、多次 grep 同一模式）。每次调用的结果都进入对话历史，消耗
// prompt token。通过记忆化，第二次调用直接返回缓存结果，并在对话历史中
// 用简短占位符替代完整结果（"已缓存，结果与上方相同"），节省 token。
//
// 效果：重复工具调用结果从 N×result_tokens 降到 N×20（占位符），
// 对于频繁读取大文件的场景可节省 50-80% 的工具结果 token。

// ToolResultMemo 缓存只读工具的执行结果
type ToolResultMemo struct {
	mu      sync.RWMutex
	entries map[string]*MemoEntry
	maxSize int
}

// MemoEntry 记忆化条目
type MemoEntry struct {
	Key        string    `json:"key"`
	ToolName   string    `json:"toolName"`
	ArgsHash   string    `json:"argsHash"`
	Result     string    `json:"result"`
	ResultLen  int       `json:"resultLen"`
	HitCount   int       `json:"hitCount"`
	CreatedAt  time.Time `json:"createdAt"`
	LastAccess time.Time `json:"lastAccess"`
}

// NewToolResultMemo 创建工具结果记忆化缓存
func NewToolResultMemo(maxSize int) *ToolResultMemo {
	if maxSize <= 0 {
		maxSize = 50
	}
	return &ToolResultMemo{
		entries: make(map[string]*MemoEntry),
		maxSize: maxSize,
	}
}

// MemoKey 计算工具调用的缓存键
func MemoKey(toolName string, args []byte) string {
	h := sha256.New()
	h.Write([]byte(toolName))
	h.Write(args)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:8])
}

// IsMemoizable 判断工具是否可记忆化（只读工具才可缓存）
func IsMemoizable(toolName string, readOnly bool) bool {
	if !readOnly {
		return false
	}
	switch toolName {
	case "read_file", "read", "cat":
		return true
	case "grep", "search":
		return true
	case "glob", "find":
		return true
	case "ls", "list":
		return true
	case "code_index":
		return true
	default:
		return false
	}
}

// Get 获取缓存的结果
func (m *ToolResultMemo) Get(key string) (*MemoEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.entries[key]
	if ok {
		entry.HitCount++
		entry.LastAccess = time.Now()
	}
	return entry, ok
}

// Put 存储工具结果
func (m *ToolResultMemo) Put(toolName string, args []byte, result string) {
	key := MemoKey(toolName, args)
	m.mu.Lock()
	defer m.mu.Unlock()

	// 如果缓存满了，移除最久未访问的条目
	if len(m.entries) >= m.maxSize {
		var oldestKey string
		var oldestTime time.Time
		for k, e := range m.entries {
			if oldestKey == "" || e.LastAccess.Before(oldestTime) {
				oldestKey = k
				oldestTime = e.LastAccess
			}
		}
		if oldestKey != "" {
			delete(m.entries, oldestKey)
		}
	}

	m.entries[key] = &MemoEntry{
		Key:        key,
		ToolName:   toolName,
		ArgsHash:   key, // 复用 MemoKey 作为 ArgsHash
		Result:     result,
		ResultLen:  len(result),
		HitCount:   0,
		CreatedAt:  time.Now(),
		LastAccess: time.Now(),
	}
}

// GetCachedPlaceholder 返回缓存命中的占位符文本
func GetCachedPlaceholder(entry *MemoEntry) string {
	return "[已缓存: " + entry.ToolName + " 结果同上方调用，共 " +
		formatBytes(entry.ResultLen) + "，如需查看完整内容请参考之前的输出]"
}

// GetStats 获取统计
func (m *ToolResultMemo) GetStats() MemoStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	totalHits := 0
	totalSaved := 0
	for _, e := range m.entries {
		totalHits += e.HitCount
		totalSaved += e.HitCount * e.ResultLen
	}
	return MemoStats{
		CachedItems:   len(m.entries),
		TotalHits:     totalHits,
		TokensSaved:   totalSaved / 4, // 粗略估算
		BytesCached:   totalSaved,
	}
}

// MemoStats 记忆化统计
type MemoStats struct {
	CachedItems int `json:"cachedItems"`
	TotalHits   int `json:"totalHits"`
	TokensSaved int `json:"tokensSaved"`
	BytesCached int `json:"bytesCached"`
}

// Reset 清空缓存
func (m *ToolResultMemo) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = make(map[string]*MemoEntry)
}

func formatBytes(n int) string {
	if n < 1024 {
		return formatInt(n) + "B"
	}
	if n < 1024*1024 {
		return formatInt(n/1024) + "KB"
	}
	return formatInt(n/(1024*1024)) + "MB"
}

func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
