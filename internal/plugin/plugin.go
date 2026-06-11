// Package plugin is Reasonix's MCP client. It connects to external MCP servers and
// adapts their tools to the tool.Tool interface, so the agent treats plugin
// tools and built-ins uniformly. The wire protocol is JSON-RPC 2.0 in every
// case; only the transport differs (stdio subprocess, Streamable HTTP, or the
// legacy HTTP+SSE). A transport interface hides that difference so the MCP-level
// logic — handshake, tools/list, tools/call — is written once.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

// protocolVersion is the MCP revision Reasonix advertises during initialize.
const protocolVersion = "2024-11-05"

// Spec declares an external MCP server. Type selects the transport: "stdio"
// (default) runs Command/Args/Env as a subprocess; "http" / "streamable-http"
// and "sse" connect to URL with optional static Headers.
type Spec struct {
	Name    string
	Type    string
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	Headers map[string]string
	// Dir, when set, is the working directory of a stdio subprocess. Empty means
	// inherit reasonix's cwd (the default for user-configured plugins). It exists
	// for cwd-aware servers like CodeGraph, which detect the project from the
	// directory they are launched in — they must be pinned to the project root.
	Dir string
	// Stderr optionally mirrors plugin subprocess stderr output. Stderr is always
	// captured in a bounded buffer for failure diagnostics; nil keeps it out of
	// the terminal so child logs cannot corrupt interactive UIs.
	Stderr io.Writer
	// ReadOnlyToolNames marks trusted raw MCP tool names as read-only even when
	// the server omits annotations.readOnlyHint. It is for first-party adapters
	// with known semantics; user-configured plugins should rely on MCP metadata.
	ReadOnlyToolNames map[string]bool
	// StripRawPrefix, when non-empty, removes this prefix from each MCP tool's
	// raw name before namespacing. For example, StripRawPrefix="codegraph_" turns
	// "codegraph_context" into "context", yielding "mcp__codegraph__context"
	// instead of the redundant "mcp__codegraph__codegraph_context". The original
	// raw name is preserved for MCP protocol calls.
	StripRawPrefix string
	// LowPriority runs a stdio subprocess below normal scheduling priority, for
	// background indexers (CodeGraph) that must not starve the user's machine.
	LowPriority bool
}

// transport carries JSON-RPC messages to and from one MCP server. call sends a
// request and returns its result (correlating by id internally); notify sends a
// fire-and-forget notification; close releases resources. Server-initiated
// messages (notifications, requests like roots/list) are ignored — Reasonix is a
// tools/prompts/resources consumer, not a sampling/roots provider (see SPEC §9).
type transport interface {
	call(ctx context.Context, method string, params any) (json.RawMessage, error)
	notify(ctx context.Context, method string, params any) error
	close()
}

// Host owns the running plugin connections and closes them together. It also
// aggregates the prompts and resources discovered across servers, which the
// chat UI surfaces (prompts as slash commands, resources as @-references).
type Host struct {
	// mu guards the slices below: StartAll builds the Host single-threaded, but
	// after that a /mcp hot-add or -remove (one goroutine) can run concurrently
	// with reads from a running turn's @ref resolution or the status UI.
	mu        sync.RWMutex
	clients   []*Client
	prompts   []Prompt
	resources []Resource
	failures  []Failure
	closed    bool

	// Lazy/background servers may still be handshaking when a session closes.
	// Close cancels those startup contexts and waits for their goroutines before
	// taking the client snapshot, so a just-connected stdio child cannot escape
	// teardown and keep a Windows workspace directory locked.
	deferredCancels []context.CancelFunc
	deferredWG      sync.WaitGroup

	// Detached stats/schema-cache writers from Start; off the boot path but
	// drained by Close so cleanup can't race a still-open cache file.
	bgWrites sync.WaitGroup
}

// Prompts returns every MCP prompt discovered across connected servers.
func (h *Host) Prompts() []Prompt {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]Prompt(nil), h.prompts...)
}

// Resources returns every MCP resource discovered across connected servers.
func (h *Host) Resources() []Resource {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]Resource(nil), h.resources...)
}

// ServerNames returns the connected servers' names, in connection order.
func (h *Host) ServerNames() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	names := make([]string, len(h.clients))
	for i, c := range h.clients {
		names[i] = c.name
	}
	return names
}

