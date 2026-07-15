package agent

import "sync"

// ── OPT-221: TokenAwareWeightedRoundRobin (Token感知加权轮询器) ──
// 基于权重在多个处理器间进行加权轮询分配。每个处理器按其权重在轮询序列中
// 出现相应次数，从而实现按权重比例的请求分发。
//
// 原理：将所有处理器按权重展开为线性序列（权重为 N 的处理器在序列中出现
// N 次），随后按 currentIndex 顺序循环选取。权重越大，被选中的频率越高。
//
// 效果：在多处理器场景下实现可控的按权重负载分配，避免低权重处理器过载，
// 同时保留分发统计以支持监控与调优。

// WRRHandler 描述一个参与加权轮询的处理器。
type WRRHandler struct {
	Name   string
	Weight int
}

// TokenAwareWeightedRoundRobin Token感知加权轮询器。
type TokenAwareWeightedRoundRobin struct {
	mu              sync.RWMutex
	handlers        map[string]int // name → weight
	currentIndex    int
	totalWeight     int
	dispatchedCount int
}

// NewTokenAwareWeightedRoundRobin 创建加权轮询器。
func NewTokenAwareWeightedRoundRobin() *TokenAwareWeightedRoundRobin {
	return &TokenAwareWeightedRoundRobin{
		handlers: make(map[string]int),
	}
}

// AddHandler 添加处理器（若已存在则更新其权重）。
func (w *TokenAwareWeightedRoundRobin) AddHandler(name string, weight int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if weight < 0 {
		weight = 0
	}
	if old, exists := w.handlers[name]; exists {
		w.totalWeight -= old
	}
	w.handlers[name] = weight
	w.totalWeight += weight
}

// Next 按加权轮询策略选择下一个处理器，返回处理器名称与是否成功。
// 若无处理器或总权重为 0，返回 ("", false)。
func (w *TokenAwareWeightedRoundRobin) Next() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.handlers) == 0 || w.totalWeight == 0 {
		return "", false
	}

	seq := tawrrBuildSequence(w.handlers)
	if len(seq) == 0 {
		return "", false
	}

	if w.currentIndex >= len(seq) {
		w.currentIndex = 0
	}

	name := seq[w.currentIndex]
	w.currentIndex++
	w.dispatchedCount++
	return name, true
}

// GetHandlers 返回当前已注册的处理器列表（按名称排序以保证确定性）。
func (w *TokenAwareWeightedRoundRobin) GetHandlers() []WRRHandler {
	w.mu.RLock()
	defer w.mu.RUnlock()

	list := make([]WRRHandler, 0, len(w.handlers))
	for name, weight := range w.handlers {
		list = append(list, WRRHandler{Name: name, Weight: weight})
	}
	// 按名称排序以保证输出确定性
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].Name < list[i].Name {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	return list
}

// GetStats 返回统计信息：handlerCount、totalWeight、dispatchedCount、currentIndex。
func (w *TokenAwareWeightedRoundRobin) GetStats() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return map[string]interface{}{
		"handlerCount":    len(w.handlers),
		"totalWeight":     w.totalWeight,
		"dispatchedCount": w.dispatchedCount,
		"currentIndex":    w.currentIndex,
	}
}

// Reset 重置轮询器，清除所有处理器与统计。
func (w *TokenAwareWeightedRoundRobin) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.handlers = make(map[string]int)
	w.currentIndex = 0
	w.totalWeight = 0
	w.dispatchedCount = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 tawrr 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// tawrrBuildSequence 根据处理器权重构建加权轮询序列。
// 权重为 N 的处理器在序列中出现 N 次，序列按处理器名称排序以保证确定性。
func tawrrBuildSequence(handlers map[string]int) []string {
	names := make([]string, 0, len(handlers))
	for name := range handlers {
		names = append(names, name)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}

	seq := make([]string, 0, len(handlers))
	for _, name := range names {
		weight := handlers[name]
		for k := 0; k < weight; k++ {
			seq = append(seq, name)
		}
	}
	return seq
}
