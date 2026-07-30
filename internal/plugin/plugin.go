// Package plugin is Reasonix's MCP client. It connects to external MCP servers and
// adapts their tools to the tool.Tool interface, so the agent treats plugin
// tools and built-ins uniformly. The wire protocol is JSON-RPC 2.0 in every
// case; only the transport differs (stdio subprocess, Streamable HTTP, or the
// legacy HTTP+SSE). A transport interface hides that difference so the MCP-level
// logic — handshake, tools/list, tools/call — is written once.
package plugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/mcplaunch"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

// protocolVersion is the MCP revision Reasonix advertises during initialize.
const protocolVersion = "2024-11-05"

// MCPProcessMode selects how a local stdio MCP process is launched.
// It is an internal runtime field, not a user-facing config knob.
type MCPProcessMode string

const (
	// MCPProcessHost runs authorized stdio MCP as a trusted host process that
	// does not inherit the agent Bash command sandbox. This is the product
	// default so servers such as chrome-devtools-mcp can reach the real browser,
	// Keychain, LaunchServices, and local app services.
	MCPProcessHost MCPProcessMode = "host"
	// MCPProcessConfined wraps the process with sandbox.CommandArgs. Reserved for
	// internal managed deployments and tests; never auto-selected for user installs.
	MCPProcessConfined MCPProcessMode = "confined"
)

// ResolvedProcessMode returns the effective process mode. Empty means host.
func (s Spec) ResolvedProcessMode() MCPProcessMode {
	switch s.ProcessMode {
	case MCPProcessConfined:
		return MCPProcessConfined
	default:
		return MCPProcessHost
	}
}

// defaultCallTimeout is the MCP JSON-RPC call deadline applied when neither the
// caller context nor config provides one. It is intentionally finite so a slow
// or hung MCP server cannot block an agent turn indefinitely.
const defaultCallTimeout = 300 * time.Second

// Spec declares an external MCP server. Type selects the transport: "stdio"
// (default) runs Command/Args/Env as a subprocess; "http" / "streamable-http"
// and "sse" connect to URL with optional static Headers.
type Spec struct {
	Name string
	// Package is the installed plugin package that contributed this server.
	// It is host-only provenance and intentionally excluded from fingerprints.
	Package string
	Type    string
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	Headers map[string]string
	// DefaultCallTimeout is the global MCP call cap for this server. Zero keeps
	// Reasonix's built-in defaultCallTimeout.
	DefaultCallTimeout time.Duration
	// CallTimeout overrides DefaultCallTimeout for all calls to this server.
	// Zero falls back to DefaultCallTimeout.
	CallTimeout time.Duration
	// ToolTimeouts overrides the per-call deadline for raw MCP tool names.
	// Keys are server-local tool names as returned by tools/list, not the
	// model-visible mcp__server__tool names.
	ToolTimeouts map[string]time.Duration
	// Dir, when set, is the working directory of a stdio subprocess. Empty means
	// inherit reasonix's cwd (the default for user-configured plugins). It exists
	// for cwd-aware servers like CodeGraph, which detect the project from the
	// directory they are launched in — they must be pinned to the project root.
	Dir string
	// WorkspaceRoot is the project root exposed through the MCP roots capability.
	// It is runtime-only and intentionally separate from Dir: user-installed
	// stdio servers keep inheriting Reasonix's cwd while still receiving the
	// explicit workspace root when they ask for roots/list.
	WorkspaceRoot string
	// Stderr optionally mirrors plugin subprocess stderr output. Stderr is always
	// captured in a bounded buffer for failure diagnostics; nil keeps it out of
	// the terminal so child logs cannot corrupt interactive UIs.
	Stderr io.Writer
	// LaunchManager owns exact project launch grants and mutable launcher locks.
	// It never contributes to SchemaCacheKey or provider-visible tool schemas.
	LaunchManager *mcplaunch.Manager
	// ConfigSource disambiguates otherwise identical server names coming from
	// workspace config, a host transport, or a user-installed plugin package.
	ConfigSource string
	// Authorized is the single runtime authorization result for this server.
	// User-installed and explicit host-session servers set it directly; project
	// servers set it only after an exact launch grant is resolved.
	Authorized            bool
	RequireLaunchApproval bool
	// LaunchArgs and launcher metadata are host-local immutable resolutions for
	// mutable package launchers. LauncherIdentityArgs is the same exact package
	// resolution without an automatically injected offline/no-install flag: that
	// enforcement-only flag changes process invocation but not the server identity
	// the user approved. These fields never contribute to SchemaCacheKey or the
	// provider-visible tool surface; Args remains the user's stable config.
	LaunchArgs              []string
	LauncherIdentityArgs    []string
	LauncherLocator         string
	LauncherResolvedVersion string
	LauncherDigest          string
	// ProcessMode selects how an authorized stdio MCP process is launched.
	// Empty defaults to host (trusted host process, no command sandbox).
	// confined is reserved for internal managed deployments and tests; it is
	// never exposed in common settings and never used as an automatic fallback.
	ProcessMode MCPProcessMode
	// Sandbox is only applied when ProcessMode is confined. Host-mode servers
	// keep private state/cache/temp dirs without wrapping the process in the
	// agent command sandbox.
	Sandbox  sandbox.Spec
	StateDir string
	// StripRawPrefix, when non-empty, removes this prefix from each MCP tool's
	// raw name before namespacing. For example, StripRawPrefix="server_" turns
	// "server_search" into "search", yielding "mcp__search__search" instead of
	// the redundant "mcp__search__server_search". The original raw name is
	// preserved for MCP protocol calls.
	StripRawPrefix string
	// LowPriority runs a stdio subprocess below normal scheduling priority, for
	// background indexers that must not starve the user's machine.
	LowPriority bool
}

// transport carries JSON-RPC messages to and from one MCP server. call sends a
// request and returns its result (correlating by id internally); notify sends a
// fire-and-forget notification; close releases resources. Transports route MCP
// progress notifications to the active tool call and answer the client
// capabilities Reasonix advertises (currently ping and roots/list).
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
	deferredCancels     map[string][]context.CancelFunc
	deferredGenerations map[string]uint64
	deferredWG          sync.WaitGroup

	// spawningMu + spawning prevent concurrent spawns of the same server from
	// multiple callers (e.g. several controller tabs sharing one Host). The
	// owner publishes its result before closing done so waiters can reuse the
	// discovered tools without issuing concurrent tools/list calls.
	spawningMu sync.Mutex
	spawning   map[string]*spawnAttempt

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

	// SkipPersistence disables RecordStartup / SaveCachedSchema side effects.
	// Use for read-only live probes (capability diagnostics) that must not
	// write MCP stats or schema cache files under Reasonix home.
	SkipPersistence bool
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

