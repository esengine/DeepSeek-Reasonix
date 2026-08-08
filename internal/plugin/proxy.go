package plugin

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// serverProxy is a stable handle for one MCP server name. Consumers keep the
// same tool names while the live *Client rolls underneath.
type serverProxy struct {
	name       string
	mu         sync.RWMutex
	active     *Client
	generation uint64
	closed     atomic.Bool
}

func newServerProxy(name string) *serverProxy {
	return &serverProxy{name: name}
}

func (p *serverProxy) replace(ctx context.Context, next *Client, generation uint64) error {
	if p == nil {
		return fmt.Errorf("plugin: nil server proxy")
	}
	if p.closed.Load() {
		if next != nil {
			next.close()
		}
		return fmt.Errorf("plugin: server proxy %q closed", p.name)
	}
	p.mu.Lock()
	prev := p.active
	p.active = next
	p.generation = generation
	p.mu.Unlock()
	if prev != nil && prev != next && prev.t != nil {
		prev.close()
	}
	_ = ctx
	return nil
}

func (p *serverProxy) client() *Client {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.active
}

func (p *serverProxy) close() {
	if p == nil || !p.closed.CompareAndSwap(false, true) {
		return
	}
	p.mu.Lock()
	c := p.active
	p.active = nil
	p.mu.Unlock()
	if c != nil && c.t != nil {
		c.close()
	}
}

func closeServerProxies(proxies map[string]*serverProxy) {
	for _, p := range proxies {
		p.close()
	}
}

// CancelInFlightMCP is a best-effort drain hook: closes active proxied
// backends for every server so generation drain can abort mid-call work.
// Ordinary tool calls do not yet track per-call context cancel on Host;
// ReplaceServerBackend still closes the previous client.
func (h *Host) CancelInFlightMCP() {
	if h == nil {
		return
	}
	h.mu.Lock()
	proxies := make([]*serverProxy, 0, len(h.proxies))
	for _, p := range h.proxies {
		proxies = append(proxies, p)
	}
	h.mu.Unlock()
	for _, p := range proxies {
		// Close active client to abort stdio/HTTP transport reads.
		if c := p.client(); c != nil && c.t != nil {
			c.close()
		}
	}
}

// ReplaceServerBackend swaps the live client for name behind a stable proxy.
// Tool schemas and names stay owned by the registry; only the connection moves.
// generation is the runtime generation performing the replace.
func (h *Host) ReplaceServerBackend(ctx context.Context, name string, next *Client, generation uint64) error {
	if h == nil {
		return fmt.Errorf("plugin: nil Host")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("plugin: empty server name")
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		if next != nil {
			next.close()
		}
		return fmt.Errorf("plugin: host closed")
	}
	if h.proxies == nil {
		h.proxies = make(map[string]*serverProxy)
	}
	p := h.proxies[name]
	if p == nil {
		p = newServerProxy(name)
		h.proxies[name] = p
	}
	// Keep clients slice consistent: remove previous, append next.
	if prev := p.client(); prev != nil {
		for i, c := range h.clients {
			if c == prev {
				h.clients = append(h.clients[:i], h.clients[i+1:]...)
				break
			}
		}
	}
	if next != nil {
		h.clients = append(h.clients, next)
	}
	h.mu.Unlock()
	return p.replace(ctx, next, generation)
}

func (h *Host) lookupClient(name string) *Client {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed {
		return nil
	}
	if h.proxies != nil {
		if p := h.proxies[name]; p != nil {
			if c := p.client(); c != nil {
				return c
			}
		}
	}
	for _, c := range h.clients {
		if c.name == name {
			return c
		}
	}
	return nil
}
