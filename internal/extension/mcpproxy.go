package extension

import (
	"context"
	"fmt"
	"sync"
)

// MCPBackend is one replaceable MCP server connection behind MCPProxy.
// The plugin.Host client satisfies this via a thin adapter at the call site.
type MCPBackend interface {
	Backend
	// Call invokes one tool on this backend.
	Call(ctx context.Context, toolName string, args []byte) ([]byte, error)
}

// MCPProxy is a stable consumer-facing handle for one MCP server name. Tool
// schemas and the provider-visible tool prefix stay owned by RuntimeSnapshot;
// this proxy only swaps the live process/connection underneath.
type MCPProxy struct {
	name  string
	inner *StableProxy
}

// NewMCPProxy returns an empty proxy for the given server name.
func NewMCPProxy(name string) *MCPProxy {
	return &MCPProxy{name: name, inner: NewStableProxy()}
}

// Name returns the stable MCP server identity.
func (p *MCPProxy) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

// Replace swaps the active MCP backend and drains the previous one.
func (p *MCPProxy) Replace(ctx context.Context, next MCPBackend, generation uint64) error {
	if p == nil || p.inner == nil {
		return fmt.Errorf("extension: nil MCPProxy")
	}
	return p.inner.Replace(ctx, next, generation)
}

// Call routes a tool invocation to the active backend. Fail-fast when no
// backend is registered (crash/replace window). In-flight calls cancel when
// the backend is replaced or the proxy is closed (drain).
func (p *MCPProxy) Call(ctx context.Context, toolName string, args []byte) ([]byte, error) {
	if p == nil || p.inner == nil {
		return nil, fmt.Errorf("extension: MCP proxy unavailable")
	}
	var out []byte
	err := p.inner.CallCtx(ctx, func(callCtx context.Context, b Backend) error {
		mcp, ok := b.(MCPBackend)
		if !ok {
			return fmt.Errorf("extension: MCP backend type mismatch")
		}
		var callErr error
		out, callErr = mcp.Call(callCtx, toolName, args)
		return callErr
	})
	return out, err
}

// CancelInFlight aborts outstanding MCP calls (generation drain).
func (p *MCPProxy) CancelInFlight() {
	if p == nil || p.inner == nil {
		return
	}
	p.inner.CancelInFlight()
}

// Close drains the active backend.
func (p *MCPProxy) Close(ctx context.Context) error {
	if p == nil || p.inner == nil {
		return nil
	}
	return p.inner.Close(ctx)
}

// Generation returns the active backend generation.
func (p *MCPProxy) Generation() uint64 {
	if p == nil || p.inner == nil {
		return 0
	}
	return p.inner.Generation()
}

// MCPProxySet is a named registry of stable MCP proxies for one generation.
type MCPProxySet struct {
	mu     sync.Mutex
	byName map[string]*MCPProxy
}

// NewMCPProxySet returns an empty set.
func NewMCPProxySet() *MCPProxySet {
	return &MCPProxySet{byName: make(map[string]*MCPProxy)}
}

// Get returns the stable proxy for name, creating it if needed.
func (s *MCPProxySet) Get(name string) *MCPProxy {
	if s == nil {
		return NewMCPProxy(name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byName == nil {
		s.byName = make(map[string]*MCPProxy)
	}
	if p, ok := s.byName[name]; ok {
		return p
	}
	p := NewMCPProxy(name)
	s.byName[name] = p
	return p
}

// Close drains every proxy.
func (s *MCPProxySet) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	proxies := make([]*MCPProxy, 0, len(s.byName))
	for _, p := range s.byName {
		proxies = append(proxies, p)
	}
	s.byName = nil
	s.mu.Unlock()
	var first error
	for _, p := range proxies {
		if err := p.Close(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}