var advertisedToolsEmptyListRetryDelays = []time.Duration{
	50 * time.Millisecond,
	150 * time.Millisecond,
	300 * time.Millisecond,
}

// ErrServerAlreadyConnected marks an attempted MCP connection whose server name
// is already live on the host.
var ErrServerAlreadyConnected = errors.New("plugin server already connected")

func serverAlreadyConnectedError(name string) error {
	return fmt.Errorf("%w: %q", ErrServerAlreadyConnected, name)
}

// IsServerAlreadyConnected reports whether err means the MCP server name is
// already live on the host.
func IsServerAlreadyConnected(err error) bool {
	return errors.Is(err, ErrServerAlreadyConnected)
}

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
				if !p.SkipPersistence {
					h.bgWrites.Add(1)
					go func() { defer h.bgWrites.Done(); _ = RecordStartup(spec.Name, phaseADur) }()
				}
				ch <- result{idx: idx, spec: spec, err: fmt.Errorf("start plugin %q: %w", spec.Name, err)}
				return
			}

			ts, err := c.listTools(callCtx)
			if err != nil {
				phaseADur := recordedPhaseADur()
				cancelStartup()
				if !p.SkipPersistence {
					h.bgWrites.Add(1)
					go func() { defer h.bgWrites.Done(); _ = RecordStartup(spec.Name, phaseADur) }()
				}
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
			if !p.SkipPersistence {
				h.bgWrites.Add(1)
				go func() {
					defer h.bgWrites.Done()
					_ = RecordStartup(spec.Name, phaseADur)
					_ = SaveCachedSchema(spec.Name, CachedSchema{
						CacheKey: SchemaCacheKey(spec),
						Capabilities: map[string]bool{
							"tools":     c.hasTools,
							"prompts":   c.hasPrompts,
							"resources": c.hasResources,
						},
						Tools: cacheableToolsOf(ts),
					})
				}()
			}

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
	var cancels []context.CancelFunc
	for _, serverCancels := range h.deferredCancels {
		cancels = append(cancels, serverCancels...)
	}
	h.deferredCancels = nil
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

// queueBackgroundWrite keeps detached persistence inside the Host lifecycle.
// Callers must enqueue before their Close-drained startup owner completes, so
// Close cannot begin waiting before the WaitGroup increment is visible.
func (h *Host) queueBackgroundWrite(write func()) {
	h.bgWrites.Add(1)
	go func() {
		defer h.bgWrites.Done()
		write()
	}()
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
	hasTools     bool
	hasPrompts   bool
	hasResources bool

	toolCount int    // tools discovered, for /mcp status
	transport string // declared transport type, for /mcp status ("stdio"/"http")

	// Prompts and resources discovered during StartAll, stored here so the
	// parallel startup can collect them per-client before merging into Host.
	prompts   []Prompt
	resources []Resource
	toolsMu   sync.Mutex
	tools     []ToolInfo

	// toolAdapters caches the model-visible remote tool adapters produced by
	// the first successful tools/list call. Shared hosts reuse Client instances
	// across controllers, so subsequent ToolsFor calls must not re-query slow
	// MCP servers just to rebuild identical schemas.
	toolsListed  bool
	toolAdapters []tool.Tool
	progressID   atomic.Uint64
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
	Name            string
	Description     string
	ReadOnlyHint    bool
	DestructiveHint bool
	SchemaError     string
}

// ServerStatus summarises one connected server for the /mcp command.
type ServerStatus struct {
	Name      string
	Transport string
	Tools     int
	Prompts   int
	Resources int
	HasTools  bool
	ToolList  []ToolInfo
}

// AuthorizeSpecLaunch records durable consent for an explicitly user-installed
// project MCP without starting it a second time. The normal project discovery
// path still requires a user action; install_source calls this only while
// applying a plan the user already requested. Reuse an existing launcher lock
// when one exists, but do not add a second network/version-resolution step to an
// explicit install: the durable grant follows the exact configured command or
// endpoint and future changes still invalidate it.
func AuthorizeSpecLaunch(ctx context.Context, spec Spec) error {
	return authorizeSpecLaunch(ctx, spec, false)
}

// AuthorizeProjectSpecLaunch records the one durable launch confirmation used
// for repository-discovered MCP configuration. Mutable package launchers are
// resolved and locked, but the MCP server itself is not started: the caller can
// connect it exactly once after this function returns.
func AuthorizeProjectSpecLaunch(ctx context.Context, spec Spec) error {
	return authorizeSpecLaunch(ctx, spec, true)
}

func authorizeSpecLaunch(ctx context.Context, spec Spec, lockMutableLauncher bool) error {
	if !spec.RequireLaunchApproval {
		return nil
	}
	manager := spec.LaunchManager
	if manager == nil {
		return fmt.Errorf("MCP launch authorization store is unavailable")
	}
	var prepared Spec
	var launcherLock *mcplaunch.LauncherLock
	var err error
	if lockMutableLauncher {
		prepared, launcherLock, err = preparePersistentLauncher(ctx, spec)
	} else {
		prepared, err = applyStoredLauncherLock(spec)
	}
	if err != nil {
		return err
	}
	identityDigest, err := projectLaunchIdentityDigest(ctx, prepared)
	if err != nil {
		return err
	}
	if launcherLock != nil {
		// Store the resolution before the grant so a failed state write cannot
		// leave an authorization whose exact launcher identity is unavailable.
		if err := manager.PutLauncherLock(*launcherLock); err != nil {
			return err
		}
	}
	return manager.Authorize(prepared.Name, launchConfigSource(prepared), identityDigest)
}

// Failure records one MCP server that was configured but could not connect.
type Failure struct {
	Name                   string
	Transport              string
	Error                  string
	RequiresLaunchApproval bool
}

type launchApprovalError struct {
	server  string
	changed bool
}

func (e *launchApprovalError) Error() string {
	if e.changed {
		return fmt.Sprintf("project-provided MCP server %q changed; blocked before process or network startup and requires explicit re-authorization", e.server)
	}
	return fmt.Sprintf("project-provided MCP server %q is blocked before process or network startup until the user authorizes it", e.server)
}

func requiresLaunchApproval(err error) bool {
	var launchTarget *launchApprovalError
	return errors.As(err, &launchTarget)
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
			HasTools:  c.hasTools,
		}
		c.toolsMu.Lock()
		s.ToolList = append([]ToolInfo(nil), c.tools...)
		c.toolsMu.Unlock()
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

// ConnectingServers returns server names whose startup handshake is currently in
// flight. It is intentionally status-only: connected clients and failures remain
// the source of truth for ready/issue states.
func (h *Host) ConnectingServers() []string {
	h.spawningMu.Lock()
	defer h.spawningMu.Unlock()
	out := make([]string, 0, len(h.spawning))
	for name := range h.spawning {
		out = append(out, name)
	}
	sort.Strings(out)
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
	f := Failure{
		Name: s.Name, Transport: tt, Error: summarizeFailureError(err),
		RequiresLaunchApproval: requiresLaunchApproval(err),
	}
	for i := range h.failures {
		if h.failures[i].Name == s.Name {
			h.failures[i] = f
			return
		}
	}
	h.failures = append(h.failures, f)
}

// RecordLaunchApprovalRequired keeps an intentionally disconnected project MCP
// visible as awaiting authorization. This is used after an explicit launch
// revocation, where no failed connection attempt exists to create the status.
func (h *Host) RecordLaunchApprovalRequired(s Spec) {
	h.RecordFailure(s, &launchApprovalError{server: s.Name})
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

func (h *Host) registerDeferredCancel(name string, cancel context.CancelFunc) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		cancel()
		return 0
	}
	if h.deferredCancels == nil {
		h.deferredCancels = make(map[string][]context.CancelFunc)
	}
	if h.deferredGenerations == nil {
		h.deferredGenerations = make(map[string]uint64)
	}
	generation := h.deferredGenerations[name]
	if generation == 0 {
		generation = 1
		h.deferredGenerations[name] = generation
	}
	h.deferredCancels[name] = append(h.deferredCancels[name], cancel)
	return generation
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

// ErrSpawningInFlight is returned by Host.Add when another caller is already
// spawning the same server on this host. The caller should retry later.
var ErrSpawningInFlight = errors.New("server spawn already in progress")

type spawnAttempt struct {
	done  chan struct{}
	tools []tool.Tool
	err   error
}

// beginSpawn atomically claims the sole right to spawn the named server.
// Returns owner=true if the caller should proceed. When another caller is
// already spawning the same server, owner=false and done is closed when that
// spawn finishes.
func (h *Host) beginSpawn(name string) (*spawnAttempt, bool) {
	h.spawningMu.Lock()
	defer h.spawningMu.Unlock()
	if h.spawning == nil {
		h.spawning = make(map[string]*spawnAttempt)
	}
	if attempt, ok := h.spawning[name]; ok {
		return attempt, false
	}
	attempt := &spawnAttempt{done: make(chan struct{})}
	h.spawning[name] = attempt
	return attempt, true
}

// endSpawn releases the spawn claim for the named server.
func (h *Host) endSpawn(name string, tools []tool.Tool, err error) {
	h.spawningMu.Lock()
	if attempt, ok := h.spawning[name]; ok {
		attempt.tools = append([]tool.Tool(nil), tools...)
		attempt.err = err
		delete(h.spawning, name)
		close(attempt.done)
	}
	h.spawningMu.Unlock()
}

// has reports whether a server with this name is already connected.
func (h *Host) has(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.hasLocked(name)
}

func (h *Host) hasLocked(name string) bool {
	for _, c := range h.clients {
		if c.name == name {
			return true
		}
	}
	return false
}

// HasClient reports whether a server with this name is already connected to the host.
func (h *Host) HasClient(name string) bool { return h.has(name) }

// HasClientForSpec reports whether the shared Host client for spec.Name was
// created from the same runtime connection identity. Server names are only a
// display/routing namespace; they are not sufficient authorization identity
// when controllers with different project configs share one Host.
func (h *Host) HasClientForSpec(spec Spec) bool {
	c := h.client(spec.Name)
	return c != nil && MCPRuntimeSpecMatches(c.spec, spec)
}

// ToolsFor returns the namespaced tool instances for an already-connected client.
// ctx bounds the tools/list call so a non-responsive server does not hang
// permanently. An error is returned when no client with that name is connected.
func (h *Host) ToolsFor(ctx context.Context, name string) ([]tool.Tool, error) {
	h.mu.RLock()
	closed := h.closed
	h.mu.RUnlock()
	if closed {
		return nil, fmt.Errorf("plugin host is closed")
	}

	// Attempt to resolve via the existing Client.
	c := h.client(name)
	if c == nil {
		return nil, fmt.Errorf("client %q not found on shared host", name)
	}
	if tools, ok := c.cachedTools(); ok {
		return tools, nil
	}
	return c.listTools(ctx)
}

// ToolsForSpec is the identity-bound variant used by stable capability
// frontends. It refuses a same-name client from another controller, project
// identity, endpoint, or prior hot-update generation instead of treating that
// client as the current runtime's authorized server.
func (h *Host) ToolsForSpec(ctx context.Context, spec Spec) ([]tool.Tool, error) {
	h.mu.RLock()
	closed := h.closed
	h.mu.RUnlock()
	if closed {
		return nil, fmt.Errorf("plugin host is closed")
	}
	c := h.client(spec.Name)
	if c == nil {
		return nil, fmt.Errorf("client %q not found on shared host", spec.Name)
	}
	if !MCPRuntimeSpecMatches(c.spec, spec) {
		return nil, fmt.Errorf("connected MCP server %q identity does not match the current runtime configuration", spec.Name)
	}
	if tools, ok := c.cachedTools(); ok {
		return tools, nil
	}
	return c.listTools(ctx)
}

// MCPRuntimeSpecMatches compares the complete host-local runtime behavior of
// two specs while deliberately excluding non-behavioral handles such as the
// stderr writer and LaunchManager pointer. Secret values are compared only in
// memory and are never serialized into diagnostics or provider-visible state.
func MCPRuntimeSpecMatches(a, b Spec) bool {
	return reflect.DeepEqual(mcpRuntimeSpecIdentityOf(a), mcpRuntimeSpecIdentityOf(b))
}

// MCPToolMatchesSpec reports whether a concrete plugin adapter or pinned lazy
// placeholder belongs to the requested runtime spec. Unknown tool
// implementations fail closed when a runtime-bound capability frontend asks.
func MCPToolMatchesSpec(t tool.Tool, spec Spec) bool {
	switch typed := t.(type) {
	case *remoteTool:
		return typed != nil && typed.client != nil && MCPRuntimeSpecMatches(typed.client.spec, spec)
	case *lazyTool:
		return typed != nil && typed.shared != nil && MCPRuntimeSpecMatches(typed.shared.spec, spec)
	default:
		return false
	}
}

type mcpRuntimeSpecIdentity struct {
	Name                    string
	Package                 string
	Type                    string
	Command                 string
	Args                    []string
	Env                     map[string]string
	URL                     string
	Headers                 map[string]string
	DefaultCallTimeout      time.Duration
	CallTimeout             time.Duration
	ToolTimeouts            map[string]time.Duration
	Dir                     string
	WorkspaceRoot           string
	LaunchWorkspace         string
	ConfigSource            string
	RequireLaunchApproval   bool
	LaunchArgs              []string
	LauncherIdentityArgs    []string
	LauncherLocator         string
	LauncherResolvedVersion string
	LauncherDigest          string
	ProcessMode             MCPProcessMode
	Sandbox                 sandbox.Spec
	StateDir                string
	StripRawPrefix          string
	LowPriority             bool
}

func mcpRuntimeSpecIdentityOf(s Spec) mcpRuntimeSpecIdentity {
	launchWorkspace := ""
	if s.LaunchManager != nil {
		launchWorkspace = s.LaunchManager.WorkspaceFingerprint()
	}
	return mcpRuntimeSpecIdentity{
		Name:                    strings.TrimSpace(s.Name),
		Package:                 strings.TrimSpace(s.Package),
		Type:                    canonicalMCPRuntimeTransport(s.Type),
		Command:                 s.Command,
		Args:                    nonEmptyStrings(s.Args),
		Env:                     nonEmptyStringMap(s.Env),
		URL:                     s.URL,
		Headers:                 nonEmptyStringMap(s.Headers),
		DefaultCallTimeout:      s.DefaultCallTimeout,
		CallTimeout:             s.CallTimeout,
		ToolTimeouts:            nonEmptyDurationMap(s.ToolTimeouts),
		Dir:                     s.Dir,
		WorkspaceRoot:           s.WorkspaceRoot,
		LaunchWorkspace:         launchWorkspace,
		ConfigSource:            strings.TrimSpace(s.ConfigSource),
		RequireLaunchApproval:   s.RequireLaunchApproval,
		LaunchArgs:              nonEmptyStrings(s.LaunchArgs),
		LauncherIdentityArgs:    nonEmptyStrings(s.LauncherIdentityArgs),
		LauncherLocator:         s.LauncherLocator,
		LauncherResolvedVersion: s.LauncherResolvedVersion,
		LauncherDigest:          s.LauncherDigest,
		ProcessMode:             s.ResolvedProcessMode(),
		Sandbox:                 canonicalMCPRuntimeSandbox(s.Sandbox),
		StateDir:                s.StateDir,
		StripRawPrefix:          s.StripRawPrefix,
		LowPriority:             s.LowPriority,
	}
}

func canonicalMCPRuntimeTransport(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "stdio":
		return "stdio"
	case "http", "streamable-http", "streamable_http":
		return "streamable-http"
	case "sse":
		return "sse"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func canonicalMCPRuntimeSandbox(in sandbox.Spec) sandbox.Spec {
	in.WriteRoots = nonEmptyStrings(in.WriteRoots)
	in.ReadRoots = nonEmptyStrings(in.ReadRoots)
	in.AppContainerWriteRoots = nonEmptyStrings(in.AppContainerWriteRoots)
	in.ForbidReadRoots = nonEmptyStrings(in.ForbidReadRoots)
	return in
}

func nonEmptyStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return in
}

func nonEmptyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	return in
}