// ReadResource reads a resource uri from the named server. It is how the chat
// UI resolves an @server:uri reference — the uri need not be one listed by
// resources/list (servers may expose templated uris), so we read it directly.
func (h *Host) ReadResource(ctx context.Context, server, uri string) (string, error) {
	h.mu.RLock()
	var target *Client
	for _, c := range h.clients {
		if c.name == server {
			target = c
			break
		}
	}
	h.mu.RUnlock()
	if target == nil {
		return "", fmt.Errorf("no MCP server named %q", server)
	}
	return target.readResource(ctx, uri) // network call: outside the lock
}

// StartPolicy tunes batch plugin startup. The zero value disables every safeguard,
// so most call sites should use the StartAll / StartAvailable wrappers, which
// fill in production defaults.
type StartPolicy struct {
	// PerPluginTimeout caps how long a single plugin's handshake (start +
	// initialize + listTools + listPrompts/Resources) may take. Zero disables.
	// Exceeded plugins are recorded as failures and, when AbortOnError is set,
	// tear down the whole batch with the timeout as the cause.
	PerPluginTimeout time.Duration

	// Concurrency caps how many handshakes run at once. Zero or negative means
	// no cap (every plugin gets a goroutine immediately). A small cap prevents
	// process storms / FD exhaustion when many MCP servers are configured.
	Concurrency int

	// AbortOnError makes any single failure tear down the partial batch and
	// return an error (StartAll semantics). When false, failures are recorded
	// on the host and other plugins keep going (StartAvailable semantics).
	AbortOnError bool
}

// defaultStartConcurrency caps parallel handshakes for the batch-start wrappers.
// Eight is the standard "process storm" guardrail (Bazel's --jobs=auto, most LSP
// managers) — large enough to mask single-plugin latency, small enough to spare
// a workstation with 20+ configured MCP servers from fork-bombing itself.
const defaultStartConcurrency = 8

// defaultStartTimeout is the per-plugin budget used by StartAvailable. Five
// seconds covers a healthy stdio MCP spawning under a slow npm/node loader; past
// that, an interactive user is better served by recording the failure and moving
// on than by stalling the whole session.
const defaultStartTimeout = 5 * time.Second

// StartAll connects every plugin in parallel, performs the MCP handshake, and
// returns the union of their tools (namespaced "mcp__<server>__<tool>"). On any
// failure it tears down everything started so far. The caller must Close the Host.
//
// For stdio plugins, subprocess lifetime is bound to ctx (via
// exec.CommandContext): cancelling ctx kills the children and unblocks reads.
func StartAll(ctx context.Context, specs []Spec) (*Host, []tool.Tool, error) {
	return Start(ctx, specs, StartPolicy{
		Concurrency:  defaultStartConcurrency,
		AbortOnError: true,
	})
}

// StartAvailable connects every plugin it can and records failures on the host
// instead of aborting the whole session. The returned tools are the union of the
// successfully connected servers.
func StartAvailable(ctx context.Context, specs []Spec) (*Host, []tool.Tool) {
	h, tools, _ := Start(ctx, specs, StartPolicy{
		PerPluginTimeout: defaultStartTimeout,
		Concurrency:      defaultStartConcurrency,
		// AbortOnError stays false: a misconfigured plugin must not bring down
		// the whole session at boot.
	})
	return h, tools
}

