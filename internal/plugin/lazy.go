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
	spec       Spec
	host       *Host
	reg        *tool.Registry
	ctx        context.Context // session-scoped — outlives any single turn
	generation uint64

	mu       sync.Mutex
	state    spawnState
	real     map[string]tool.Tool // namespaced name → real tool, populated on success
	spawnErr error
	swapped  bool
	// ready is closed when state leaves spawnInFlight so concurrent waiters can
	// observe the result without killing a shared host process.
	ready chan struct{}
	// removePrefix is set for cache-miss placeholders so trySwap drops the
	// single "<server>__connect" stub before re-registering the real tools
	// under their actual namespaced names. Cache-hit placeholders use the
	// same names as the real tools, so reg.Add overwrites in place and no
	// prefix removal is needed.
	removePrefix string
}

// beginInFlight transitions idle → inFlight and creates the waiter channel.
// Caller must hold s.mu. Returns false when the host is closed.
func (s *lazySpawn) beginInFlight() bool {
	if !s.host.beginDeferredSpawn() {
		s.state = spawnFailed
		s.spawnErr = fmt.Errorf("plugin host is closed")
		s.broadcastReady()
		return false
	}
	s.state = spawnInFlight
	s.ready = make(chan struct{})
	return true
}

// broadcastReady closes the current ready channel if any. Caller holds s.mu.
func (s *lazySpawn) broadcastReady() {
	if s.ready != nil {
		close(s.ready)
		s.ready = nil
	}
}

// kick starts the spawn if it has not yet started. Cache-miss catalog discovery
// and tests may call this; cache-hit boot registration uses kick=false so the
// process starts on first real tool call via EnsureConnected.
func (s *lazySpawn) kick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != spawnIdle {
		return
	}
	if !s.beginInFlight() {
		return
	}
	go func() {
		defer s.host.endDeferredSpawn()
		s.run()
	}()
}

// run does the handshake without holding mu (host.EnsureConnected can take
// seconds), then reacquires mu to publish the result.
func (s *lazySpawn) run() {
	real, err := s.host.EnsureConnectedWithLifecycle(s.ctx, s.ctx, s.spec, s.generation)
	var cacheTools []tool.Tool
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if errors.Is(err, ErrDeferredSpawnCancelled) || errors.Is(err, context.Canceled) {
			s.state = spawnFailed
			s.spawnErr = err
			s.broadcastReady()
			return
		}
		s.state = spawnFailed
		s.spawnErr = err
		s.host.RecordFailure(s.spec, err)
		s.broadcastReady()
		return
	}
	s.real = make(map[string]tool.Tool, len(real))
	for _, t := range real {
		s.real[t.Name()] = t
	}
	s.state = spawnReady
	s.trySwap()
	cacheTools = real
	s.broadcastReady()
	// Save cache outside the critical path of tool dispatch. Register the write
	// before this deferred spawn ends so Host.Close observes and drains it.
	s.host.queueBackgroundWrite(func() {
		saveLazyCachedSchema(s.spec, cacheTools)
	})
}

