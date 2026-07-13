package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// ── OPT-17: 对话历史去重 (Conversation Deduplication) ──
// 检测并移除对话历史中的重复内容，减少 prompt token。
//
// 原理：在长对话中，模型经常引用之前的内容（如重复粘贴大段代码、
// 重复执行相同命令并得到相同输出）。这些重复内容占据大量 token
// 但不增加信息量。通过指纹去重，检测到完全相同的消息后用简短
// 引用替代，节省 token。
//
// 效果：长对话中可减少 20-40% 的历史 token，特别是代码编辑场景
// 中模型经常反复查看同一文件。

// ConversationDeduplicator 对话历史去重器
type ConversationDeduplicator struct {
	mu           sync.RWMutex
	fingerprints map[string]*DedupEntry // 指纹 → 条目
	enabled      bool
	minLength    int // 最小去重长度（短于此不去重）
}

// DedupEntry 去重条目
type DedupEntry struct {
	Hash       string    `json:"hash"`
	MessageIdx int       `json:"messageIdx"` // 首次出现的消息索引
	ContentLen int       `json:"contentLen"`
	HitCount   int       `json:"hitCount"`
	FirstSeen  time.Time `json:"firstSeen"`
}

// NewConversationDeduplicator 创建对话历史去重器
func NewConversationDeduplicator() *ConversationDeduplicator {
	return &ConversationDeduplicator{
		fingerprints: make(map[string]*DedupEntry),
		enabled:      true,
		minLength:     200, // 200 字符以上才去重
	}
}

// HashContent 计算消息内容的指纹
// 规范化：去除首尾空白、统一换行符
func HashContent(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.TrimSpace(normalized)
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:8])
}

// ShouldDeduplicate 判断消息是否应该去重
func (d *ConversationDeduplicator) ShouldDeduplicate(content string) (bool, *DedupEntry) {
	if !d.enabled || len(content) < d.minLength {
		return false, nil
	}

	hash := HashContent(content)

	d.mu.RLock()
	defer d.mu.RUnlock()

	if entry, ok := d.fingerprints[hash]; ok {
		entry.HitCount++
		return true, entry
	}
	return false, nil
}

// Record 记录一条消息的指纹
func (d *ConversationDeduplicator) Record(content string, messageIdx int) {
	if !d.enabled || len(content) < d.minLength {
		return
	}

	hash := HashContent(content)

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.fingerprints[hash]; !ok {
		d.fingerprints[hash] = &DedupEntry{
			Hash:       hash,
			MessageIdx: messageIdx,
			ContentLen: len(content),
			HitCount:   0,
			FirstSeen:  time.Now(),
		}
	}
}

// GetDedupPlaceholder 返回去重后的占位符
func GetDedupPlaceholder(entry *DedupEntry) string {
	return "[内容与第 " + formatInt(entry.MessageIdx+1) +
		" 条消息相同，共 " + formatBytes(entry.ContentLen) +
		"，已省略以节省 token]"
}

// GetStats 获取统计
func (d *ConversationDeduplicator) GetStats() DedupStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	totalDeduped := 0
	totalSaved := 0
	for _, e := range d.fingerprints {
		if e.HitCount > 0 {
			totalDeduped += e.HitCount
			totalSaved += e.HitCount * e.ContentLen
		}
	}
	return DedupStats{
		TrackedMessages: len(d.fingerprints),
		DedupedMessages: totalDeduped,
		TokensSaved:     totalSaved / 4,
	}
}

// DedupStats 去重统计
type DedupStats struct {
	TrackedMessages int `json:"trackedMessages"`
	DedupedMessages int `json:"dedupedMessages"`
	TokensSaved     int `json:"tokensSaved"`
}

// Reset 清空去重器
func (d *ConversationDeduplicator) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fingerprints = make(map[string]*DedupEntry)
}

// SetEnabled 启用/禁用去重
func (d *ConversationDeduplicator) SetEnabled(enabled bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.enabled = enabled
}
