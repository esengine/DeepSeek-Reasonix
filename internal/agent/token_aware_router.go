package agent

import "sync"

// ── OPT-169: TokenAwareRouter (Token 感知路由器) ──
// 基于 token 预算和延迟将请求路由到最优端点。
// 权重 = availableTokens / max(latencyMs, 1)，选择权重最高的可用端点。

// RouteEndpoint 路由端点
type RouteEndpoint struct {
	Name            string
	AvailableTokens int
	LatencyMs       int
	Weight          float64
}

// TokenAwareRouter Token 感知路由器
type TokenAwareRouter struct {
	mu                sync.RWMutex
	endpoints         []RouteEndpoint
	routeCount        int
	totalTokensRouted int
}

// NewTokenAwareRouter 创建 Token 感知路由器
func NewTokenAwareRouter() *TokenAwareRouter {
	return &TokenAwareRouter{
		endpoints: make([]RouteEndpoint, 0),
	}
}

// RegisterEndpoint 注册端点，权重 = availableTokens / max(latencyMs, 1)
func (r *TokenAwareRouter) RegisterEndpoint(name string, availableTokens int, latencyMs int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.endpoints = append(r.endpoints, RouteEndpoint{
		Name:            name,
		AvailableTokens: availableTokens,
		LatencyMs:       latencyMs,
		Weight:          tarComputeWeight(availableTokens, latencyMs),
	})
}

// Route 路由到权重最高的可用端点，返回端点名和是否成功
func (r *TokenAwareRouter) Route(requiredTokens int) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	bestIdx := -1
	bestWeight := 0.0
	for i, ep := range r.endpoints {
		if ep.AvailableTokens >= requiredTokens && ep.Weight > bestWeight {
			bestWeight = ep.Weight
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return "", false
	}

	r.endpoints[bestIdx].AvailableTokens -= requiredTokens
	r.endpoints[bestIdx].Weight = tarComputeWeight(
		r.endpoints[bestIdx].AvailableTokens,
		r.endpoints[bestIdx].LatencyMs,
	)
	r.routeCount++
	r.totalTokensRouted += requiredTokens
	return r.endpoints[bestIdx].Name, true
}

// UpdateEndpointTokens 更新端点的可用 token 数
func (r *TokenAwareRouter) UpdateEndpointTokens(name string, tokens int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.endpoints {
		if r.endpoints[i].Name == name {
			r.endpoints[i].AvailableTokens = tokens
			r.endpoints[i].Weight = tarComputeWeight(tokens, r.endpoints[i].LatencyMs)
			return
		}
	}
}

// GetStats 返回路由器统计信息
func (r *TokenAwareRouter) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return map[string]interface{}{
		"endpointCount":     len(r.endpoints),
		"routeCount":        r.routeCount,
		"totalTokensRouted": r.totalTokensRouted,
	}
}

// Reset 重置路由器
func (r *TokenAwareRouter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.endpoints = make([]RouteEndpoint, 0)
	r.routeCount = 0
	r.totalTokensRouted = 0
}

// tarComputeWeight 计算端点权重 = availableTokens / max(latencyMs, 1)
func tarComputeWeight(availableTokens int, latencyMs int) float64 {
	if latencyMs < 1 {
		latencyMs = 1
	}
	return float64(availableTokens) / float64(latencyMs)
}
