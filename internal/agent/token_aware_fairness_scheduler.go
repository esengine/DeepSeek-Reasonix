package agent

import "sync"

// ── OPT-224: TokenAwareFairnessScheduler (Token感知公平调度器) ──
// 按租户维度维护 token 请求队列，并以轮询方式公平出队，确保各租户公平
// 获得 token 配额。单次出队最多服务 maxPerTurn 个 token，超出部分回填
// 队首等待下一轮，防止单个大请求长时间占用调度。
//
// 原理：每轮 Dequeue 按 servedCount % 租户数 选择下一个租户，弹出其队首
// 请求；若请求 token 数超过 maxPerTurn，则仅服务 maxPerTurn 并将余量回填。
//
// 效果：在多租户场景下实现公平的 token 配额调度，避免饥饿。

// TokenAwareFairnessScheduler Token感知公平调度器。
type TokenAwareFairnessScheduler struct {
	mu          sync.RWMutex
	queues      map[string][]int // tenant → token requests
	servedCount int
	totalServed int
	maxPerTurn  int
}

// NewTokenAwareFairnessScheduler 创建调度器，maxPerTurn 小于 1 时取 1。
func NewTokenAwareFairnessScheduler(maxPerTurn int) *TokenAwareFairnessScheduler {
	if maxPerTurn < 1 {
		maxPerTurn = 1
	}
	return &TokenAwareFairnessScheduler{
		queues:     make(map[string][]int),
		maxPerTurn: maxPerTurn,
	}
}

// Enqueue 将 token 请求入队到指定租户（空租户名将被忽略）。
func (s *TokenAwareFairnessScheduler) Enqueue(tenant string, tokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if tenant == "" {
		return
	}
	if tokens < 0 {
		tokens = 0
	}
	s.queues[tenant] = append(s.queues[tenant], tokens)
}

// Dequeue 公平出队（轮询各租户），返回租户名、本次服务的 token 数与是否成功。
// 若请求超过 maxPerTurn，仅服务 maxPerTurn 并将余量回填该租户队首。
func (s *TokenAwareFairnessScheduler) Dequeue() (string, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.queues) == 0 {
		return "", 0, false
	}

	tenant := tafsFindNextTenant(s.queues, s.servedCount)
	if tenant == "" {
		return "", 0, false
	}

	queue := s.queues[tenant]
	if len(queue) == 0 {
		delete(s.queues, tenant)
		return "", 0, false
	}

	requested := queue[0]
	served := requested
	remainder := 0
	if served > s.maxPerTurn {
		remainder = served - s.maxPerTurn
		served = s.maxPerTurn
	}

	if remainder > 0 {
		// 余量回填队首
		newQueue := make([]int, 0, len(queue))
		newQueue = append(newQueue, remainder)
		newQueue = append(newQueue, queue[1:]...)
		s.queues[tenant] = newQueue
	} else {
		s.queues[tenant] = queue[1:]
	}

	if len(s.queues[tenant]) == 0 {
		delete(s.queues, tenant)
	}

	s.servedCount++
	s.totalServed += served
	return tenant, served, true
}

// GetQueueDepth 返回指定租户的队列深度。
func (s *TokenAwareFairnessScheduler) GetQueueDepth(tenant string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.queues[tenant])
}

// GetStats 返回统计信息：tenantCount、servedCount、totalServed、maxPerTurn。
func (s *TokenAwareFairnessScheduler) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"tenantCount": len(s.queues),
		"servedCount": s.servedCount,
		"totalServed": s.totalServed,
		"maxPerTurn":  s.maxPerTurn,
	}
}

// Reset 重置调度器，清除所有队列与统计（保留 maxPerTurn）。
func (s *TokenAwareFairnessScheduler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.queues = make(map[string][]int)
	s.servedCount = 0
	s.totalServed = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 tafs 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// tafsFindNextTenant 按轮询位置选择下一个有非空队列的租户。
// 租户按名称排序以保证确定性，从 servedCount % 租户数 起始扫描。
func tafsFindNextTenant(queues map[string][]int, servedCount int) string {
	if len(queues) == 0 {
		return ""
	}

	names := make([]string, 0, len(queues))
	for name := range queues {
		names = append(names, name)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}

	start := servedCount % len(names)
	for i := 0; i < len(names); i++ {
		name := names[(start+i)%len(names)]
		if len(queues[name]) > 0 {
			return name
		}
	}
	return ""
}