// Start is the unified batch-startup primitive behind StartAll / StartAvailable.
// It fans out handshakes in parallel under the policy's concurrency cap, gives
// each plugin its own per-plugin timeout, and either aborts the batch on first
// failure (AbortOnError=true) or records failures on the host and keeps going.
//
// Result ordering matches specs (stable for /mcp status). For stdio plugins the
// subprocess is bound to the parent ctx, not the per-plugin startup timeout:
// successful servers stay alive after startup, while failed/time-limited starts
// are closed explicitly before the goroutine returns.
func Start(ctx context.Context, specs []Spec, p StartPolicy) (*Host, []tool.Tool, error) {
	if len(specs) == 0 {
		return &Host{}, nil, nil
	}

	type result struct {
		idx    int
		spec   Spec
		client *Client
		tools  []tool.Tool
		err    error
	}

	// A buffered channel acts as a counting semaphore. Capacity 0/negative
	// means no cap — we still launch one goroutine per spec, but they all run
	// immediately. Capped, the extra goroutines block on the semaphore until a
	// slot frees up; collection order is still by idx so /mcp status is stable.
	concurrency := p.Concurrency
	if concurrency <= 0 || concurrency > len(specs) {
		concurrency = len(specs)
	}
	sem := make(chan struct{}, concurrency)
	ch := make(chan result, len(specs))

	// Created before the fan-out so the detached cache writers can join bgWrites.
	h := &Host{}

	for i, s := range specs {
		go func(idx int, spec Spec) {
			sem <- struct{}{}
			defer func() { <-sem }()

			callCtx := ctx
			cancelStartup := func() {}
			if p.PerPluginTimeout > 0 {
				var cancel context.CancelFunc
				callCtx, cancel = context.WithTimeout(ctx, p.PerPluginTimeout)
				cancelStartup = cancel
			}

			phaseAStart := time.Now()
			recordedPhaseADur := func() time.Duration {
				dur := time.Since(phaseAStart)
				if p.PerPluginTimeout > 0 && callCtx.Err() == context.DeadlineExceeded && dur < p.PerPluginTimeout {
					return p.PerPluginTimeout
				}
				return dur
			}

			// Transport on the parent ctx, startup RPCs on the timed callCtx: the
			// per-plugin timeout caps initialize+listTools, but the long-lived
			// stdio child must outlive the startup scope and later phase-B calls.
			c, err := start(ctx, callCtx, spec)
			if err != nil {
				phaseADur := recordedPhaseADur()
				cancelStartup()
				h.bgWrites.Add(1)
				go func() { defer h.bgWrites.Done(); _ = RecordStartup(spec.Name, phaseADur) }()
				ch <- result{idx: idx, spec: spec, err: fmt.Errorf("start plugin %q: %w", spec.Name, err)}
				return
			}

			ts, err := c.listTools(callCtx)
			if err != nil {
				phaseADur := recordedPhaseADur()
				cancelStartup()
				h.bgWrites.Add(1)
				go func() { defer h.bgWrites.Done(); _ = RecordStartup(spec.Name, phaseADur) }()
				c.close()
				ch <- result{idx: idx, spec: spec, err: fmt.Errorf("list tools from %q: %w", spec.Name, err)}
				return
			}
			c.toolCount = len(ts)

			// Persist for next launch on the side: a slow stats/cache write
			// must not delay tools coming online, and either failure is
			// recoverable (we just re-handshake or skip auto-demote).
			phaseADur := recordedPhaseADur()
			cancelStartup()
			h.bgWrites.Add(1)
			go func() {
				defer h.bgWrites.Done()
				_ = RecordStartup(spec.Name, phaseADur)
				_ = SaveCachedSchema(spec.Name, CachedSchema{
					SpecHash: SpecFingerprint(spec),
					Capabilities: map[string]bool{
						"prompts":   c.hasPrompts,
						"resources": c.hasResources,
					},
					Tools: cacheableToolsOf(ts),
				})
			}()

			// Prompts and resources are deferred to StartPhaseB so the boot path
			// can return as soon as tools are ready — the slow-to-list surfaces
			// stream in later and fan out an MCPSurfaceReady event each.
			ch <- result{idx: idx, spec: spec, client: c, tools: ts}
		}(i, s)
	}

	// Wait for every goroutine even on abort: started clients sit beyond a
	// failing index, so we need them all back to tear them down in Close().
	results := make([]result, len(specs))
	for range specs {
		r := <-ch
		results[r.idx] = r
	}

	var tools []tool.Tool
	var firstErr error
	for _, r := range results {
		if r.err != nil {
			if p.AbortOnError {
				if firstErr == nil {
					firstErr = r.err
				}
			} else {
				h.RecordFailure(r.spec, r.err)
			}
			continue
		}
		h.clients = append(h.clients, r.client)
		tools = append(tools, r.tools...)
		// prompts/resources are filled in later by StartPhaseB.
	}
	if firstErr != nil {
		h.Close()
		return nil, nil, firstErr
	}
	return h, tools, nil
}

