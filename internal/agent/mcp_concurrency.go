package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/config"
)

// MCP concurrency policies. A server is parallel unless something says
// otherwise, which preserves the shared-Host performance tradeoff.
const (
	MCPConcurrencyParallel = "parallel"
	MCPConcurrencySerial   = "serial"
)

// knownStatefulMCPServers are servers whose tools mutate session state the
// protocol does not model — an open page, a selected tab, a cursor. They may
// even declare readOnly, because nothing is written to disk, yet two children
// interleaving on the one shared process still corrupt each other's run.
// Matching is by substring so vendor prefixes and versions still hit.
var knownStatefulMCPServers = []string{
	"browser",
	"playwright",
	"puppeteer",
	"chrome",
	"chromium",
	"selenium",
}

// mcpServerIsSerial reports whether calls to this server must not overlap.
// Explicit configuration always wins; the built-in list is only a conservative
// default for servers known to carry session state.
func mcpServerIsSerial(entry config.PluginEntry) bool {
	switch strings.ToLower(strings.TrimSpace(entry.Concurrency)) {
	case MCPConcurrencySerial:
		return true
	case MCPConcurrencyParallel:
		return false
	}
	name := strings.ToLower(strings.TrimSpace(entry.Name))
	if name == "" {
		return false
	}
	for _, known := range knownStatefulMCPServers {
		if strings.Contains(name, known) {
			return true
		}
	}
	return false
}

// serverIsSerial resolves the policy for a configured server.
func (r *MCPCapabilityRuntime) serverIsSerial(server string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	configured, ok := r.servers[strings.TrimSpace(server)]
	r.mu.RUnlock()
	return ok && mcpServerIsSerial(configured.entry)
}

// mcpServerGates holds one gate per serialized server. It lives on the session
// runtime because the process whose state the calls interleave on is shared at
// exactly that scope.
type mcpServerGates struct {
	mu sync.Mutex
	m  map[string]chan struct{}
}

func (g *mcpServerGates) gate(server string) chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.m == nil {
		g.m = map[string]chan struct{}{}
	}
	if _, ok := g.m[server]; !ok {
		g.m[server] = make(chan struct{}, 1)
	}
	return g.m[server]
}

// withServerGate runs one call with exclusive access to a stateful server.
// Parallel servers run straight through, so the common path is unchanged, and a
// queued call still honours its own cancellation instead of pinning the session
// behind a stuck server.
func (r *MCPCapabilityRuntime) withServerGate(ctx context.Context, server string, execute func() error) error {
	if r == nil || !r.serverIsSerial(server) {
		return execute()
	}
	gate := r.gates.gate(server)
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
		return execute()
	case <-ctx.Done():
		return fmt.Errorf("waiting for exclusive access to MCP server %q: %w", server, ctx.Err())
	}
}