func nonEmptyDurationMap(in map[string]time.Duration) map[string]time.Duration {
	if len(in) == 0 {
		return nil
	}
	return in
}

// client returns the named connected client, or nil.
func (h *Host) client(name string) *Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		if c.name == name {
			return c
		}
	}
	return nil
}

// Add connects one server live: it performs the MCP handshake, discovers the
// server's tools (and prompts/resources when advertised), appends it to the
// host, and returns its namespaced tools for the caller to register. ctx bounds a
// stdio child's lifetime, so pass the session-scoped context — not a per-turn one
// — or the subprocess dies when that turn ends. Errors if the name is taken.
func (h *Host) Add(ctx context.Context, s Spec) ([]tool.Tool, error) {
	return h.addWithLifecycle(ctx, ctx, s, 0)
}

// EnsureConnected returns tools for an already-connected server, or starts the
// shared single-flight handshake and waits for it. Concurrent callers for the
// same server share one initialize/tools-list; cancelling a waiter only cancels
// that wait and never kills a process still used by other runtimes.
func (h *Host) EnsureConnected(ctx context.Context, s Spec) ([]tool.Tool, error) {
	return h.EnsureConnectedWithLifecycle(ctx, ctx, s, 0)
}

// EnsureConnectedWithLifecycle is EnsureConnected with separate subprocess
// lifetime (lifeCtx) and startup/call (callCtx) contexts, plus an optional
// deferred generation for lazy registration.
func (h *Host) EnsureConnectedWithLifecycle(lifeCtx, callCtx context.Context, s Spec, deferredGeneration uint64) ([]tool.Tool, error) {
	if deferredGeneration != 0 && !h.deferredGenerationCurrent(s.Name, deferredGeneration) {
		return nil, ErrDeferredSpawnCancelled
	}
	if tools, err := h.ToolsFor(callCtx, s.Name); err == nil {
		return tools, nil
	}
	tools, err := h.addWithLifecycle(lifeCtx, callCtx, s, deferredGeneration)
	if IsServerAlreadyConnected(err) {
		return h.ToolsFor(callCtx, s.Name)
	}
	return tools, err
}

