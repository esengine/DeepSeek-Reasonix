package agent

import "sync"

// ── OPT-146: TokenAwareDispatcher (Token 感知分发器) ──
// 根据 token 预算将任务分发到不同处理器。每个处理器拥有独立的
// token 预算，Dispatch 时选择剩余预算最多且能容纳该任务的处理器。
// 所有处理器预算不足时返回空字符串表示拒绝。

// DispatchHandler 分发处理器，具有独立的 token 预算和任务队列。
type DispatchHandler struct {
	// Name 是处理器的名称标识。
	Name string
	// Budget 是该处理器的 token 总预算。
	Budget int
	// Used 是该处理器已使用的 token 数量。
	Used int
	// Queue 是该处理器待处理的任务队列。
	Queue []string
}

// TokenAwareDispatcher Token 感知分发器，根据 token 预算分发任务到不同处理器。
type TokenAwareDispatcher struct {
	mu               sync.RWMutex
	handlers         map[string]*DispatchHandler
	totalDispatched  int
	totalRouted      int
	totalRejected    int
	budgetPerHandler int
}

// NewTokenAwareDispatcher 创建一个新的 Token 感知分发器。
// budgetPerHandler 指定每个处理器的 token 预算。
func NewTokenAwareDispatcher(budgetPerHandler int) *TokenAwareDispatcher {
	return &TokenAwareDispatcher{
		handlers:         make(map[string]*DispatchHandler),
		budgetPerHandler: budgetPerHandler,
	}
}

// RegisterHandler 注册一个具有指定名称的新处理器。
// 新处理器使用分发器配置的 budgetPerHandler 作为其预算。
func (d *TokenAwareDispatcher) RegisterHandler(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[name] = &DispatchHandler{
		Name:   name,
		Budget: d.budgetPerHandler,
		Used:   0,
		Queue:  []string{},
	}
}

// Dispatch 将任务分发到剩余预算最多且能容纳 tokenEstimate 的处理器。
// 返回被分发到的处理器名称；若所有处理器预算不足则返回空字符串。
func (d *TokenAwareDispatcher) Dispatch(task string, tokenEstimate int) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.totalDispatched++

	handler := tdpFindBestHandler(d.handlers, tokenEstimate)
	if handler == nil {
		d.totalRejected++
		return ""
	}

	handler.Used += tokenEstimate
	handler.Queue = append(handler.Queue, task)
	d.totalRouted++
	return handler.Name
}

// GetHandlerLoad 返回每个处理器已使用的预算。
func (d *TokenAwareDispatcher) GetHandlerLoad() map[string]int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return tdpCopyHandlerLoad(d.handlers)
}

// GetStats 返回分发器的统计信息，包括 totalDispatched、totalRouted、
// totalRejected、handlerCount 和 avgUtilization。
func (d *TokenAwareDispatcher) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	avgUtil := 0.0
	if len(d.handlers) > 0 {
		totalUtil := 0.0
		for _, h := range d.handlers {
			if h.Budget > 0 {
				totalUtil += float64(h.Used) / float64(h.Budget)
			}
		}
		avgUtil = totalUtil / float64(len(d.handlers))
	}

	return map[string]interface{}{
		"totalDispatched": d.totalDispatched,
		"totalRouted":     d.totalRouted,
		"totalRejected":   d.totalRejected,
		"handlerCount":    len(d.handlers),
		"avgUtilization":  avgUtil,
	}
}

// Reset 重置分发器的所有状态，包括处理器列表和统计计数。
func (d *TokenAwareDispatcher) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers = make(map[string]*DispatchHandler)
	d.totalDispatched = 0
	d.totalRouted = 0
	d.totalRejected = 0
}

// tdpFindBestHandler 查找剩余预算最多且能容纳 tokenEstimate 的处理器。
// 若没有处理器能容纳该任务，返回 nil。
func tdpFindBestHandler(handlers map[string]*DispatchHandler, tokenEstimate int) *DispatchHandler {
	var best *DispatchHandler
	maxRemaining := -1
	for _, h := range handlers {
		remaining := h.Budget - h.Used
		if remaining >= tokenEstimate && remaining > maxRemaining {
			maxRemaining = remaining
			best = h
		}
	}
	return best
}

// tdpCopyHandlerLoad 复制每个处理器的已用预算，返回 name->used 的映射。
func tdpCopyHandlerLoad(handlers map[string]*DispatchHandler) map[string]int {
	result := make(map[string]int, len(handlers))
	for name, h := range handlers {
		result[name] = h.Used
	}
	return result
}
