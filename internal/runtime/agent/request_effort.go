package agent

import (
	"context"
	"strings"
	"sync/atomic"

	"reasonix/internal/contract/provider"
)

// requestEffortState is the reasoning depth every main-loop request carries when
// the session changed it after the provider was built; empty means the effort the
// provider was built with.
type requestEffortState struct{ level atomic.Pointer[string] }

func (s *requestEffortState) set(level string) {
	if level == "" {
		s.level.Store(nil)
		return
	}
	s.level.Store(&level)
}

func (s *requestEffortState) get() string {
	if p := s.level.Load(); p != nil {
		return *p
	}
	return ""
}

// RequestEffortLevels are the depths this session's provider takes per request;
// empty when it takes only the effort it was built with.
func (a *Agent) RequestEffortLevels() []string {
	if p, ok := a.svc.prov.(provider.RequestEffortProvider); ok {
		return p.RequestEfforts()
	}
	return nil
}

// SetRequestEffort makes later main requests carry level; "" returns to the
// provider's built effort. It rides the request, never the prompt, so the cached
// prefix is unchanged. The caller checked the level against RequestEffortLevels.
func (a *Agent) SetRequestEffort(level string) {
	a.sess.effort.set(strings.ToLower(strings.TrimSpace(level)))
}

// RequestEffort is the depth the next main request carries over the built one,
// "" when none.
func (a *Agent) RequestEffort() string { return a.sess.effort.get() }

type requestEffortKey struct{}

// WithRequestEffort carries the session's switched depth into a tool call, so a
// sub-agent that inherits its parent's effort inherits the one in force now and
// not the one the parent's provider was built with.
func WithRequestEffort(ctx context.Context, level string) context.Context {
	if level == "" {
		return ctx
	}
	return context.WithValue(ctx, requestEffortKey{}, level)
}