// AddWithLifecycle connects one server live, allowing caller to specify separate
// contexts for the subprocess lifecycle (lifeCtx, session-scoped) and the startup
// handshake/list calls (callCtx, turn-scoped/timeout-bound).
func (h *Host) AddWithLifecycle(lifeCtx, callCtx context.Context, s Spec) ([]tool.Tool, error) {
	return h.addWithLifecycle(lifeCtx, callCtx, s, 0)
}

func (h *Host) addWithLifecycle(lifeCtx, callCtx context.Context, s Spec, deferredGeneration uint64) ([]tool.Tool, error) {
	if deferredGeneration != 0 && !h.deferredGenerationCurrent(s.Name, deferredGeneration) {
		return nil, ErrDeferredSpawnCancelled
	}
	if h.has(s.Name) {
		return nil, serverAlreadyConnectedError(s.Name)
	}
	spawnKey := s.Name
	if deferredGeneration != 0 {
		spawnKey = fmt.Sprintf("%s#%d", s.Name, deferredGeneration)
	}
	attempt, owner := h.beginSpawn(spawnKey)
	if !owner {
		select {
		case <-attempt.done:
			if attempt.err != nil {
				return nil, attempt.err
			}
			return append([]tool.Tool(nil), attempt.tools...), nil
		case <-callCtx.Done():
			return nil, callCtx.Err()
		case <-lifeCtx.Done():
			return nil, lifeCtx.Err()
		}
	}
	var tools []tool.Tool
	var err error
	defer func() { h.endSpawn(spawnKey, tools, err) }()
	// Double-check after acquiring the spawn token: another caller may have
	// connected the server between our h.has check and beginSpawn.
	if h.has(s.Name) {
		err = serverAlreadyConnectedError(s.Name)
		return nil, err
	}
	tools, err = h.addConnectedWithLifecycle(lifeCtx, callCtx, s, deferredGeneration)
	return tools, err
}