// Close terminates all plugin connections.
func (h *Host) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	cancels := append([]context.CancelFunc(nil), h.deferredCancels...)
	h.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	h.deferredWG.Wait()

	h.mu.RLock()
	clients := append([]*Client(nil), h.clients...) // snapshot; close outside the lock
	h.mu.RUnlock()
	for _, c := range clients {
		c.close()
	}
	h.bgWrites.Wait() // drain detached stats/schema writers before returning
}

// StartPhaseB asynchronously fetches the auxiliary surfaces (prompts and
// resources) for every connected client. Boot calls it right after Start
// returns, on a session-scoped ctx, so the agent becomes responsive as soon as
// tools are ready and the slower list calls stream in afterwards. Each finished
// surface fires an MCPSurfaceReady event on sink so UIs (e.g. /mcp status) can
// refresh without polling. A nil sink is tolerated — the merge still happens.
// Errors are logged and swallowed: prompts/resources are non-essential and must
// not break the session over one slow server.
func (h *Host) StartPhaseB(ctx context.Context, sink event.Sink) {
	h.mu.RLock()
	clients := append([]*Client(nil), h.clients...)
	h.mu.RUnlock()
	for _, c := range clients {
		if c.hasPrompts {
			go h.fetchPrompts(ctx, c, sink)
		}
		if c.hasResources {
			go h.fetchResources(ctx, c, sink)
		}
	}
}

func (h *Host) fetchPrompts(ctx context.Context, c *Client, sink event.Sink) {
	aux, auxCtx, cancel, err := c.auxiliaryClient(ctx)
	if err != nil {
		slog.Warn("plugin: start auxiliary prompt client failed", "server", c.name, "err", err)
		return
	}
	defer cancel()
	defer aux.close()

	ps, err := aux.listPrompts(auxCtx)
	if err != nil {
		slog.Warn("plugin: listPrompts failed", "server", c.name, "err", err)
		return
	}
	for i := range ps {
		ps[i].client = c
	}
	h.mu.Lock()
	c.prompts = ps
	h.prompts = append(h.prompts, ps...)
	h.mu.Unlock()
	if sink != nil {
		sink.Emit(event.Event{
			Kind: event.MCPSurfaceReady,
			Text: fmt.Sprintf("%s: prompts ready (%d items)", c.name, len(ps)),
		})
	}
}

func (h *Host) fetchResources(ctx context.Context, c *Client, sink event.Sink) {
	aux, auxCtx, cancel, err := c.auxiliaryClient(ctx)
	if err != nil {
		slog.Warn("plugin: start auxiliary resource client failed", "server", c.name, "err", err)
		return
	}
	defer cancel()
	defer aux.close()

	rs, err := aux.listResources(auxCtx)
	if err != nil {
		slog.Warn("plugin: listResources failed", "server", c.name, "err", err)
		return
	}
	h.mu.Lock()
	c.resources = rs
	h.resources = append(h.resources, rs...)
	h.mu.Unlock()
	if sink != nil {
		sink.Emit(event.Event{
			Kind: event.MCPSurfaceReady,
			Text: fmt.Sprintf("%s: resources ready (%d items)", c.name, len(rs)),
		})
	}
}

// Client is one MCP server connection: a name plus the transport carrying its
// JSON-RPC. The MCP-level methods (initialize, listTools, …) are transport-
// agnostic — they go through t.
type Client struct {
	name string
	t    transport
	spec Spec

	// Capabilities advertised by the server at initialize. prompts/list and
	// resources/list are only called when advertised, so we never provoke a
	// "method not found" on a tools-only server.
	hasPrompts   bool
	hasResources bool

	toolCount int    // tools discovered, for /mcp status
	transport string // declared transport type, for /mcp status ("stdio"/"http")

	// Prompts and resources discovered during StartAll, stored here so the
	// parallel startup can collect them per-client before merging into Host.
	prompts   []Prompt
	resources []Resource
	tools     []ToolInfo
}

func (c *Client) auxiliaryClient(ctx context.Context) (*Client, context.Context, context.CancelFunc, error) {
	auxCtx, cancel := context.WithTimeout(ctx, defaultStartTimeout)
	aux, err := start(auxCtx, auxCtx, c.spec)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return aux, auxCtx, cancel, nil
}

