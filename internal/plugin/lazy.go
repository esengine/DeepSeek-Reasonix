// MCP placeholder tools. Background startup registers cheap placeholder entries
// in the tool registry at boot — using the on-disk schema cache when it exists —
// and kicks the real subprocess spawn / handshake immediately. By the time the
// model calls a tool, the connection is usually already up.
//
// Cache-hit placeholders are PINNED for the whole session: they present the
// cached names/descriptions/schemas from boot onward and forward Execute to the
// real tools once the handshake completes, but the registry entries themselves
// are never replaced. The provider request's tools array is part of the cached
// prompt prefix, so swapping in live tools mid-session — whenever the live
// handshake differed from the cache — invalidated the whole conversation's
// provider cache at 10x miss pricing. Live drift lands in the schema cache and
// surfaces next session. Only the cache-miss connect stub still swaps (there
// was nothing real to present), a one-time cost per server.
package plugin

import (
	"context"
	"encoding/json"
	"errors"
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

// kick starts the spawn if it has not yet started. Background registration calls
// this immediately; tests may leave it idle to exercise the placeholder path.
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
	var cacheTools []tool.Tool
	s.mu.Lock()
	if err != nil {
		if errors.Is(err, ErrSpawningInFlight) {
			// Another tab is already spawning this server; reset to idle so
			// the next call retries instead of recording a spurious failure.
			s.state = spawnIdle
			s.spawnErr = nil
			s.mu.Unlock()
			return
		}
		if IsServerAlreadyConnected(err) {
			// The server was already started by another controller sharing
			// the same host. Fetch the tools from the existing client
			// instead of entering the failed state.
			if tools, err2 := s.host.ToolsFor(s.ctx, s.spec.Name); err2 == nil {
				s.real = make(map[string]tool.Tool, len(tools))
				for _, t := range tools {
					s.real[t.Name()] = t
				}
				s.state = spawnReady
				s.trySwap()
				cacheTools = tools
				s.mu.Unlock()
				saveLazyCachedSchema(s.spec, cacheTools)
				return
			}
			// ToolsFor failed — still not a real failure; just mark failed
			// without recording it so /mcp status stays clean.
			s.state = spawnFailed
			s.spawnErr = err
			s.mu.Unlock()
			return
		}
		s.state = spawnFailed
		s.spawnErr = err
		s.host.RecordFailure(s.spec, err)
		s.mu.Unlock()
		return
	}
	s.real = make(map[string]tool.Tool, len(real))
	for _, t := range real {
		s.real[t.Name()] = t
	}
	s.state = spawnReady
	s.trySwap()
	cacheTools = real
	s.mu.Unlock()
	saveLazyCachedSchema(s.spec, cacheTools)
}

func saveLazyCachedSchema(spec Spec, real []tool.Tool) {
	_ = SaveCachedSchema(spec.Name, CachedSchema{
		SpecHash:     SpecFingerprint(spec),
		Capabilities: map[string]bool{"tools": len(real) > 0},
		Tools:        cacheableToolsOf(real),
	})
}

// trySwap publishes the real tools after a successful spawn. Caller must hold
// s.mu.
//
// Cache-miss placeholders (removePrefix set) genuinely swap: the single
// "<server>__connect" stub is dropped and the real tools register under their
// own names — a one-time tool-set change per server, unavoidable because no
// schema existed to present earlier.
//
// Cache-hit placeholders do NOT touch the registry. The lazyTools already
// carry the cached names/descriptions/schemas the model has seen since boot,
// and Execute forwards to the real tool once ready — swapping in the live
// tools would rewrite the request's tools array mid-session whenever the live
// handshake differs from the cache (description tweaks, schema upgrades, new
// tools), invalidating the provider prefix cache at 10x miss pricing. The
// live result still lands in the schema cache (saveLazyCachedSchema), so the
// NEXT session presents the updated surface — freshness deferred one session
// in exchange for byte-stable tool bytes within this one, same trade the
// environment-probe snapshot makes for the system prompt.
func (s *lazySpawn) trySwap() {
	if s.swapped || s.state != spawnReady {
		return
	}
	if s.removePrefix != "" {
		s.reg.RemovePrefix(s.removePrefix)
		for _, t := range s.real {
			s.reg.Add(t)
		}
	}
	s.swapped = true
}

