package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

const (
	toolListRefreshDebounce    = 300 * time.Millisecond
	toolListRefreshMaxBackoff  = 5 * time.Second
	toolListRefreshMaxAttempts = 4
)

type toolListRefreshWait func(context.Context, time.Duration) error

type toolListRefreshState struct {
	mu                sync.Mutex
	ctx               context.Context
	cancel            context.CancelFunc
	closed            bool
	running           bool
	cycleDone         chan struct{}
	onChanged         func([]tool.Tool)
	stopNotifications func()
	wait              toolListRefreshWait
	noticeRevision    atomic.Uint64
	publishedRevision atomic.Uint64
}

// toolCatalogSnapshot is immutable after publication. All slices are built off
// lock and replaced together under toolsMu, so readers see either the complete
// old catalog or the complete new catalog.
type toolCatalogSnapshot struct {
	listed      bool
	generation  uint64
	fingerprint [sha256.Size]byte
	infos       []ToolInfo
	adapters    []tool.Tool
}

type toolCatalogFingerprintEntry struct {
	RawName         string          `json:"raw_name"`
	VisibleName     string          `json:"visible_name"`
	Description     string          `json:"description"`
	Schema          json.RawMessage `json:"schema,omitempty"`
	OutputSchema    json.RawMessage `json:"output_schema,omitempty"`
	SchemaError     string          `json:"schema_error,omitempty"`
	ReadOnlyHint    bool            `json:"read_only_hint,omitempty"`
	DestructiveHint bool            `json:"destructive_hint,omitempty"`
}

type toolListSubscriptions struct {
	nextID      atomic.Uint64
	subscribers map[uint64]*toolListSubscriber
}

type toolListSubscriber struct {
	ctx        context.Context
	callback   func(Spec, []tool.Tool)
	deliveryMu sync.Mutex
}

type toolListReplay struct {
	spec  Spec
	tools []tool.Tool
}

// SubscribeToolListChanges receives refreshed live tool sets after a connected
// server sends notifications/tools/list_changed. The returned function removes
// the subscription; ctx cancellation does the same automatically.
func (h *Host) SubscribeToolListChanges(ctx context.Context, callback func(Spec, []tool.Tool)) func() {
	return h.subscribeToolListChanges(ctx, callback, false)
}

// SubscribeToolListChangesWithReplay first delivers the complete catalogs of
// already-connected servers, then every later list_changed publication. Replay
// and live deliveries are serialized per subscriber so an update cannot
// overtake the snapshot used to initialize a late session runtime.
func (h *Host) SubscribeToolListChangesWithReplay(ctx context.Context, callback func(Spec, []tool.Tool)) func() {
	return h.subscribeToolListChanges(ctx, callback, true)
}

func (h *Host) subscribeToolListChanges(ctx context.Context, callback func(Spec, []tool.Tool), replay bool) func() {
	if h == nil || callback == nil {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	subscriber := &toolListSubscriber{ctx: ctx, callback: callback}
	if replay {
		// Publications can discover the subscriber as soon as h.mu is released.
		// Hold its delivery gate until every captured catalog has been replayed so
		// those later publications cannot apply first and then be overwritten.
		subscriber.deliveryMu.Lock()
	}
	id := h.toolListChanges.nextID.Add(1)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		if replay {
			subscriber.deliveryMu.Unlock()
		}
		return func() {}
	}
	if h.toolListChanges.subscribers == nil {
		h.toolListChanges.subscribers = map[uint64]*toolListSubscriber{}
	}
	h.toolListChanges.subscribers[id] = subscriber
	var snapshots []toolListReplay
	if replay {
		snapshots = make([]toolListReplay, 0, len(h.clients))
		for _, client := range h.clients {
			if client == nil {
				continue
			}
			if tools, ok := client.cachedTools(); ok {
				snapshots = append(snapshots, toolListReplay{spec: client.spec, tools: tools})
			}
		}
	}
	h.mu.Unlock()
	if replay {
		for _, snapshot := range snapshots {
			if ctx.Err() != nil {
				break
			}
			callback(snapshot.spec, append([]tool.Tool(nil), snapshot.tools...))
		}
		subscriber.deliveryMu.Unlock()
	}

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.toolListChanges.subscribers, id)
			h.mu.Unlock()
		})
	}
	stop := context.AfterFunc(ctx, unsubscribe)
	return func() {
		stop()
		unsubscribe()
	}
}