// ToolInfo is the human-facing metadata returned by MCP tools/list for one tool.
type ToolInfo struct {
	Name        string
	Description string
}

// ServerStatus summarises one connected server for the /mcp command.
type ServerStatus struct {
	Name      string
	Transport string
	Tools     int
	Prompts   int
	Resources int
	ToolList  []ToolInfo
}

// Failure records one MCP server that was configured but could not connect.
type Failure struct {
	Name      string
	Transport string
	Error     string
}

// Servers returns a status summary per connected server, in connection order.
func (h *Host) Servers() []ServerStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]ServerStatus, 0, len(h.clients))
	for _, c := range h.clients {
		s := ServerStatus{
			Name:      c.name,
			Transport: c.transport,
			Tools:     c.toolCount,
			ToolList:  append([]ToolInfo(nil), c.tools...),
		}
		for _, p := range h.prompts {
			if p.Server == c.name {
				s.Prompts++
			}
		}
		for _, r := range h.resources {
			if r.Server == c.name {
				s.Resources++
			}
		}
		out = append(out, s)
	}
	return out
}

// Failures returns configured MCP servers that failed to connect.
func (h *Host) Failures() []Failure {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]Failure, len(h.failures))
	copy(out, h.failures)
	return out
}

// RecordFailure stores a failed MCP connection attempt for status UIs.
func (h *Host) RecordFailure(s Spec, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tt := strings.ToLower(strings.TrimSpace(s.Type))
	if tt == "" {
		tt = "stdio"
	}
	f := Failure{Name: s.Name, Transport: tt, Error: summarizeFailureError(err)}
	for i := range h.failures {
		if h.failures[i].Name == s.Name {
			h.failures[i] = f
			return
		}
	}
	h.failures = append(h.failures, f)
}

// ClearFailure drops a recorded startup/connection failure for status UIs.
func (h *Host) ClearFailure(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clearFailure(name)
}

// clearFailure drops the failure record for name. The caller holds h.mu (Lock) —
// it runs inside addConnected / Remove, which already mutate under the lock.
func (h *Host) clearFailure(name string) {
	kept := h.failures[:0]
	for _, f := range h.failures {
		if f.Name != name {
			kept = append(kept, f)
		}
	}
	h.failures = kept
}

// NewHost returns an empty Host. Boot always constructs one — even with no
// plugins configured — so servers can be hot-added later via Add (the `/mcp add`
// command), which keeps the controller's host pointer stable for the session.
func NewHost() *Host { return &Host{} }

func (h *Host) registerDeferredCancel(cancel context.CancelFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		cancel()
		return
	}
	h.deferredCancels = append(h.deferredCancels, cancel)
}

func (h *Host) beginDeferredSpawn() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.deferredWG.Add(1)
	return true
}

func (h *Host) endDeferredSpawn() {
	h.deferredWG.Done()
}

// has reports whether a server with this name is already connected.
func (h *Host) has(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		if c.name == name {
			return true
		}
	}
	return false
}

// Add connects one server live: it performs the MCP handshake, discovers the
// server's tools (and prompts/resources when advertised), appends it to the
// host, and returns its namespaced tools for the caller to register. ctx bounds a
// stdio child's lifetime, so pass the session-scoped context — not a per-turn one
// — or the subprocess dies when that turn ends. Errors if the name is taken.
func (h *Host) Add(ctx context.Context, s Spec) ([]tool.Tool, error) {
	if h.has(s.Name) {
		return nil, fmt.Errorf("server %q is already connected", s.Name)
	}
	return h.addConnected(ctx, s)
}

func (h *Host) addConnected(ctx context.Context, s Spec) ([]tool.Tool, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, fmt.Errorf("plugin host is closed")
	}
	h.mu.RUnlock()

	c, err := start(ctx, ctx, s)
	if err != nil {
		return nil, err
	}
	ts, err := c.listTools(ctx)
	if err != nil {
		c.close()
		return nil, fmt.Errorf("list tools: %w", err)
	}
	c.toolCount = len(ts)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		c.close()
		return nil, fmt.Errorf("plugin host is closed")
	}
	h.clients = append(h.clients, c)
	h.clearFailure(s.Name)
	h.mu.Unlock()
	// Prompts and resources stream in on the long ctx the caller passed (Host.Add
	// uses the session-scoped PluginCtx, not a per-turn ctx), so the slow list
	// calls cannot starve a /mcp add of its return value. nil sink keeps hot-add
	// quiet — the chat UI re-queries Host.Prompts()/Resources() on demand.
	if c.hasPrompts {
		go h.fetchPrompts(ctx, c, nil)
	}
	if c.hasResources {
		go h.fetchResources(ctx, c, nil)
	}
	return ts, nil
}