func (h *Host) addConnected(ctx context.Context, s Spec) ([]tool.Tool, error) {
	return h.addConnectedWithLifecycle(ctx, ctx, s, 0)
}

func (h *Host) addConnectedWithLifecycle(lifeCtx, callCtx context.Context, s Spec, deferredGeneration uint64) ([]tool.Tool, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, fmt.Errorf("plugin host is closed")
	}
	h.mu.RUnlock()

	c, err := start(lifeCtx, callCtx, s)
	if err != nil {
		return nil, err
	}
	ts, err := c.listTools(callCtx)
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
	if deferredGeneration != 0 && h.deferredGenerations[s.Name] != deferredGeneration {
		h.mu.Unlock()
		c.close()
		return nil, ErrDeferredSpawnCancelled
	}
	if h.hasLocked(s.Name) {
		h.mu.Unlock()
		c.close()
		return nil, serverAlreadyConnectedError(s.Name)
	}
	h.clients = append(h.clients, c)
	h.clearFailure(s.Name)
	h.mu.Unlock()
	// Prompts and resources stream in on the long lifeCtx the caller passed (Host.Add
	// uses the session-scoped PluginCtx, not a per-turn ctx), so the slow list
	// calls cannot starve a /mcp add of its return value. nil sink keeps hot-add
	// quiet — the chat UI re-queries Host.Prompts()/Resources() on demand.
	if c.hasPrompts {
		go h.fetchPrompts(lifeCtx, c, nil)
	}
	if c.hasResources {
		go h.fetchResources(lifeCtx, c, nil)
	}
	return ts, nil
}

// Remove disconnects the named server and drops its prompts/resources, returning
// the namespaced tool-name prefix ("mcp__<server>__") the caller unregisters from
// the tool registry, and whether the server was connected.
func (h *Host) Remove(name string) (toolPrefix string, found bool) {
	h.mu.Lock()
	cancels := append([]context.CancelFunc(nil), h.deferredCancels[name]...)
	delete(h.deferredCancels, name)
	if h.deferredGenerations == nil {
		h.deferredGenerations = make(map[string]uint64)
	}
	h.deferredGenerations[name]++
	if h.deferredGenerations[name] == 0 {
		h.deferredGenerations[name] = 1
	}
	idx := -1
	for i, c := range h.clients {
		if c.name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		h.mu.Unlock()
		for _, cancel := range cancels {
			cancel()
		}
		if len(cancels) == 0 {
			return "", false
		}
		return ToolPrefix(name), true
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

	for _, cancel := range cancels {
		cancel()
	}
	removed.close() // kills the subprocess: outside the lock

	return "mcp__" + normalizeName(name) + "__", true
}

func (h *Host) deferredGenerationCurrent(name string, generation uint64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return !h.closed && generation != 0 && h.deferredGenerations[name] == generation
}

// ErrDeferredSpawnCancelled marks a lazy generation invalidated by remove or
// host shutdown before it could publish a client.
var ErrDeferredSpawnCancelled = errors.New("deferred MCP spawn cancelled")

// start opens the transport on lifeCtx (whose cancellation later closes the
// subprocess) and uses callCtx for the initialize round-trip (whose cancellation
// only bounds startup RPCs). Splitting the two lets a per-plugin timeout cap
// handshake latency without making the timeout context own a successfully
// registered stdio server; the child also has to outlive phase A so phase B
// (prompts + resources) can still call it later. Callers that don't care pass
// the same ctx for both.
func start(lifeCtx, callCtx context.Context, s Spec) (*Client, error) {
	var err error
	s, err = applyStoredLauncherLock(s)
	if err != nil {
		return nil, err
	}
	s, err = resolveProjectLaunchAuthorization(callCtx, s)
	if err != nil {
		return nil, err
	}
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

// resolveProjectLaunchAuthorization deliberately skips identity resolution for
// installed and host-session servers. Their explicit installation is already
// the authorization decision; only repository-declared servers need an exact
// executable or endpoint digest before startup.
func resolveProjectLaunchAuthorization(ctx context.Context, s Spec) (Spec, error) {
	if !s.RequireLaunchApproval {
		return s, nil
	}
	identityDigest, err := projectLaunchIdentityDigest(ctx, s)
	if err != nil {
		return s, err
	}
	return applyEstablishedLaunchGrant(s, identityDigest)
}

func applyEstablishedLaunchGrant(s Spec, identityDigest string) (Spec, error) {
	if !s.RequireLaunchApproval {
		return s, nil
	}
	if s.LaunchManager == nil {
		return s, fmt.Errorf("MCP launch authorization store is unavailable")
	}
	authorized, changed, err := s.LaunchManager.LaunchAuthorized(s.Name, launchConfigSource(s), identityDigest)
	if err != nil {
		return s, err
	}
	if !authorized {
		return s, &launchApprovalError{server: s.Name, changed: changed}
	}
	// A matching exact-identity launch grant is the user's authorization for
	// this project server. Calls proceed like an explicit install, while global
	// deny rules and execution safety boundaries remain authoritative.
	s.Authorized = true
	return s, nil
}

// ResolveStoredAuthorization applies an existing exact project grant without
// starting a process or opening a network connection. Cached lazy/on-demand
// tools use it before strict read-only filtering so every execution path sees
// the same server-level authorization. Errors fail closed by returning the
// original unauthorized Spec; a parent connection surfaces the detailed error.
func ResolveStoredAuthorization(ctx context.Context, s Spec) Spec {
	if !s.RequireLaunchApproval {
		return s
	}
	locked, err := applyStoredLauncherLock(s)
	if err != nil {
		return s
	}
	authorized, err := resolveProjectLaunchAuthorization(ctx, locked)
	if err != nil {
		return s
	}
	return authorized
}

// ServerAuthorized is the single MCP authorization source. Tools do not carry
// an independent trust bit: installation or an exact project launch grant
// authorizes the server, while read-only/destructive classification remains a
// live per-tool safety fact.
func (s Spec) ServerAuthorized() bool {
	return s.Authorized
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
		return newSSETransport(ctx, s)
	default:
		return nil, fmt.Errorf("unknown transport type %q (want stdio|http|sse)", s.Type)
	}
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	params, unregisterProgress := c.withProgress(ctx, method, params)
	defer unregisterProgress()

	callCtx, cancel, timeout := c.contextWithCallTimeout(ctx, method, params)
	if cancel != nil {
		defer cancel()
	}

	res, err := c.callTransport(callCtx, method, params)
	if timeout > 0 && errors.Is(err, context.DeadlineExceeded) && callCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		slog.Warn("plugin: MCP call timed out",
			"server", c.name, "method", method, "tool", rawToolNameFromCallParams(params), "timeout", timeout)
		return nil, c.timeoutError(method, params, timeout)
	}
	return res, err
}

