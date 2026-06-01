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
	"strings"

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
	clients   []*Client
	prompts   []Prompt
	resources []Resource
	failures  []Failure
}

// Prompts returns every MCP prompt discovered across connected servers.
func (h *Host) Prompts() []Prompt { return h.prompts }

// Resources returns every MCP resource discovered across connected servers.
func (h *Host) Resources() []Resource { return h.resources }

// ServerNames returns the connected servers' names, in connection order.
func (h *Host) ServerNames() []string {
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
	for _, c := range h.clients {
		if c.name == server {
			return c.readResource(ctx, uri)
		}
	}
	return "", fmt.Errorf("no MCP server named %q", server)
}

// StartAll connects every plugin in parallel, performs the MCP handshake, and
// returns the union of their tools (namespaced "mcp__<server>__<tool>"). On any
// failure it tears down everything started so far. The caller must Close the Host.
//
// For stdio plugins, subprocess lifetime is bound to ctx (via
// exec.CommandContext): cancelling ctx kills the children and unblocks reads.
func StartAll(ctx context.Context, specs []Spec) (*Host, []tool.Tool, error) {
	if len(specs) == 0 {
		return &Host{}, nil, nil
	}

	type result struct {
		idx    int
		client *Client
		tools  []tool.Tool
		err    error
	}

	// Start all plugins in parallel — each is an independent subprocess or
	// HTTP connection with no cross-dependencies.
	ch := make(chan result, len(specs))
	for i, s := range specs {
		go func(idx int, spec Spec) {
			c, err := start(ctx, spec)
			if err != nil {
				ch <- result{idx: idx, err: fmt.Errorf("start plugin %q: %w", spec.Name, err)}
				return
			}

			ts, err := c.listTools(ctx)
			if err != nil {
				c.close()
				ch <- result{idx: idx, err: fmt.Errorf("list tools from %q: %w", spec.Name, err)}
				return
			}
			c.toolCount = len(ts)

			// Prompts and resources are auxiliary: only fetched when the server
			// advertised the capability, and a listing error is tolerated (skipped)
			// rather than failing the whole session over a non-essential surface.
			if c.hasPrompts {
				if ps, perr := c.listPrompts(ctx); perr == nil {
					c.prompts = ps
				}
			}
			if c.hasResources {
				if rs, rerr := c.listResources(ctx); rerr == nil {
					c.resources = rs
				}
			}

			ch <- result{idx: idx, client: c, tools: ts}
		}(i, s)
	}

	// Collect results in index order so the Host.clients slice matches the
	// original specs order (stable for /mcp status display).
	results := make([]result, len(specs))
	for range specs {
		r := <-ch
		results[r.idx] = r
	}

	// Collect every started client into the Host first, so that if any plugin
	// failed, h.Close() tears down all of them — including ones whose index sits
	// after the failure (parallel start means they're already running).
	h := &Host{}
	var tools []tool.Tool
	var firstErr error
	for _, r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		h.clients = append(h.clients, r.client)
		tools = append(tools, r.tools...)
		h.prompts = append(h.prompts, r.client.prompts...)
		h.resources = append(h.resources, r.client.resources...)
	}
	if firstErr != nil {
		h.Close()
		return nil, nil, firstErr
	}
	return h, tools, nil
}

// StartAvailable connects every plugin it can and records failures on the host
// instead of aborting the whole session. The returned tools are the union of the
// successfully connected servers.
func StartAvailable(ctx context.Context, specs []Spec) (*Host, []tool.Tool) {
	h := &Host{}
	var tools []tool.Tool
	for _, s := range specs {
		ts, err := h.addConnected(ctx, s)
		if err != nil {
			h.RecordFailure(s, err)
			continue
		}
		tools = append(tools, ts...)
	}
	return h, tools
}

// Close terminates all plugin connections.
func (h *Host) Close() {
	for _, c := range h.clients {
		c.close()
	}
}