func (h *Host) bindToolListChanges(c *Client) {
	if h == nil || c == nil {
		return
	}
	c.setToolsChangedCallback(func(tools []tool.Tool) {
		h.publishToolListChange(c, tools)
	})
	c.watchToolListChanges()
}

func (h *Host) registerStartedClient(c *Client, tools []tool.Tool) ([]tool.Tool, error) {
	h.mu.Lock()
	err := h.noteClientLocked(c, nil)
	h.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if cached, ok := c.cachedTools(); ok {
		return cached, nil
	}
	return tools, nil
}

func (h *Host) publishToolListChange(c *Client, tools []tool.Tool) {
	if h == nil || c == nil {
		return
	}
	h.mu.RLock()
	if h.closed || h.lookupClientLocked(c.name) != c {
		h.mu.RUnlock()
		return
	}
	spec := c.spec
	subscribers := make([]*toolListSubscriber, 0, len(h.toolListChanges.subscribers))
	for _, subscriber := range h.toolListChanges.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	h.mu.RUnlock()
	for _, subscriber := range subscribers {
		deliverToolListChange(subscriber, spec, tools)
	}
}

func deliverToolListChange(subscriber *toolListSubscriber, spec Spec, tools []tool.Tool) {
	if subscriber == nil {
		return
	}
	subscriber.deliveryMu.Lock()
	defer subscriber.deliveryMu.Unlock()
	if subscriber.ctx.Err() != nil {
		return
	}
	subscriber.callback(spec, append([]tool.Tool(nil), tools...))
}

func (c *Client) watchToolListChanges() {
	if !c.supportsToolListChanged {
		return
	}
	t, ok := c.t.(notificationTransport)
	if !ok {
		return
	}
	c.refresh.mu.Lock()
	defer c.refresh.mu.Unlock()
	if c.refresh.closed || c.refresh.stopNotifications != nil {
		return
	}
	c.refresh.stopNotifications = t.registerNotification("notifications/tools/list_changed", func(json.RawMessage) {
		c.requestToolsRefresh()
	})
}

func (c *Client) requestToolsRefresh() {
	c.refresh.mu.Lock()
	if c.refresh.closed {
		c.refresh.mu.Unlock()
		return
	}
	c.refresh.noticeRevision.Add(1)
	if c.refresh.running {
		c.refresh.mu.Unlock()
		return
	}
	c.refresh.running = true
	c.refresh.cycleDone = make(chan struct{})
	c.refresh.mu.Unlock()
	go c.runToolsRefreshes()
}

// ensureToolsRefresh lets a user retry recover a dirty catalog after a bounded
// refresh cycle failed or exhausted its catch-up attempts. It never
// invents a new notice revision and therefore cannot create a self-sustaining
// loop without a real notification or call attempt.
func (c *Client) ensureToolsRefresh() {
	if c == nil || !c.toolCatalogStale() {
		return
	}
	c.refresh.mu.Lock()
	if c.refresh.closed || c.refresh.running {
		c.refresh.mu.Unlock()
		return
	}
	c.refresh.running = true
	c.refresh.cycleDone = make(chan struct{})
	c.refresh.mu.Unlock()
	go c.runToolsRefreshes()
}

func (c *Client) runToolsRefreshes() {
	c.refresh.mu.Lock()
	ctx := c.refresh.ctx
	wait := c.refresh.wait
	c.refresh.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if wait == nil {
		wait = sleepContext
	}
	finished := false
	defer func() {
		if !finished {
			c.finishToolsRefreshCycle()
		}
	}()

	// Bound catch-up because some servers self-notify on every tools/list.
	// Exhaustion stays stale and fail-closed; a later notice or user retry starts
	// a fresh bounded cycle.
	refreshDelay := toolListRefreshDebounce
	for range toolListRefreshMaxAttempts {
		if err := wait(ctx, refreshDelay); err != nil {
			return
		}
		c.refresh.mu.Lock()
		closed := c.refresh.closed
		c.refresh.mu.Unlock()
		if closed {
			return
		}
		targetRevision := c.refresh.noticeRevision.Load()
		refreshCtx, cancel := context.WithTimeout(ctx, c.toolListRefreshTimeout())
		tools, changed, err := c.refreshTools(refreshCtx, targetRevision)
		cancel()
		if err != nil {
			if ctx.Err() == nil && !c.closed.Load() {
				slog.Warn("plugin: refresh tools after list_changed failed", "server", c.name, "err", err)
			}
			return
		}
		if changed {
			c.refresh.mu.Lock()
			callback := c.refresh.onChanged
			closed = c.refresh.closed
			c.refresh.mu.Unlock()
			if !closed && callback != nil {
				callback(append([]tool.Tool(nil), tools...))
			}
		}
		c.refresh.mu.Lock()
		if c.refresh.noticeRevision.Load() == targetRevision {
			c.finishToolsRefreshCycleLocked()
			finished = true
			c.refresh.mu.Unlock()
			return
		}
		c.refresh.mu.Unlock()
		refreshDelay = nextToolListRefreshDelay(refreshDelay)
	}
}