func (c *Client) withProgress(ctx context.Context, method string, params any) (any, func()) {
	if method != "tools/call" {
		return params, func() {}
	}
	sink, ok := tool.ProgressFrom(ctx)
	if !ok {
		return params, func() {}
	}
	router, ok := c.t.(progressTransport)
	if !ok {
		return params, func() {}
	}
	callParams, ok := params.(map[string]any)
	if !ok {
		return params, func() {}
	}

	token := fmt.Sprintf("reasonix-%d", c.progressID.Add(1))
	copyParams := make(map[string]any, len(callParams))
	for key, value := range callParams {
		copyParams[key] = value
	}
	meta := map[string]any{}
	if existing, ok := callParams["_meta"].(map[string]any); ok {
		for key, value := range existing {
			meta[key] = value
		}
	}
	meta["progressToken"] = token
	copyParams["_meta"] = meta
	unregister := router.registerProgress(token, sink)
	return copyParams, unregister
}

func (c *Client) callTransport(ctx context.Context, method string, params any) (json.RawMessage, error) {
	res, err := c.t.call(ctx, method, params)
	if err == nil || method == "initialize" || !isHTTPSessionExpired(err) {
		return res, err
	}
	if initErr := c.initializeSession(ctx, false); initErr != nil {
		return nil, fmt.Errorf("%w; reinitialize failed: %v", err, initErr)
	}
	return c.t.call(ctx, method, params)
}

func (c *Client) contextWithCallTimeout(ctx context.Context, method string, params any) (context.Context, context.CancelFunc, time.Duration) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, nil, 0
	}
	timeout := c.callTimeout(method, params)
	if timeout <= 0 {
		timeout = defaultCallTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	return callCtx, cancel, timeout
}

func (c *Client) callTimeout(method string, params any) time.Duration {
	if method == "tools/call" {
		if raw := rawToolNameFromCallParams(params); raw != "" {
			if timeout := c.spec.ToolTimeouts[raw]; timeout > 0 {
				return timeout
			}
		}
	}
	if c.spec.CallTimeout > 0 {
		return c.spec.CallTimeout
	}
	if c.spec.DefaultCallTimeout > 0 {
		return c.spec.DefaultCallTimeout
	}
	return defaultCallTimeout
}

func rawToolNameFromCallParams(params any) string {
	m, ok := params.(map[string]any)
	if !ok {
		return ""
	}
	name, _ := m["name"].(string)
	return name
}

func (c *Client) timeoutError(method string, params any, timeout time.Duration) error {
	if method == "tools/call" {
		if raw := rawToolNameFromCallParams(params); raw != "" {
			return fmt.Errorf("MCP tool %q timed out after %s; increase tool_timeout_seconds or call_timeout_seconds to allow longer runs: %w",
				c.name+"."+raw, formatTimeout(timeout), context.DeadlineExceeded)
		}
	}
	return fmt.Errorf("MCP method %q on server %q timed out after %s; increase mcp_call_timeout_seconds or call_timeout_seconds to allow longer runs: %w",
		method, c.name, formatTimeout(timeout), context.DeadlineExceeded)
}

func formatTimeout(timeout time.Duration) string {
	if timeout > 0 && timeout%time.Second == 0 {
		return fmt.Sprintf("%ds", int(timeout/time.Second))
	}
	return timeout.String()
}

func (c *Client) notify(ctx context.Context, method string, params any) error {
	return c.t.notify(ctx, method, params)
}

func (c *Client) close() { c.t.close() }

func isHTTPSessionExpired(err error) bool {
	var expired *httpSessionExpiredError
	return errors.As(err, &expired)
}

func (c *Client) initialize(ctx context.Context) error {
	return c.initializeSession(ctx, true)
}

func (c *Client) initializeSession(ctx context.Context, recordCapabilities bool) error {
	capabilities := map[string]any{}
	if len(mcpRoots(c.spec.WorkspaceRoot)) > 0 {
		capabilities["roots"] = map[string]any{"listChanged": false}
	}
	res, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    capabilities,
		"clientInfo":      map[string]any{"name": "reasonix", "version": "dev"},
	})
	if err != nil {
		return err
	}
	if !recordCapabilities {
		// Runtime session refresh must not rewrite startup-only capability flags.
		return c.notify(ctx, "notifications/initialized", map[string]any{})
	}
	// Record which optional capabilities the server advertises. Presence of the
	// key (even with an empty object) signals support.
	var ir struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(res, &ir); err != nil {
		slog.Warn("plugin: parse initialize capabilities", "server", c.name, "err", err)
	}
	_, c.hasTools = ir.Capabilities["tools"]
	_, c.hasPrompts = ir.Capabilities["prompts"]
	_, c.hasResources = ir.Capabilities["resources"]

	return c.notify(ctx, "notifications/initialized", map[string]any{})
}

type mcpTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	// Annotations carries MCP's optional tool hints. readOnlyHint controls reader
	// classification; destructiveHint remains destructive even when another hint
	// claims the tool is read-only. Approval policy is applied separately.
	Annotations *struct {
		ReadOnlyHint    bool `json:"readOnlyHint"`
		DestructiveHint bool `json:"destructiveHint"`
	} `json:"annotations"`
}

func (c *Client) listTools(ctx context.Context) ([]tool.Tool, error) {
	c.toolsMu.Lock()
	defer c.toolsMu.Unlock()
	if c.toolsListed {
		return append([]tool.Tool(nil), c.toolAdapters...), nil
	}

	out, err := c.listToolsRawSettled(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateMCPToolNames(out); err != nil {
		return nil, fmt.Errorf("plugin %q: %w", c.name, err)
	}

	toolInfos := make([]ToolInfo, 0, len(out))
	tools := make([]tool.Tool, 0, len(out))
	normalizedSchemas := make(map[string]json.RawMessage, len(out))
	for _, t := range out {
		schema, err := normalizeAndValidateToolSchema(t.InputSchema)
		if err != nil {
			continue
		}
		normalizedSchemas[t.Name] = schema
	}
	for _, t := range out {
		readOnlyHint := t.Annotations != nil && t.Annotations.ReadOnlyHint
		destructiveHint := t.Annotations != nil && t.Annotations.DestructiveHint
		info := ToolInfo{Name: t.Name, Description: t.Description, ReadOnlyHint: readOnlyHint, DestructiveHint: destructiveHint}
		schema, ok := normalizedSchemas[t.Name]
		if !ok {
			if _, err := normalizeAndValidateToolSchema(t.InputSchema); err != nil {
				info.SchemaError = schemaValidationError(err)
			}
			toolInfos = append(toolInfos, info)
			continue
		}
		visibleName := t.Name
		if c.spec.StripRawPrefix != "" {
			visibleName = strings.TrimPrefix(visibleName, c.spec.StripRawPrefix)
		}
		readOnly := readOnlyHint
		toolInfos = append(toolInfos, info)
		tools = append(tools, &remoteTool{
			client:           c,
			name:             toolName(c.name, visibleName),
			rawName:          t.Name,
			visibleName:      visibleName,
			desc:             t.Description,
			schema:           schema,
			outputSchema:     t.OutputSchema,
			declaredReadOnly: readOnlyHint,
			readOnly:         readOnly,
			destructive:      destructiveHint,
		})
	}
	sort.SliceStable(toolInfos, func(i, j int) bool { return toolInfos[i].Name < toolInfos[j].Name })
	sortedTools := sortToolsByName(tools)
	c.tools = toolInfos
	c.toolAdapters = append([]tool.Tool(nil), sortedTools...)
	c.toolsListed = true
	return append([]tool.Tool(nil), sortedTools...), nil
}

func normalizeAndValidateToolSchema(raw json.RawMessage) (json.RawMessage, error) {
	schema := canonicalizeSchema(raw)
	if err := provider.ValidateToolSchema(schema); err != nil {
		return nil, err
	}
	return schema, nil
}

func schemaValidationError(err error) string {
	const maxRunes = 512
	msg := strings.TrimSpace(err.Error())
	runes := []rune(msg)
	if len(runes) > maxRunes {
		msg = string(runes[:maxRunes]) + "..."
	}
	return "invalid input schema: " + msg
}

func (c *Client) listToolsRaw(ctx context.Context) ([]mcpTool, error) {
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
	return out.Tools, nil
}

// listToolsRawSettled gives dynamically registering servers a bounded startup
// window before their initial tool catalog is considered complete.
func (c *Client) listToolsRawSettled(ctx context.Context) ([]mcpTool, error) {
	out, err := c.listToolsRaw(ctx)
	if err != nil || !c.hasTools || len(out) > 0 {
		return out, err
	}
	for _, delay := range advertisedToolsEmptyListRetryDelays {
		if err := sleepContext(ctx, delay); err != nil {
			return nil, err
		}
		out, err = c.listToolsRaw(ctx)
		if err != nil || len(out) > 0 {
			return out, err
		}
	}
	return out, nil
}

func validateMCPToolNames(tools []mcpTool) error {
	seen := make(map[string]bool, len(tools))
	for _, candidate := range tools {
		name := strings.TrimSpace(candidate.Name)
		if name == "" {
			return fmt.Errorf("tools/list returned an empty tool name")
		}
		if seen[candidate.Name] {
			return fmt.Errorf("tools/list returned duplicate tool name %q", candidate.Name)
		}
		seen[candidate.Name] = true
	}
	return nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) cachedTools() ([]tool.Tool, bool) {
	c.toolsMu.Lock()
	defer c.toolsMu.Unlock()
	if !c.toolsListed {
		return nil, false
	}
	return append([]tool.Tool(nil), c.toolAdapters...), true
}

// toolName builds Reasonix's canonical model-visible name
// "mcp__<server>__<tool>". The registry separately resolves unique portable
// and Claude plugin-qualified references without exposing duplicate schemas.
func toolName(server, raw string) string {
	return ToolPrefix(server) + normalizeName(raw)
}

// ToolPrefix is the model-visible namespace prefix for every tool from server.
func ToolPrefix(server string) string {
	return "mcp__" + normalizeName(server) + "__"
}

// MCPConnectPermissionName is the canonical permission and hook identity for
// starting server on demand. It is intentionally outside the mcp__ tool
// namespace: permission rules match tool names exactly, so a connect must have
// its own non-colliding name instead of pretending a tool-prefix is a glob.
func MCPConnectPermissionName(server string) string {
	return "mcp_connect__" + normalizeName(server)
}

// ModelToolName is the canonical model-visible name for server's raw tool —
// including the collision-hash suffix normalizeName appends when the raw name
// needed sanitising. Every permission/hook/audit surface that names an MCP
// tool must build the name through this function; a second normalization that
// skips the hash would let deny/ask rules written for the executed name miss.
func ModelToolName(server, raw string) string {
	return toolName(server, raw)
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
	client           *Client
	name             string // namespaced "mcp__<server>__<tool>"
	rawName          string // original name for tools/call
	visibleName      string // raw name after configured prefix stripping
	desc             string
	schema           json.RawMessage
	outputSchema     json.RawMessage
	declaredReadOnly bool // server hint, independent of server authorization
	readOnly         bool // effective reader classification for this live snapshot
	// destructive is the MCP destructiveHint. It takes precedence over a
	// conflicting readOnlyHint in Plan and strict read-only execution.
	destructive bool
}

func (t *remoteTool) Name() string        { return t.name }
func (t *remoteTool) Description() string { return t.desc }
func (t *remoteTool) MCPServerName() string {
	if t.client == nil {
		return ""
	}
	return t.client.name
}
func (t *remoteTool) MCPRawToolName() string     { return t.rawName }
func (t *remoteTool) MCPVisibleToolName() string { return t.visibleName }
func (t *remoteTool) MCPPackageName() string {
	if t.client == nil {
		return ""
	}
	return t.client.spec.Package
}

func (t *remoteTool) MCPServerAuthorized() bool {
	return t.client != nil && t.client.spec.ServerAuthorized()
}

// ReadOnly reflects MCP readOnlyHint plus backward-compatible Spec overrides.
// It defaults to false, so opaque tools remain write-capable unless the server
// or local configuration explicitly classifies them as read-only.
func (t *remoteTool) securitySnapshot() (declaredReadOnly, readOnly, destructive bool) {
	if t.client == nil {
		return t.declaredReadOnly, t.readOnly, t.destructive
	}
	t.client.toolsMu.Lock()
	defer t.client.toolsMu.Unlock()
	return t.declaredReadOnly, t.readOnly, t.destructive
}

func (t *remoteTool) ReadOnly() bool {
	_, readOnly, _ := t.securitySnapshot()
	return readOnly
}

func (t *remoteTool) MCPDestructiveHint() bool {
	_, _, destructive := t.securitySnapshot()
	return destructive
}

func (t *remoteTool) Schema() json.RawMessage {
	if len(t.schema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return canonicalizeSchema(t.schema)
}

func (t *remoteTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	text, _, err := t.ExecuteWithImages(ctx, args)
	return text, err
}

// ExecuteWithImages implements tool.ImageTool: MCP results may carry image
// content items, which callers with a structural image channel (the agent)
// forward to vision models instead of relying on the text placeholders alone.
func (t *remoteTool) ExecuteWithImages(ctx context.Context, args json.RawMessage) (string, []string, error) {
	var argMap map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argMap); err != nil {
			return "", nil, fmt.Errorf("invalid args: %w", err)
		}
	}
	_, readOnly, destructive := t.securitySnapshot()
	if tool.HasReaderExecutionIntent(ctx) {
		// Final, linearizable check for a reader-authorized call: the snapshot
		// above and every live security reconciliation serialize on the owning
		// client's toolsMu. A call approved as a non-destructive reader must never
		// execute after authorization or safety metadata changed — state drift
		// here returns an actionable error instead of
		// dispatching.
		if !t.MCPServerAuthorized() || !readOnly || destructive {
			return "", nil, fmt.Errorf("MCP server %q changed the authorization or security metadata for tool %q; the call was blocked before dispatch — refresh the server from a parent session before retrying", t.client.name, t.rawName)
		}
	}
	if tool.HasNonDestructiveMCPExecutionIntent(ctx) {
		// Planner lane: authorized + non-destructive only. Missing readOnlyHint
		// is intentional and does not block; destructive promotion or lost
		// authorization must produce zero tools/call.
		if !t.MCPServerAuthorized() || destructive {
			return "", nil, fmt.Errorf("MCP server %q changed the authorization or destructive classification for tool %q; the call was blocked before dispatch — retry so Reasonix can re-apply the current Planner MCP safety boundary", t.client.name, t.rawName)
		}
	}
	res, err := t.client.call(ctx, "tools/call", map[string]any{
		"name":      t.rawName,
		"arguments": argMap,
	})
	if err != nil {
		return "", nil, err
	}
	return parseToolResult(res)
}

