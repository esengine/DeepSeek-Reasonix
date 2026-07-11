package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// ── OPT-09: 并发请求去重 ──
// 当多个并发请求发送几乎相同的输入时（如 auto_plan + executor 并行），
// 去重器合并相同请求，只发送一次 API 调用，结果共享给所有等待者。
// 仅对非流式请求有效（流式请求需要立即输出，无法等待）。

// Deduplicator 是请求去重器
type Deduplicator struct {
	mu       sync.Mutex
	inflight map[string]*dedupEntry
	ttl      time.Duration
}

type dedupEntry struct {
	done chan struct{}
	resp *Response
	err  error
}

// Response 是非流式完成响应（去重器使用）
type Response struct {
	Content string
	Usage   *Usage
}

// NewDeduplicator 创建新的请求去重器
func NewDeduplicator() *Deduplicator {
	return &Deduplicator{
		inflight: make(map[string]*dedupEntry),
		ttl:      5 * time.Second,
	}
}

// RequestKey 计算请求的指纹（仅用前 N 条消息避免大上下文计算开销）
func RequestKey(model string, messages []Message) string {
	h := sha256.New()
	h.Write([]byte(model))
	maxMessages := 5
	if len(messages) < maxMessages {
		maxMessages = len(messages)
	}
	for i := 0; i < maxMessages; i++ {
		h.Write([]byte(messages[i].Content))
		h.Write([]byte(messages[i].Role))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Deduplicate 合并相同请求。如果已有相同请求在飞行中，等待其结果而不是发起新请求。
// send 函数只会在第一个请求时被调用。
func (d *Deduplicator) Deduplicate(key string, send func() (*Response, error)) (*Response, error) {
	d.mu.Lock()
	if existing, ok := d.inflight[key]; ok {
		d.mu.Unlock()
		// 等待已有请求完成
		select {
		case <-existing.done:
			return existing.resp, existing.err
		case <-time.After(d.ttl):
			// 超时，发起新请求
		}
	}

	entry := &dedupEntry{done: make(chan struct{})}
	d.inflight[key] = entry
	d.mu.Unlock()

	resp, err := send()

	entry.resp = resp
	entry.err = err
	close(entry.done)

	// 延迟清理
	time.AfterFunc(d.ttl, func() {
		d.mu.Lock()
		delete(d.inflight, key)
		d.mu.Unlock()
	})

	return resp, err
}

// DefaultDeduplicator 全局默认去重器
var DefaultDeduplicator = NewDeduplicator()
