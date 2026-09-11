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
	prev, err := p.swap(next, generation)
	if err != nil {
		if next != nil {
			next.close()
		}
		return err
	}
	if prev != nil && prev != next && prev.t != nil {
		prev.close()
	}
	_ = ctx
	return nil
}

func (p *serverProxy) swap(next *Client, generation uint64) (*Client, error) {
	if p == nil {
		return nil, fmt.Errorf("plugin: nil server proxy")
	}
	if p.closed.Load() {
		return nil, fmt.Errorf("plugin: server proxy %q closed", p.name)
	}
	p.mu.Lock()
	prev := p.active
	p.active = next
	p.generation = generation
	p.mu.Unlock()
	return prev, nil
}

func (p *serverProxy) client() *Client {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.active
}

// detachIf clears the active backend only when it is the exact instance being
// removed. The caller closes the client after releasing Host.mu.
func (p *serverProxy) detachIf(client *Client) bool {
	if p == nil || client == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active != client {
		return false
	}
	p.active = nil
	return true
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
	// Publish the proxy and clients slice under the same Host lock. Otherwise a
	// scope rollback can remove next after h.mu is released but before p.replace
	// publishes it, leaving the proxy pointed at a closed client.
	prev := p.client()
	if prev == next {
		_, err := p.swap(next, generation)
		h.mu.Unlock()
		return err
	}
	if next != nil {
		if err := h.noteClientFromContext(ctx, next); err != nil {
			h.mu.Unlock()
			next.close()
			return err
		}
	}
	replaced, err := p.swap(next, generation)
	if err != nil {
		if next != nil {
			for i, client := range h.clients {
				if client == next {
					h.clients = append(h.clients[:i], h.clients[i+1:]...)
					break
				}
			}
		}
		h.mu.Unlock()
		if next != nil {
			next.close()
		}
		return err
	}
	if prev != nil {
		for i, client := range h.clients {
			if client == prev {
				h.clients = append(h.clients[:i], h.clients[i+1:]...)
				break
			}
		}
	}
	h.mu.Unlock()
	if replaced != nil && replaced != next && replaced.t != nil {
		replaced.close()
	}
	return nil
}

func (h *Host) lookupClient(name string) *Client {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lookupClientLocked(name)
}

// lookupClientLocked returns the active exact client. Caller holds h.mu for
// read or write; proxy.client has its own leaf lock.
func (h *Host) lookupClientLocked(name string) *Client {
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