// Tool-result images are forwarded to vision models as base64 data URLs, so
// each item is validated and budgeted here rather than trusted from the MCP
// server: payloads that are oversized, unparseable, beyond the per-result
// count, or of a mime type outside the set every supported vision API accepts
// are replaced with a text placeholder instead of poisoning the provider
// request.
const (
	maxToolResultImageBytes = 4 << 20 // base64 length; stays under provider per-image and request caps
	maxToolResultImages     = 5
)

var toolResultImageMimes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// parseToolResult flattens an MCP tools/call result into plain text plus the
// image content items as data URLs. Every image item leaves a short placeholder
// in the text at its position, so text-only consumers (and non-vision models)
// still learn an image was returned.
func parseToolResult(res json.RawMessage) (string, []string, error) {
	var out struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", nil, fmt.Errorf("decode tool result: %w", err)
	}
	var sb strings.Builder
	var images []string
	for _, c := range out.Content {
		switch c.Type {
		case "text":
			sb.WriteString(c.Text)
		case "image":
			placeholder, url := toolResultImage(c.MimeType, c.Data, len(images))
			sb.WriteString(placeholder)
			if url != "" {
				images = append(images, url)
			}
		}
	}
	text := sb.String()
	if out.IsError {
		return text, images, fmt.Errorf("plugin tool reported error: %s", text)
	}
	return text, images, nil
}

// toolResultImage validates one MCP image content item and returns its text
// placeholder plus the data URL to forward ("" when the item is dropped).
func toolResultImage(mime, data string, kept int) (placeholder, url string) {
	if kept >= maxToolResultImages {
		return "[image omitted: per-result image limit reached]", ""
	}
	mime = strings.ToLower(strings.TrimSpace(mime))
	if mime == "" {
		mime = "image/png"
	}
	if !toolResultImageMimes[mime] {
		return "[image omitted: unsupported type " + mime + "]", ""
	}
	// Some servers wrap base64 in whitespace; vision APIs reject non-canonical
	// payloads, so normalize before validating.
	data = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t', ' ':
			return -1
		}
		return r
	}, data)
	if data == "" {
		return "[image omitted: no data]", ""
	}
	if len(data) > maxToolResultImageBytes {
		return fmt.Sprintf("[image omitted: %d bytes exceeds the %d-byte limit]", len(data), maxToolResultImageBytes), ""
	}
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return "[image omitted: invalid base64]", ""
	}
	return "[image: " + mime + "]", "data:" + mime + ";base64," + data
}
