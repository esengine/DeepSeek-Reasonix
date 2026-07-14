package agent

import "sync"

// ── OPT-189: TokenAwareLoadBalancer (Token 感知负载均衡器) ──
// 基于 token 负载在多个处理器间均衡请求。每个处理器维护当前 token
// 负载和最大容量，Distribute 时选择负载最低的处理器进行分配，
// Release 时释放对应负载，实现动态均衡。
//
// 原理：在多处理器（如多个 LLM 实例或并发处理单元）场景下，
// 不同请求消耗的 token 数量差异很大。基于 token 负载而非请求
// 数量进行均衡，可以更精确地分配计算资源，避免某个处理器因
// 大请求而过载。
//
// 效果：均衡处理器间 token 负载，提升整体吞吐量，降低单点过载风险。

// LBHandler 负载均衡处理器。
type LBHandler struct {
	Name        string // 处理器名称
	CurrentLoad int    // 当前 token 负载
	MaxCapacity int    // 最大 token 容量
}

// TokenAwareLoadBalancer Token 感知负载均衡器，基于 token 负载在多个处理器间均衡请求。
type TokenAwareLoadBalancer struct {
	mu                  sync.RWMutex
	handlers            []LBHandler
	balancedCount       int
	totalTokensBalanced int
}

// NewTokenAwareLoadBalancer 创建一个新的 Token 感知负载均衡器。
func NewTokenAwareLoadBalancer() *TokenAwareLoadBalancer {
	return &TokenAwareLoadBalancer{
		handlers:            make([]LBHandler, 0),
		balancedCount:       0,
		totalTokensBalanced: 0,
	}
}

// AddHandler 添加处理器。
// name 为处理器名称，maxCapacity 为该处理器的最大 token 容量。
func (lb *TokenAwareLoadBalancer) AddHandler(name string, maxCapacity int) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.handlers = append(lb.handlers, LBHandler{
		Name:        name,
		CurrentLoad: 0,
		MaxCapacity: maxCapacity,
	})
}

// Distribute 将请求分配给负载最低的处理器。
// tokens 为本次请求的 token 数量。若存在可用处理器且分配后不超过
// 其最大容量，则分配并返回处理器名称和 true；否则返回空字符串和 false。
func (lb *TokenAwareLoadBalancer) Distribute(tokens int) (string, bool) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if len(lb.handlers) == 0 {
		return "", false
	}
	idx := talbFindMinLoad(lb.handlers)
	if idx < 0 {
		return "", false
	}
	handler := &lb.handlers[idx]
	// 检查是否有足够容量
	if handler.CurrentLoad+tokens > handler.MaxCapacity {
		return "", false
	}
	handler.CurrentLoad += tokens
	lb.balancedCount++
	lb.totalTokensBalanced += tokens
	return handler.Name, true
}

// Release 释放处理器的负载。
// name 为处理器名称，tokens 为要释放的 token 数量。
// 负载不会降至负数。
func (lb *TokenAwareLoadBalancer) Release(name string, tokens int) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	for i := range lb.handlers {
		if lb.handlers[i].Name == name {
			lb.handlers[i].CurrentLoad -= tokens
			if lb.handlers[i].CurrentLoad < 0 {
				lb.handlers[i].CurrentLoad = 0
			}
			break
		}
	}
}

// GetHandlerLoad 获取处理器当前负载。
// 若处理器不存在则返回 -1。
func (lb *TokenAwareLoadBalancer) GetHandlerLoad(name string) int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	for _, handler := range lb.handlers {
		if handler.Name == name {
			return handler.CurrentLoad
		}
	}
	return -1
}

// GetStats 返回统计信息，包含 handlerCount、balancedCount 和 totalTokensBalanced。
func (lb *TokenAwareLoadBalancer) GetStats() map[string]interface{} {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return map[string]interface{}{
		"handlerCount":        len(lb.handlers),
		"balancedCount":       lb.balancedCount,
		"totalTokensBalanced": lb.totalTokensBalanced,
	}
}

// Reset 重置负载均衡器状态，清空处理器列表和所有统计计数。
func (lb *TokenAwareLoadBalancer) Reset() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.handlers = make([]LBHandler, 0)
	lb.balancedCount = 0
	lb.totalTokensBalanced = 0
}

// talbFindMinLoad 找到负载最低的处理器索引（辅助函数）。
// 若处理器列表为空则返回 -1。
func talbFindMinLoad(handlers []LBHandler) int {
	if len(handlers) == 0 {
		return -1
	}
	minIdx := 0
	minLoad := handlers[0].CurrentLoad
	for i := 1; i < len(handlers); i++ {
		if handlers[i].CurrentLoad < minLoad {
			minLoad = handlers[i].CurrentLoad
			minIdx = i
		}
	}
	return minIdx
}