func saveLazyCachedSchema(spec Spec, real []tool.Tool) {
	_ = SaveCachedSchema(spec.Name, CachedSchema{
		CacheKey:     SchemaCacheKey(spec),
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
	shared      *lazySpawn
	name        string // namespaced "mcp__<server>__<tool>"
	rawName     string // original server-local tool name, when cached
	visibleName string // raw name after configured prefix stripping
	desc        string
	schema      json.RawMessage
	// readOnly is guarded by shared.mu because a live handshake can demote a
	// stale cached reader before asking the model to retry.
	readOnly bool
	// destructive is guarded by shared.mu because a live handshake may promote
	// a stale cached false value before asking the model to retry.
	destructive bool
	// hasCache true → schema is trusted, so Execute runs the handshake
	// synchronously and forwards in one turn. false → schema is empty, so we
	// can't honour the model's call; we kick the spawn async and ask for a
	// retry on the next turn, when the swap will have installed the real
	// tools with real schemas.
	hasCache bool
}

func (lt *lazyTool) Name() string        { return lt.name }
func (lt *lazyTool) Description() string { return lt.desc }
func (lt *lazyTool) ReadOnly() bool {
	if lt.shared == nil {
		return lt.readOnly
	}
	lt.shared.mu.Lock()
	defer lt.shared.mu.Unlock()
	return lt.readOnly
}
func (lt *lazyTool) MCPServerName() string {
	if lt.shared == nil {
		return ""
	}
	return lt.shared.spec.Name
}
func (lt *lazyTool) MCPRawToolName() string     { return lt.rawName }
func (lt *lazyTool) MCPVisibleToolName() string { return lt.visibleName }
func (lt *lazyTool) MCPPackageName() string {
	if lt.shared == nil {
		return ""
	}
	return lt.shared.spec.Package
}

func (lt *lazyTool) MCPServerAuthorized() bool {
	return lt.shared != nil && lt.shared.spec.ServerAuthorized()
}

func (lt *lazyTool) MCPDestructiveHint() bool {
	if lt.shared == nil {
		return lt.destructive
	}
	lt.shared.mu.Lock()
	defer lt.shared.mu.Unlock()
	return lt.destructive
}
func (lt *lazyTool) Schema() json.RawMessage {
	if len(lt.schema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return canonicalizeSchema(lt.schema)
}

func (lt *lazyTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	sp := lt.shared
	for {
		sp.mu.Lock()

		// Catch up on a background spawn that finished while we were idle.
		if sp.state == spawnReady && !sp.swapped {
			sp.trySwap()
		}

		switch sp.state {
		case spawnReady:
			if !lt.hasCache {
				sp.mu.Unlock()
				return fmt.Sprintf("MCP server %q is connected; its real tools are now available on the next turn", sp.spec.Name), nil
			}
			real := sp.real[lt.name]
			safetyErr := lt.reconcileLiveSafety(real)
			sp.mu.Unlock()
			if real == nil {
				return "", fmt.Errorf("MCP server %q did not expose tool %q (the cached schema may be stale)", sp.spec.Name, lt.name)
			}
			if safetyErr != nil {
				return "", safetyErr
			}
			return real.Execute(ctx, args)

		case spawnFailed:
			err := sp.spawnErr
			sp.mu.Unlock()
			return "", fmt.Errorf("MCP server %q failed to start: %w", sp.spec.Name, err)

		case spawnInFlight:
			// Wait for the in-flight handshake (same process for every waiter).
			// Cancelling ctx only abandons this wait; the shared host spawn continues.
			wait := sp.ready
			sp.mu.Unlock()
			if wait == nil {
				continue
			}
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return "", ctx.Err()
			case <-sp.ctx.Done():
				return "", sp.ctx.Err()
			}

		case spawnIdle:
			if !lt.hasCache {
				// Cache-miss: we don't trust args to match a real schema, so
				// drive the handshake async and ask the model to retry. By the
				// next turn the swap will have installed the real tools with
				// real schemas under different names.
				if !sp.beginInFlight() {
					err := sp.spawnErr
					sp.mu.Unlock()
					return "", fmt.Errorf("MCP server %q failed to start: %w", sp.spec.Name, err)
				}
				go func() {
					defer sp.host.endDeferredSpawn()
					sp.run()
				}()
				sp.mu.Unlock()
				return "", fmt.Errorf("MCP server %q is initializing on first use — call again on the next turn for its real tools", sp.spec.Name)
			}
			// Cache-hit: run the handshake synchronously via EnsureConnected so
			// concurrent parent/child/tab callers share one process and waiters
			// complete in the same turn. Bound startup so a wedged server cannot
			// hang the turn forever (#4806).
			if !sp.beginInFlight() {
				err := sp.spawnErr
				sp.mu.Unlock()
				return "", fmt.Errorf("MCP server %q failed to start: %w", sp.spec.Name, err)
			}
			sp.mu.Unlock()

			spawnCtx, cancel := context.WithTimeout(sp.ctx, defaultStartTimeout)
			real, err := sp.host.EnsureConnectedWithLifecycle(sp.ctx, spawnCtx, sp.spec, sp.generation)
			cancel()

			sp.mu.Lock()
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					// Transient startup budget miss: allow a later turn to retry.
					sp.state = spawnIdle
					sp.spawnErr = nil
					sp.broadcastReady()
					sp.mu.Unlock()
					sp.host.endDeferredSpawn()
					return "", fmt.Errorf("MCP server %q startup timed out — retry this tool on a later turn", sp.spec.Name)
				}
				if errors.Is(err, ErrDeferredSpawnCancelled) || errors.Is(err, context.Canceled) {
					sp.state = spawnFailed
					sp.spawnErr = err
					sp.broadcastReady()
					sp.mu.Unlock()
					sp.host.endDeferredSpawn()
					return "", fmt.Errorf("MCP server %q failed to start: %w", sp.spec.Name, err)
				}
				sp.state = spawnFailed
				sp.spawnErr = err
				sp.host.RecordFailure(sp.spec, err)
				sp.broadcastReady()
				sp.mu.Unlock()
				sp.host.endDeferredSpawn()
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
				sp.broadcastReady()
				sp.mu.Unlock()
				sp.host.endDeferredSpawn()
				return "", fmt.Errorf("MCP server %q did not expose tool %q (the cached schema may be stale)", sp.spec.Name, lt.name)
			}
			safetyErr := lt.reconcileLiveSafety(r)
			sp.broadcastReady()
			sp.mu.Unlock()
			sp.host.queueBackgroundWrite(func() {
				saveLazyCachedSchema(sp.spec, real)
			})
			sp.host.endDeferredSpawn()
			if safetyErr != nil {
				return "", safetyErr
			}
			return r.Execute(ctx, args)
		}

		sp.mu.Unlock()
		return "", fmt.Errorf("deferred plugin %q in unexpected state", sp.spec.Name)
	}
}

