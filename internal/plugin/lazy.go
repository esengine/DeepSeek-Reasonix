// Lazy-tier MCP placeholder tools. A "lazy" plugin registers cheap placeholder
// entries in the tool registry at boot — using the on-disk schema cache when
// it exists — and defers the actual subprocess spawn / handshake to the first
// model call. A "background" plugin is identical except it also kicks the
// spawn off at boot so by the time the model calls, the swap is already done.
//
// Why the indirection: a lazy/background server still needs stable placeholder
// tools before the real handshake finishes. Once it does finish, lazySpawn swaps
// the placeholders for real tools through tool.Registry's own lock, so the next
// model request sees the real schemas without waiting for another placeholder
// Execute call.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"reasonix/internal/tool"
)

// DefaultStartupBudget is the per-plugin latency budget used by boot when
// deciding whether to auto-demote (see Recommend). Kept here rather than in
// stats.go because it's the value boot.go pairs with each Recommend call.
func DefaultStartupBudget() time.Duration { return defaultStartTimeout }

// spawnState is the lazy-spawn state machine. Transitions are:
//
//	idle → inFlight → ready
//	idle → inFlight → failed
//
// All transitions are gated by lazySpawn.mu so only one goroutine runs the
// handshake even when multiple Execute calls race on first use.
type spawnState int

const (
	spawnIdle spawnState = iota
	spawnInFlight
	spawnReady
	spawnFailed
)

// lazySpawn is shared by every placeholder lazyTool registered for one
// server: they all observe the same state machine and trigger at most one
// handshake.
type lazySpawn struct {
	spec Spec
	host *Host
	reg  *tool.Registry
	ctx  context.Context // session-scoped — outlives any single turn

	mu       sync.Mutex
	state    spawnState
	real     map[string]tool.Tool // namespaced name → real tool, populated on success
	spawnErr error
	swapped  bool
	// removePrefix is set for cache-miss placeholders so trySwap drops the
	// single "<server>__connect" stub before re-registering the real tools
	// under their actual namespaced names. Cache-hit placeholders use the
	// same names as the real tools, so reg.Add overwrites in place and no
	// prefix removal is needed.
	removePrefix string
}

// kick starts the spawn if it has not yet started. Used by background-tier
// registration; lazy-tier kicks on first call instead.
func (s *lazySpawn) kick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != spawnIdle {
		return
	}
	if !s.host.beginDeferredSpawn() {
		s.state = spawnFailed
		s.spawnErr = fmt.Errorf("plugin host is closed")
		return
	}
	s.state = spawnInFlight
	go func() {
		defer s.host.endDeferredSpawn()
		s.run()
	}()
}

// run does the handshake without holding mu (host.Add can take seconds), then
// reacquires mu to publish the result.
func (s *lazySpawn) run() {
	real, err := s.host.Add(s.ctx, s.spec)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.state = spawnFailed
		s.spawnErr = err
		s.host.RecordFailure(s.spec, err)
		return
	}
	s.real = make(map[string]tool.Tool, len(real))
	for _, t := range real {
		s.real[t.Name()] = t
	}
	s.state = spawnReady
	s.trySwap()
}

// trySwap installs the real tools into reg if the spawn is ready and the
// swap hasn't happened. Caller must hold s.mu.
func (s *lazySpawn) trySwap() {
	if s.swapped || s.state != spawnReady {
		return
	}
	if s.removePrefix != "" {
		s.reg.RemovePrefix(s.removePrefix)
	}
	for _, t := range s.real {
		s.reg.Add(t)
	}
	s.swapped = true
}

// lazyTool is a tool.Tool placeholder backed by a shared lazySpawn. The model
// sees cached metadata (or a stub when no cache exists); Execute consults the
// state machine, kicking off the handshake on first call.
type lazyTool struct {
	shared   *lazySpawn
	name     string // namespaced "mcp__<server>__<tool>"
	desc     string
	schema   json.RawMessage
	readOnly bool
	// hasCache true → schema is trusted, so Execute runs the handshake
	// synchronously and forwards in one turn. false → schema is empty, so we
	// can't honour the model's call; we kick the spawn async and ask for a
	// retry on the next turn, when the swap will have installed the real
	// tools with real schemas.
	hasCache bool
}

