package agent

import (
	"context"
	"reasonix/internal/provider"
)

func (a *Agent) withProviderCacheSession(ctx context.Context) context.Context {
	// A bound path is stable across resume. Ephemeral/sub-agent sessions use
	// their own instance identity, never a parent session's inherited header.
	identity := a.SessionPath()
	if identity == "" {
		s := a.Session()
		s.mu.Lock()
		if s.cacheSessionID == "" {
			s.cacheSessionID = provider.NewCacheSessionID()
		}
		identity = s.cacheSessionID
		s.mu.Unlock()
	}
	return provider.WithCacheSession(ctx, identity)
}
