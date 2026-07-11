package provider

import (
	"context"
	"sync"
	"time"
)

// ── OPT-09 集成: 流式请求去重包装器 ──
// DeduplicatingProvider 包装一个 Provider，对流式请求进行去重。
// 当多个并发请求具有相同的请求指纹时，只有第一个请求实际调用 API，
// 后续请求等待第一个完成后重放缓冲的 chunks。
//
// 使用方式：
//   prov = NewDeduplicatingProvider(prov)
//   agent := New(opts.WithProvider(prov))

// DeduplicatingProvider 流式请求去重包装器
type DeduplicatingProvider struct {
	inner Provider
	dedup *streamDedup
}

// streamDedup 流式去重器
type streamDedup struct {
	mu       sync.Mutex
	inflight map[string]*streamEntry
	ttl      time.Duration
}

type streamEntry struct {
	done     chan struct{}
	chunks   []Chunk
	err      error
	mu       sync.Mutex
}

// NewDeduplicatingProvider 创建去重包装器
func NewDeduplicatingProvider(inner Provider) *DeduplicatingProvider {
	return &DeduplicatingProvider{
		inner: inner,
		dedup: &streamDedup{
			inflight: make(map[string]*streamEntry),
			ttl:      10 * time.Second,
		},
	}
}

// Name 返回底层 provider 的名称
func (d *DeduplicatingProvider) Name() string {
	return d.inner.Name()
}

// Stream 启动流式完成，如果已有相同请求在飞行中则重放缓冲结果
func (d *DeduplicatingProvider) Stream(ctx context.Context, req Request) (<-chan Chunk, error) {
	key := streamRequestKey(d.inner.Name(), req)

	d.dedup.mu.Lock()
	if existing, ok := d.dedup.inflight[key]; ok {
		d.dedup.mu.Unlock()
		// 等待已有请求完成或超时
		select {
		case <-existing.done:
			return d.replayChunks(existing), existing.err
		case <-time.After(d.dedup.ttl):
			// 超时，继续发起新请求
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		d.dedup.mu.Lock()
	}

	entry := &streamEntry{done: make(chan struct{})}
	d.dedup.inflight[key] = entry
	d.dedup.mu.Unlock()

	// 第一个请求：实际调用底层 provider
	innerCh, err := d.inner.Stream(ctx, req)
	if err != nil {
		entry.err = err
		close(entry.done)
		d.scheduleCleanup(key)
		return nil, err
	}

	// 创建输出 channel，同时缓冲 chunks
	outCh := make(chan Chunk, 64)
	go func() {
		defer close(outCh)
		defer close(entry.done)
		defer d.scheduleCleanup(key)
		for chunk := range innerCh {
			entry.mu.Lock()
			entry.chunks = append(entry.chunks, chunk)
			entry.mu.Unlock()
			outCh <- chunk
		}
	}()

	return outCh, nil
}

// replayChunks 从已有请求的缓冲中重放 chunks
func (d *DeduplicatingProvider) replayChunks(entry *streamEntry) <-chan Chunk {
	outCh := make(chan Chunk, 64)
	go func() {
		defer close(outCh)
		entry.mu.Lock()
		chunks := make([]Chunk, len(entry.chunks))
		copy(chunks, entry.chunks)
		entry.mu.Unlock()
		for _, chunk := range chunks {
			outCh <- chunk
		}
	}()
	return outCh
}

// scheduleCleanup 延迟清理过期的 inflight 条目
func (d *DeduplicatingProvider) scheduleCleanup(key string) {
	time.AfterFunc(d.dedup.ttl, func() {
		d.dedup.mu.Lock()
		delete(d.dedup.inflight, key)
		d.dedup.mu.Unlock()
	})
}

// streamRequestKey 计算流式请求的指纹
func streamRequestKey(providerName string, req Request) string {
	return RequestKey(providerName, req.Messages)
}
