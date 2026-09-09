package main

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
