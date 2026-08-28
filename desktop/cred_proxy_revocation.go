package main

import "strings"

// revokeRoutes serializes revocation with credential resolution and route
// publication. A route update that already started must finish before its
// matching token is removed, so it cannot republish stale credentials after
// the user-facing clear or provider-removal operation returns.
func (p *credentialProxy) revokeRoutes(match func(*credProxyRoute) bool) {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	for token, route := range p.routes {
		if route != nil && match(route) {
			delete(p.routes, token)
		}
	}
}

// revokeRouteLocked removes one route while the caller holds updateMu.
func (p *credentialProxy) revokeRouteLocked(token string) {
	p.mu.Lock()
	delete(p.routes, token)
	p.mu.Unlock()
}

func (a *App) revokeCredentialProxyRoutesByCredential(apiKeyEnv string) {
	apiKeyEnv = strings.TrimSpace(apiKeyEnv)
	if apiKeyEnv == "" {
		return
	}
	a.credProxyMu.Lock()
	proxy := a.credProxy
	a.credProxyMu.Unlock()
	if proxy != nil {
		proxy.revokeRoutes(func(route *credProxyRoute) bool { return route.apiKeyEnv == apiKeyEnv })
	}
}

func (a *App) revokeCredentialProxyRoutesByProvider(names []string) {
	targets := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			targets[name] = struct{}{}
		}
	}
	if len(targets) == 0 {
		return
	}
	a.credProxyMu.Lock()
	proxy := a.credProxy
	a.credProxyMu.Unlock()
	if proxy != nil {
		proxy.revokeRoutes(func(route *credProxyRoute) bool {
			_, remove := targets[route.provider]
			return remove
		})
	}
}