// Client is one MCP server connection: a name plus the transport carrying its
// JSON-RPC. The MCP-level methods (initialize, listTools, …) are transport-
// agnostic — they go through t.
type Client struct {
	name string
	t    transport

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
}

// ServerStatus summarises one connected server for the /mcp command.
type ServerStatus struct {
	Name      string
	Transport string
	Tools     int
	Prompts   int
	Resources int
}

// Failure records one MCP server that was configured but could not connect.
type Failure struct {
	Name      string
	Transport string
	Error     string
}

// Servers returns a status summary per connected server, in connection order.
func (h *Host) Servers() []ServerStatus {
	out := make([]ServerStatus, 0, len(h.clients))
	for _, c := range h.clients {
		s := ServerStatus{Name: c.name, Transport: c.transport, Tools: c.toolCount}
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
	out := make([]Failure, len(h.failures))
	copy(out, h.failures)
	return out
}

// RecordFailure stores a failed MCP connection attempt for status UIs.
func (h *Host) RecordFailure(s Spec, err error) {
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

// has reports whether a server with this name is already connected.
func (h *Host) has(name string) bool {
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
	c, err := start(ctx, s)
	if err != nil {
		return nil, err
	}
	ts, err := c.listTools(ctx)
	if err != nil {
		c.close()
		return nil, fmt.Errorf("list tools: %w", err)
	}
	c.toolCount = len(ts)
	h.clients = append(h.clients, c)
	if c.hasPrompts {
		if ps, perr := c.listPrompts(ctx); perr == nil {
			h.prompts = append(h.prompts, ps...)
		} else {
			slog.Warn("plugin: listPrompts failed", "server", s.Name, "err", perr)
		}
	}
	if c.hasResources {
		if rs, rerr := c.listResources(ctx); rerr == nil {
			h.resources = append(h.resources, rs...)
		} else {
			slog.Warn("plugin: listResources failed", "server", s.Name, "err", rerr)
		}
	}
	h.clearFailure(s.Name)
	return ts, nil
}

// Remove disconnects the named server and drops its prompts/resources, returning
// the namespaced tool-name prefix ("mcp__<server>__") the caller unregisters from
// the tool registry, and whether the server was connected.
func (h *Host) Remove(name string) (toolPrefix string, found bool) {
	idx := -1
	for i, c := range h.clients {
		if c.name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", false
	}
	h.clients[idx].close()
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

	return "mcp__" + normalizeName(name) + "__", true
}

func start(ctx context.Context, s Spec) (*Client, error) {
	t, err := newTransport(ctx, s)
	if err != nil {
		return nil, err
	}
	tt := strings.ToLower(strings.TrimSpace(s.Type))
	if tt == "" {
		tt = "stdio"
	}
	c := &Client{name: s.Name, t: t, transport: tt}
	if err := c.initialize(ctx); err != nil {
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

	tools := make([]tool.Tool, 0, len(out.Tools))
	for _, t := range out.Tools {
		tools = append(tools, &remoteTool{
			client:   c,
			name:     toolName(c.name, t.Name),
			rawName:  t.Name,
			desc:     t.Description,
			schema:   canonicalizeSchema(t.InputSchema),
			readOnly: t.Annotations != nil && t.Annotations.ReadOnlyHint,
		})
	}
	return sortToolsByName(tools), nil
}

// toolName builds the model-visible namespaced name "mcp__<server>__<tool>",
// matching Claude Code. Spaces in either part are normalised to underscores so
// the name is a clean identifier the model can call.
func toolName(server, raw string) string {
	return "mcp__" + normalizeName(server) + "__" + normalizeName(raw)
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
	readOnly bool // from the tool's MCP readOnlyHint annotation
}

func (t *remoteTool) Name() string        { return t.name }
func (t *remoteTool) Description() string { return t.desc }

// ReadOnly reflects the tool's MCP readOnlyHint annotation. It defaults to
// false (opaque — we can't inspect a remote tool's side effects), so plugins
// opt into parallel-batch dispatch and the permission layer's reader-default
// by explicitly declaring readOnlyHint: true in tools/list.
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

func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}