// lazyTool is a tool.Tool placeholder backed by a shared lazySpawn. The model
// sees cached metadata (or a stub when no cache exists); Execute consults the
// state machine, kicking off the handshake on first call.
type lazyTool struct {
	shared   *lazySpawn
	name     string // namespaced "mcp__<server>__<tool>"
	rawName  string // original server-local tool name, when cached
	desc     string
	schema   json.RawMessage
	readOnly bool
	// readOnlyTrusted mirrors remoteTool: true only for a first-party
	// ReadOnlyToolNames override, so plan mode can tell trusted first-party
	// read-only from an untrusted server readOnlyHint.
	readOnlyTrusted bool
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
func (lt *lazyTool) MCPServerName() string {
	if lt.shared == nil {
		return ""
	}
	return lt.shared.spec.Name
}
func (lt *lazyTool) MCPRawToolName() string { return lt.rawName }

// PlanModeUntrustedReadOnly mirrors remoteTool: true when ReadOnly() is true only
// from an untrusted server readOnlyHint, false for a first-party override.
func (lt *lazyTool) PlanModeUntrustedReadOnly() bool {
	return lt.readOnly && !lt.readOnlyTrusted
}
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
		// forward through. Bound it with a start timeout so a wedged or
		// unreachable MCP server can't hang the whole turn indefinitely
		// (#4806) — on timeout we fail this attempt and a later turn can retry.
		sp.state = spawnInFlight
		sp.mu.Unlock()
		spawnCtx, cancel := context.WithTimeout(sp.ctx, defaultStartTimeout)
		real, err := sp.host.AddWithLifecycle(sp.ctx, spawnCtx, sp.spec)
		cancel()
		sp.mu.Lock()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				// A slow cold start can succeed on a later turn after npm/node
				// caches warm up or a remote MCP endpoint responds. Do not pin
				// the session into spawnFailed for a transient startup budget miss.
				sp.state = spawnIdle
				sp.spawnErr = nil
				sp.mu.Unlock()
				return "", fmt.Errorf("MCP server %q startup timed out — retry this tool on a later turn", sp.spec.Name)
			}
			if errors.Is(err, ErrSpawningInFlight) {
				// Another tab is already spawning this server on the shared
				// host, but this lazySpawn has no goroutine that can publish
				// that result. Reset to idle so the next call can reuse the
				// connected client once the other spawn finishes.
				sp.state = spawnIdle
				sp.spawnErr = nil
				sp.mu.Unlock()
				return "", fmt.Errorf("MCP server %q is being started by another tab — retry on next turn", sp.spec.Name)
			}
			if IsServerAlreadyConnected(err) {
				// Another tab on the shared host already started the
				// server. Fetch the tools from the existing client.
				if tools, err2 := sp.host.ToolsFor(ctx, sp.spec.Name); err2 == nil {
					sp.real = make(map[string]tool.Tool, len(tools))
					for _, t := range tools {
						sp.real[t.Name()] = t
					}
					sp.state = spawnReady
					sp.trySwap()
					r := sp.real[lt.name]
					if r != nil {
						// Unlock before forwarding so the lock isn't held
						// during Execute (matching the spawnReady pattern).
						sp.mu.Unlock()
						return r.Execute(ctx, args)
					}
				}
				// ToolsFor failed — not our fault, don't record as failure.
				sp.state = spawnFailed
				sp.spawnErr = err
				sp.mu.Unlock()
				return "", fmt.Errorf("MCP server %q failed to start: %w", sp.spec.Name, err)
			}
			sp.state = spawnFailed
			sp.spawnErr = err
			sp.host.RecordFailure(sp.spec, err)
			sp.mu.Unlock()
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
			sp.mu.Unlock()
			return "", fmt.Errorf("MCP server %q did not expose tool %q (the cached schema may be stale)", sp.spec.Name, lt.name)
		}
		sp.mu.Unlock()
		return r.Execute(ctx, args)
	}

	sp.mu.Unlock()
	return "", fmt.Errorf("deferred plugin %q in unexpected state", sp.spec.Name)
}

// LazyToolset returns the placeholder tools to register for one background spec.
// The name is historical: when cs is non-nil (cache hit) the returned slice has
// one lazyTool per cached tool, carrying the cached schema so the model can pass
// real args. If the background handshake is still pending, Execute waits for it
// and swaps in real tools. When cs is nil (cache miss) the returned slice has a
// single stub named "mcp__<server>__connect": the model can call it to wait for
// the spawn, and the real tools surface on the next turn.
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
	// A snapshot with zero tools presents nothing the model could call, so it
	// gets the same connect stub as a cache miss — otherwise the live tools
	// would silently join the registry mid-session with no placeholder names
	// reserved for them.
	if cs == nil || len(cs.Tools) == 0 {
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
			trusted := spec.toolReadOnlyTrusted(ct.Name, visibleName)
			out = append(out, &lazyTool{
				shared:          shared,
				name:            toolName(spec.Name, visibleName),
				rawName:         ct.Name,
				desc:            ct.Description,
				schema:          ct.Schema,
				readOnly:        spec.toolReadOnly(ct.Name, visibleName, ct.ReadOnly),
				readOnlyTrusted: trusted,
				hasCache:        true,
			})
		}
	}

	if kick {
		shared.kick()
	}
	return out
}