func nextToolListRefreshDelay(current time.Duration) time.Duration {
	if current <= 0 {
		return toolListRefreshDebounce
	}
	if current >= toolListRefreshMaxBackoff/2 {
		return toolListRefreshMaxBackoff
	}
	return current * 2
}

func (c *Client) toolListRefreshTimeout() time.Duration {
	startupTimeout := c.spec.ResolvedStartupTimeout()
	callTimeout := c.callTimeout("tools/list", map[string]any{})
	if startupTimeout <= 0 {
		return callTimeout
	}
	if callTimeout <= 0 || startupTimeout < callTimeout {
		return startupTimeout
	}
	return callTimeout
}

func (c *Client) finishToolsRefreshCycle() {
	c.refresh.mu.Lock()
	c.finishToolsRefreshCycleLocked()
	c.refresh.mu.Unlock()
}

func (c *Client) finishToolsRefreshCycleLocked() {
	c.refresh.running = false
	if c.refresh.cycleDone != nil {
		close(c.refresh.cycleDone)
		c.refresh.cycleDone = nil
	}
}

func (c *Client) setToolsChangedCallback(callback func([]tool.Tool)) {
	c.refresh.mu.Lock()
	if c.refresh.closed {
		c.refresh.mu.Unlock()
		return
	}
	c.refresh.onChanged = callback
	c.refresh.mu.Unlock()
}

func (c *Client) listTools(ctx context.Context) ([]tool.Tool, error) {
	if tools, ok := c.cachedTools(); ok {
		return tools, nil
	}
	c.toolListFetchMu.Lock()
	defer c.toolListFetchMu.Unlock()
	if tools, ok := c.cachedTools(); ok {
		return tools, nil
	}
	targetRevision := c.refresh.noticeRevision.Load()
	candidate, err := c.fetchToolCatalog(ctx, true)
	if err != nil {
		return nil, err
	}
	tools, _, err := c.publishToolCatalog(candidate, targetRevision)
	return tools, err
}

func (c *Client) refreshTools(ctx context.Context, targetRevision uint64) ([]tool.Tool, bool, error) {
	c.toolListFetchMu.Lock()
	defer c.toolListFetchMu.Unlock()
	candidate, err := c.fetchToolCatalog(ctx, false)
	if err != nil {
		return nil, false, err
	}
	return c.publishToolCatalog(candidate, targetRevision)
}