// Remove disconnects the named server and drops its prompts/resources, returning
// the namespaced tool-name prefix ("mcp__<server>__") the caller unregisters from
// the tool registry, and whether the server was connected.
func (h *Host) Remove(name string) (toolPrefix string, found bool) {
	h.mu.Lock()
	idx := -1
	for i, c := range h.clients {
		if c.name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		h.mu.Unlock()
		return "", false
	}
	removed := h.clients[idx]
	h.clients = append(h.clients[:idx], h.clients[idx+1:]...)

	keptP := h.prompts[:0]
	for _, p := range h.prompts {
		if p.Server != name {
			keptP = append(keptP, p)
		}
	}
	h.prompts = keptP

	keptR := h.resources[:0]
	for _, r := range h.resources {
		if r.Server != name {
			keptR = append(keptR, r)
		}
	}
	h.resources = keptR
	h.clearFailure(name)
	h.mu.Unlock()

	removed.close() // kills the subprocess: outside the lock

	return "mcp__" + normalizeName(name) + "__", true
}

// start opens the transport on lifeCtx (whose cancellation later closes the
// subprocess) and uses callCtx for the initialize round-trip (whose cancellation
// only bounds startup RPCs). Splitting the two lets a per-plugin timeout cap
// handshake latency without making the timeout context own a successfully
// registered stdio server; the child also has to outlive phase A so phase B
// (prompts + resources) can still call it later. Callers that don't care pass
// the same ctx for both.
func start(lifeCtx, callCtx context.Context, s Spec) (*Client, error) {
	t, err := newTransport(lifeCtx, s)
	if err != nil {
		return nil, err
	}
	tt := strings.ToLower(strings.TrimSpace(s.Type))
	if tt == "" {
		tt = "stdio"
	}
	c := &Client{name: s.Name, t: t, spec: s, transport: tt}
	if err := c.initialize(callCtx); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

// newTransport builds the transport for a spec's declared type. Empty / unknown
// defaults to stdio.
func newTransport(ctx context.Context, s Spec) (transport, error) {
	switch strings.ToLower(strings.TrimSpace(s.Type)) {
	case "", "stdio":
		return newStdioTransport(ctx, s)
	case "http", "streamable-http", "streamable_http":
		return newHTTPTransport(s)
	case "sse":
		// The legacy 2024-11-05 HTTP+SSE transport needs a persistent GET stream
		// with a background dispatcher — deprecated upstream ("avoid for new
		// work"). Use type="http" (Streamable HTTP), which most remote servers
		// now speak. Tracked for later (SPEC §9).
		return nil, fmt.Errorf("plugin %q: legacy sse transport not yet supported — use type=\"http\" (Streamable HTTP)", s.Name)
	default:
		return nil, fmt.Errorf("unknown transport type %q (want stdio|http|sse)", s.Type)
	}
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.t.call(ctx, method, params)
}

func (c *Client) notify(ctx context.Context, method string, params any) error {
	return c.t.notify(ctx, method, params)
}

func (c *Client) close() { c.t.close() }

func (c *Client) initialize(ctx context.Context) error {
	res, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "reasonix", "version": "dev"},
	})
	if err != nil {
		return err
	}
	// Record which optional capabilities the server advertises. Presence of the
	// key (even with an empty object) signals support.
	var ir struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(res, &ir); err != nil {
		slog.Warn("plugin: parse initialize capabilities", "server", c.name, "err", err)
	}
	_, c.hasPrompts = ir.Capabilities["prompts"]
	_, c.hasResources = ir.Capabilities["resources"]

	return c.notify(ctx, "notifications/initialized", map[string]any{})
}