func (lt *lazyTool) Name() string        { return lt.name }
func (lt *lazyTool) Description() string { return lt.desc }
func (lt *lazyTool) ReadOnly() bool      { return lt.readOnly }
func (lt *lazyTool) Schema() json.RawMessage {
	if len(lt.schema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return canonicalizeSchema(lt.schema)
}

func (lt *lazyTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	sp := lt.shared
	sp.mu.Lock()

	// Catch up on a background spawn that finished while we were idle.
	if sp.state == spawnReady && !sp.swapped {
		sp.trySwap()
	}

	switch sp.state {
	case spawnReady:
		real := sp.real[lt.name]
		sp.mu.Unlock()
		if real == nil {
			return "", fmt.Errorf("MCP server %q did not expose tool %q (the cached schema may be stale)", sp.spec.Name, lt.name)
		}
		return real.Execute(ctx, args)

	case spawnFailed:
		err := sp.spawnErr
		sp.mu.Unlock()
		return "", fmt.Errorf("MCP server %q failed to start: %w", sp.spec.Name, err)

	case spawnInFlight:
		sp.mu.Unlock()
		return "", fmt.Errorf("MCP server %q is still initializing — call this tool again on the next turn", sp.spec.Name)

	case spawnIdle:
		if !lt.hasCache {
			// Cache-miss: we don't trust args to match a real schema, so
			// drive the handshake async and ask the model to retry. By the
			// next turn the swap will have installed the real tools with
			// real schemas under different names.
			if !sp.host.beginDeferredSpawn() {
				sp.state = spawnFailed
				sp.spawnErr = fmt.Errorf("plugin host is closed")
				sp.mu.Unlock()
				return "", fmt.Errorf("MCP server %q failed to start: %w", sp.spec.Name, sp.spawnErr)
			}
			sp.state = spawnInFlight
			go func() {
				defer sp.host.endDeferredSpawn()
				sp.run()
			}()
			sp.mu.Unlock()
			return "", fmt.Errorf("MCP server %q is initializing on first use — call again on the next turn for its real tools", sp.spec.Name)
		}
		// Cache-hit: run the handshake synchronously so this one Execute can
		// forward through.
		sp.state = spawnInFlight
		sp.mu.Unlock()
		real, err := sp.host.Add(sp.ctx, sp.spec)
		sp.mu.Lock()
		defer sp.mu.Unlock()
		if err != nil {
			sp.state = spawnFailed
			sp.spawnErr = err
			sp.host.RecordFailure(sp.spec, err)
			return "", fmt.Errorf("MCP server %q failed to start: %w", sp.spec.Name, err)
		}
		sp.real = make(map[string]tool.Tool, len(real))
		for _, t := range real {
			sp.real[t.Name()] = t
		}
		sp.state = spawnReady
		sp.trySwap()
		r := sp.real[lt.name]
		if r == nil {
			return "", fmt.Errorf("MCP server %q did not expose tool %q (the cached schema may be stale)", sp.spec.Name, lt.name)
		}
		return r.Execute(ctx, args)
	}

	sp.mu.Unlock()
	return "", fmt.Errorf("lazy plugin %q in unexpected state", sp.spec.Name)
}

// LazyToolset returns the placeholder tools to register for one lazy/background
// spec. When cs is non-nil (cache hit) the returned slice has one lazyTool per
// cached tool, carrying the cached schema so the model can pass real args;
// the first Execute runs the handshake synchronously and swaps in real tools.
// When cs is nil (cache miss) the returned slice has a single stub named
// "mcp__<server>__connect": the model can call it to drive the spawn, and the
// real tools surface on the next turn.
//
// kick=true (background tier) also fires off the spawn immediately, so an
// idle session warms up without waiting for the first model call.
//
// host is the Host that receives the real Client. reg is the registry where
// real tools land after a successful spawn. sessionCtx must outlive any
// single Execute (use the controller's PluginCtx) — a turn-scoped ctx would
// kill the stdio child between turns.
func LazyToolset(spec Spec, cs *CachedSchema, host *Host, reg *tool.Registry, sessionCtx context.Context, kick bool) []tool.Tool {
	spawnCtx, cancel := context.WithCancel(sessionCtx)
	host.registerDeferredCancel(cancel)
	shared := &lazySpawn{
		spec: spec,
		host: host,
		reg:  reg,
		ctx:  spawnCtx,
	}

	var out []tool.Tool
	if cs == nil {
		shared.removePrefix = ToolPrefix(spec.Name)
		out = []tool.Tool{&lazyTool{
			shared:   shared,
			name:     shared.removePrefix + "connect",
			desc:     fmt.Sprintf("Connect MCP server %q. Call this once to drive the handshake; the server's real tools become available on the next turn.", spec.Name),
			hasCache: false,
		}}
	} else {
		out = make([]tool.Tool, 0, len(cs.Tools))
		for _, ct := range cs.Tools {
			visibleName := ct.Name
			if spec.StripRawPrefix != "" {
				visibleName = strings.TrimPrefix(visibleName, spec.StripRawPrefix)
			}
			out = append(out, &lazyTool{
				shared:   shared,
				name:     toolName(spec.Name, visibleName),
				desc:     ct.Description,
				schema:   ct.Schema,
				readOnly: spec.toolReadOnly(ct.Name, ct.ReadOnly),
				hasCache: true,
			})
		}
	}

	if kick {
		shared.kick()
	}
	return out
}