// fetchToolCatalog performs MCP I/O and constructs a complete candidate off
// lock. Startup keeps the existing empty-list settling window; notifications
// use one tools/list per attempt so retries remain bounded by the scheduler.
func (c *Client) fetchToolCatalog(ctx context.Context, settleEmpty bool) (toolCatalogSnapshot, error) {
	var (
		out []mcpTool
		err error
	)
	if settleEmpty {
		out, err = c.listToolsRawSettled(ctx)
	} else {
		out, err = c.listToolsRaw(ctx)
	}
	if err != nil {
		return toolCatalogSnapshot{}, err
	}
	if err := validateMCPToolNames(out); err != nil {
		return toolCatalogSnapshot{}, fmt.Errorf("plugin %q: %w", c.name, err)
	}

	toolInfos := make([]ToolInfo, 0, len(out))
	tools := make([]tool.Tool, 0, len(out))
	fingerprintEntries := make([]toolCatalogFingerprintEntry, 0, len(out))
	normalizedSchemas := make(map[string]json.RawMessage, len(out))
	for _, candidate := range out {
		schema, err := normalizeAndValidateToolSchema(candidate.InputSchema)
		if err == nil {
			normalizedSchemas[candidate.Name] = schema
		}
	}
	for _, candidate := range out {
		readOnlyHint := candidate.Annotations != nil && candidate.Annotations.ReadOnlyHint
		destructiveHint := candidate.Annotations != nil && candidate.Annotations.DestructiveHint
		info := ToolInfo{Name: candidate.Name, Description: candidate.Description, ReadOnlyHint: readOnlyHint, DestructiveHint: destructiveHint}
		visibleName := candidate.Name
		if c.spec.StripRawPrefix != "" {
			visibleName = strings.TrimPrefix(visibleName, c.spec.StripRawPrefix)
		}
		schema, ok := normalizedSchemas[candidate.Name]
		if !ok {
			if _, err := normalizeAndValidateToolSchema(candidate.InputSchema); err != nil {
				info.SchemaError = schemaValidationError(err)
			}
			toolInfos = append(toolInfos, info)
			fingerprintEntries = append(fingerprintEntries, toolCatalogFingerprintEntry{
				RawName: candidate.Name, VisibleName: visibleName, Description: candidate.Description,
				SchemaError: info.SchemaError, ReadOnlyHint: readOnlyHint, DestructiveHint: destructiveHint,
			})
			continue
		}
		toolInfos = append(toolInfos, info)
		outputSchema := append(json.RawMessage(nil), candidate.OutputSchema...)
		fingerprintOutputSchema := canonicalizeCatalogJSON(outputSchema)
		tools = append(tools, &remoteTool{
			client:           c,
			name:             toolName(c.name, visibleName),
			rawName:          candidate.Name,
			visibleName:      visibleName,
			desc:             candidate.Description,
			schema:           schema,
			outputSchema:     outputSchema,
			declaredReadOnly: readOnlyHint,
			readOnly:         readOnlyHint,
			destructive:      destructiveHint,
		})
		fingerprintEntries = append(fingerprintEntries, toolCatalogFingerprintEntry{
			RawName: candidate.Name, VisibleName: visibleName, Description: candidate.Description,
			Schema: schema, OutputSchema: fingerprintOutputSchema, ReadOnlyHint: readOnlyHint, DestructiveHint: destructiveHint,
		})
	}
	sort.SliceStable(toolInfos, func(i, j int) bool { return toolInfos[i].Name < toolInfos[j].Name })
	sort.SliceStable(fingerprintEntries, func(i, j int) bool { return fingerprintEntries[i].RawName < fingerprintEntries[j].RawName })
	sortedTools := sortToolsByName(tools)
	fingerprintJSON, err := json.Marshal(fingerprintEntries)
	if err != nil {
		return toolCatalogSnapshot{}, fmt.Errorf("plugin %q: fingerprint tools/list: %w", c.name, err)
	}
	return toolCatalogSnapshot{
		listed: true, fingerprint: sha256.Sum256(fingerprintJSON),
		infos: toolInfos, adapters: sortedTools,
	}, nil
}

func canonicalizeCatalogJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return append(json.RawMessage(nil), raw...)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return append(json.RawMessage(nil), raw...)
	}
	return canonical
}

// publishToolCatalog waits for already-authorized calls, then atomically makes
// a complete candidate current. publishedRevision is advanced under the same
// dispatch gate, making stale checks linearizable with call admission.
func (c *Client) publishToolCatalog(candidate toolCatalogSnapshot, targetRevision uint64) ([]tool.Tool, bool, error) {
	c.toolDispatchMu.Lock()
	defer c.toolDispatchMu.Unlock()
	if c.closed.Load() {
		return nil, false, fmt.Errorf("MCP server %q is closed", c.name)
	}

	c.toolsMu.Lock()
	changed := !c.toolCatalog.listed || c.toolCatalog.fingerprint != candidate.fingerprint
	if changed {
		c.catalogGeneration++
		candidate.generation = c.catalogGeneration
		for _, adapter := range candidate.adapters {
			if remote, ok := adapter.(*remoteTool); ok {
				remote.generation = candidate.generation
			}
		}
		c.toolCatalog = candidate
	}
	tools := append([]tool.Tool(nil), c.toolCatalog.adapters...)
	c.toolsMu.Unlock()
	c.advancePublishedToolListRevision(targetRevision)
	return tools, changed, nil
}

func (c *Client) advancePublishedToolListRevision(revision uint64) {
	for {
		current := c.refresh.publishedRevision.Load()
		if revision <= current || c.refresh.publishedRevision.CompareAndSwap(current, revision) {
			return
		}
	}
}

func (c *Client) toolCatalogStale() bool {
	return c != nil && c.refresh.publishedRevision.Load() < c.refresh.noticeRevision.Load()
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
	c.toolsMu.RLock()
	defer c.toolsMu.RUnlock()
	if !c.toolCatalog.listed {
		return nil, false
	}
	return append([]tool.Tool(nil), c.toolCatalog.adapters...), true
}