type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	// Annotations carries MCP's optional tool hints. We read readOnlyHint: a
	// plugin that declares a tool read-only opts it into Reasonix's parallel-dispatch
	// path and the permission layer's "readers default to allow". Absent
	// annotations stay false — opaque by default, never trusted implicitly.
	Annotations *struct {
		ReadOnlyHint bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

func (s Spec) toolReadOnly(rawName string, hinted bool) bool {
	return hinted || s.ReadOnlyToolNames[rawName]
}

func (c *Client) listTools(ctx context.Context) ([]tool.Tool, error) {
	res, err := c.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []mcpTool `json:"tools"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("plugin %q: decode tools/list: %w", c.name, err)
	}

	toolInfos := make([]ToolInfo, 0, len(out.Tools))
	tools := make([]tool.Tool, 0, len(out.Tools))
	for _, t := range out.Tools {
		hinted := t.Annotations != nil && t.Annotations.ReadOnlyHint
		visibleName := t.Name
		if c.spec.StripRawPrefix != "" {
			visibleName = strings.TrimPrefix(visibleName, c.spec.StripRawPrefix)
		}
		toolInfos = append(toolInfos, ToolInfo{Name: t.Name, Description: t.Description})
		tools = append(tools, &remoteTool{
			client:   c,
			name:     toolName(c.name, visibleName),
			rawName:  t.Name,
			desc:     t.Description,
			schema:   canonicalizeSchema(t.InputSchema),
			readOnly: c.spec.toolReadOnly(t.Name, hinted),
		})
	}
	sort.SliceStable(toolInfos, func(i, j int) bool { return toolInfos[i].Name < toolInfos[j].Name })
	c.tools = toolInfos
	return sortToolsByName(tools), nil
}

// toolName builds the model-visible namespaced name "mcp__<server>__<tool>",
// matching Claude Code. Spaces in either part are normalised to underscores so
// the name is a clean identifier the model can call.
func toolName(server, raw string) string {
	return ToolPrefix(server) + normalizeName(raw)
}

// ToolPrefix is the model-visible namespace prefix for every tool from server.
func ToolPrefix(server string) string {
	return "mcp__" + normalizeName(server) + "__"
}

var invalidNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func normalizeName(s string) string {
	raw := s
	s = strings.Trim(invalidNameChars.ReplaceAllString(s, "_"), "_")
	if s == "" {
		s = "unnamed"
	}
	if s != raw {
		s += "_" + shortNameHash(raw)
	}
	return s
}

func shortNameHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())[:6]
}

func summarizeFailureError(err error) string {
	msg := strings.Join(strings.Fields(err.Error()), " ")
	const max = 500
	if len(msg) > max {
		msg = msg[:max] + "..."
	}
	return msg
}

// --- JSON-RPC message types (shared by every transport) ---

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"` // omitted for notifications (id 0 unused)
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// --- remote tool adapter ---

type remoteTool struct {
	client   *Client
	name     string // namespaced "mcp__<server>__<tool>"
	rawName  string // original name for tools/call
	desc     string
	schema   json.RawMessage
	readOnly bool // from MCP readOnlyHint or trusted first-party Spec override
}

func (t *remoteTool) Name() string        { return t.name }
func (t *remoteTool) Description() string { return t.desc }

// ReadOnly reflects MCP readOnlyHint, plus trusted first-party Spec overrides.
// It defaults to false: opaque third-party tools must declare readOnlyHint
// before joining reader-default permission handling or plan-mode execution.
func (t *remoteTool) ReadOnly() bool { return t.readOnly }

func (t *remoteTool) Schema() json.RawMessage {
	if len(t.schema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return canonicalizeSchema(t.schema)
}

func (t *remoteTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var argMap map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argMap); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
	}
	res, err := t.client.call(ctx, "tools/call", map[string]any{
		"name":      t.rawName,
		"arguments": argMap,
	})
	if err != nil {
		return "", err
	}
	return parseToolResult(res)
}

// parseToolResult flattens an MCP tools/call result into plain text.
func parseToolResult(res json.RawMessage) (string, error) {
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", fmt.Errorf("decode tool result: %w", err)
	}
	var sb strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	text := sb.String()
	if out.IsError {
		return text, fmt.Errorf("plugin tool reported error: %s", text)
	}
	return text, nil
}
