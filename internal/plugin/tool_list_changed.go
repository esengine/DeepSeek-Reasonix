package plugin

import (
	"context"
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

type toolListRefreshState struct {
	mu                sync.Mutex
	ctx               context.Context
	cancel            context.CancelFunc
	closed            bool
	pending           bool
	running           bool
	onChanged         func([]tool.Tool)
	stopNotifications func()
}

type toolListSubscriptions struct {
	nextID      atomic.Uint64
	subscribers map[uint64]toolListSubscriber
}

type toolListSubscriber struct {
	ctx      context.Context
	callback func(Spec, []tool.Tool)
}

// SubscribeToolListChanges receives refreshed live tool sets after a connected
// server sends notifications/tools/list_changed. The returned function removes
// the subscription; ctx cancellation does the same automatically.
func (h *Host) SubscribeToolListChanges(ctx context.Context, callback func(Spec, []tool.Tool)) func() {
	if h == nil || callback == nil {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	id := h.toolListChanges.nextID.Add(1)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return func() {}
	}
	if h.toolListChanges.subscribers == nil {
		h.toolListChanges.subscribers = map[uint64]toolListSubscriber{}
	}
	h.toolListChanges.subscribers[id] = toolListSubscriber{ctx: ctx, callback: callback}
	h.mu.Unlock()

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
	subscribers := make([]toolListSubscriber, 0, len(h.toolListChanges.subscribers))
	for _, subscriber := range h.toolListChanges.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	h.mu.RUnlock()
	for _, subscriber := range subscribers {
		if subscriber.ctx.Err() != nil {
			continue
		}
		subscriber.callback(spec, append([]tool.Tool(nil), tools...))
	}
}

func (c *Client) watchToolListChanges() {
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
	c.refresh.pending = true
	if c.refresh.running {
		c.refresh.mu.Unlock()
		return
	}
	c.refresh.running = true
	c.refresh.mu.Unlock()
	go c.runToolsRefreshes()
}

func (c *Client) runToolsRefreshes() {
	for {
		c.refresh.mu.Lock()
		if c.refresh.closed || !c.refresh.pending {
			c.refresh.running = false
			c.refresh.mu.Unlock()
			return
		}
		c.refresh.pending = false
		c.refresh.mu.Unlock()

		ctx := c.refresh.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		tools, err := c.refreshTools(ctx)
		if err != nil {
			if ctx.Err() != nil {
				c.refresh.mu.Lock()
				c.refresh.running = false
				c.refresh.pending = false
				c.refresh.mu.Unlock()
				return
			}
			slog.Warn("plugin: refresh tools after list_changed failed", "server", c.name, "err", err)
			continue
		}
		c.refresh.mu.Lock()
		if c.refresh.closed {
			c.refresh.running = false
			c.refresh.mu.Unlock()
			return
		}
		callback := c.refresh.onChanged
		c.refresh.mu.Unlock()
		if callback != nil {
			callback(append([]tool.Tool(nil), tools...))
		}
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
	c.toolsMu.Lock()
	defer c.toolsMu.Unlock()
	if c.toolsListed {
		return append([]tool.Tool(nil), c.toolAdapters...), nil
	}
	return c.listToolsLocked(ctx)
}

func (c *Client) refreshTools(ctx context.Context) ([]tool.Tool, error) {
	c.toolsMu.Lock()
	defer c.toolsMu.Unlock()
	return c.listToolsLocked(ctx)
}

// listToolsLocked always performs a live tools/list call and replaces the
// cached adapters. Callers hold toolsMu so a list_changed refresh cannot race
// startup discovery or another notification refresh.
func (c *Client) listToolsLocked(ctx context.Context) ([]tool.Tool, error) {
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
		schema, ok := normalizedSchemas[candidate.Name]
		if !ok {
			if _, err := normalizeAndValidateToolSchema(candidate.InputSchema); err != nil {
				info.SchemaError = schemaValidationError(err)
			}
			toolInfos = append(toolInfos, info)
			continue
		}
		visibleName := candidate.Name
		if c.spec.StripRawPrefix != "" {
			visibleName = strings.TrimPrefix(visibleName, c.spec.StripRawPrefix)
		}
		toolInfos = append(toolInfos, info)
		tools = append(tools, &remoteTool{
			client:           c,
			name:             toolName(c.name, visibleName),
			rawName:          candidate.Name,
			visibleName:      visibleName,
			desc:             candidate.Description,
			schema:           schema,
			outputSchema:     candidate.OutputSchema,
			declaredReadOnly: readOnlyHint,
			readOnly:         readOnlyHint,
			destructive:      destructiveHint,
		})
	}
	sort.SliceStable(toolInfos, func(i, j int) bool { return toolInfos[i].Name < toolInfos[j].Name })
	sortedTools := sortToolsByName(tools)
	c.tools = toolInfos
	c.toolAdapters = append([]tool.Tool(nil), sortedTools...)
	c.toolCount = len(sortedTools)
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
	c.toolsMu.RLock()
	defer c.toolsMu.RUnlock()
	if !c.toolsListed {
		return nil, false
	}
	return append([]tool.Tool(nil), c.toolAdapters...), true
}