// reconcileLiveSafety updates a pinned cache-hit placeholder when the live
// server becomes stricter. Caller must hold shared.mu. The current call always
// stops on a reader-to-writer demotion or destructive promotion so the next
// attempt re-enters the agent's Plan/read-only safety checks with current metadata.
func (lt *lazyTool) reconcileLiveSafety(real tool.Tool) error {
	if real == nil {
		return nil
	}
	live, err := ReconcileCachedToolSafety(lt.shared.spec.Name, lt.rawName, CachedToolSafety{
		ReadOnly:    lt.readOnly,
		Destructive: lt.destructive,
	}, real)
	lt.readOnly = live.ReadOnly
	lt.destructive = live.Destructive
	return err
}

// LazyToolset returns the placeholder tools to register for one enabled MCP.
// When cs is non-nil (cache hit) the returned slice has one lazyTool per cached
// tool, carrying the cached schema so the model can pass real args. Execute
// waits for EnsureConnected and completes the call in the same turn. When cs is
// nil (cache miss) the returned slice has a single stub named
// "mcp__<server>__connect": the model can call it to drive the handshake, and
// the real tools surface on the next turn.
//
// kick=true starts a one-shot catalog discovery immediately (used for cache-miss
// servers at boot). kick=false leaves the process idle until the first real
// tool call — the product default for cache-hit sessions.
//
// host is the Host that receives the real Client. reg is the registry where
// real tools land after a successful spawn. sessionCtx must outlive any
// single Execute (use the controller's PluginCtx) — a turn-scoped ctx would
// kill the stdio child between turns.
func LazyToolset(spec Spec, cs *CachedSchema, host *Host, reg *tool.Registry, sessionCtx context.Context, kick bool) []tool.Tool {
	// Resolve an existing exact project grant before constructing cached
	// placeholders. This is read-only host preparation; no MCP process or network
	// connection starts here.
	spec = ResolveStoredAuthorization(sessionCtx, spec)
	spawnCtx, cancel := context.WithCancel(sessionCtx)
	shared := &lazySpawn{
		spec: spec,
		host: host,
		reg:  reg,
		ctx:  spawnCtx,
	}
	shared.generation = host.registerDeferredCancel(spec.Name, cancel)

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
			out = append(out, &lazyTool{
				shared:      shared,
				name:        toolName(spec.Name, visibleName),
				rawName:     ct.Name,
				visibleName: visibleName,
				desc:        ct.Description,
				schema:      ct.Schema,
				readOnly:    ct.ReadOnly,
				destructive: ct.Destructive,
				hasCache:    true,
			})
		}
	}

	if kick {
		shared.kick()
	}
	return out
}
