// Following notifications/tools/list_changed: a server that declared
// tools.listChanged may add, rename or drop tools after startup, and a client
// that ignores the notice keeps offering tools that are gone and cannot reach
// the ones that arrived. The catalog is re-read here; who re-registers it is the
// subscriber's business.
package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"reasonix/internal/contract/tool"
)

const toolListChangedMethod = "notifications/tools/list_changed"

const (
	// A burst of notices costs one tools/list: servers announce per tool while
	// registering, and each list is a round trip to the same server.
	toolListRefreshDebounce = 300 * time.Millisecond
	toolListRefreshBackoff  = 5 * time.Second
	// Catch-up is bounded because some servers notify on every tools/list,
	// which would otherwise be a loop with no end state.
	toolListRefreshAttempts = 4
)

// toolRefreshWait is the debounce, replaced in tests to keep them off the clock.
type toolRefreshWait func(context.Context, time.Duration) error

// toolRefresh is one connection's catalog freshness: whether the server said it
// would announce changes, how many announcements have arrived, and whether a
// refresh cycle is draining them.
type toolRefresh struct {
	mu        sync.Mutex
	declared  bool
	running   bool
	closed    bool
	revision  uint64
	ctx       context.Context
	onChanged func([]tool.Tool)
	stop      func()
	wait      toolRefreshWait
}

// declareToolListChanged records what the server advertised at initialize.
// Nothing is watched without it: the spec lets a server change its tools only
// after declaring it will say so.
func (c *Client) declareToolListChanged(declared bool) {
	c.refresh.mu.Lock()
	c.refresh.declared = declared
	c.refresh.mu.Unlock()
}

// setRefreshLifetime records the context that owns this connection, which is
// what later bounds its refreshes. It is set at start, before the client is
// published, so every publication path inherits the same lifetime.
func (c *Client) setRefreshLifetime(lifeCtx context.Context) {
	c.refresh.mu.Lock()
	c.refresh.ctx = lifeCtx
	c.refresh.mu.Unlock()
}

// watchToolListChanges subscribes this connection to its server's notice and
// reports every catalog that differs from the one in hand. onChanged runs on
// the refresh goroutine.
func (c *Client) watchToolListChanges(onChanged func([]tool.Tool)) {
	router, ok := c.t.(notificationTransport)
	if !ok {
		return
	}
	c.refresh.mu.Lock()
	if c.refresh.closed || !c.refresh.declared || c.refresh.stop != nil {
		c.refresh.mu.Unlock()
		return
	}
	c.refresh.onChanged = onChanged
	c.refresh.stop = router.registerNotification(toolListChangedMethod, func(json.RawMessage) {
		c.requestToolsRefresh()
	})
	c.refresh.mu.Unlock()
}

// stopToolListWatch ends the subscription and keeps later notices from starting
// a cycle on a connection that is going away.
func (c *Client) stopToolListWatch() {
	c.refresh.mu.Lock()
	stop := c.refresh.stop
	c.refresh.stop = nil
	c.refresh.closed = true
	c.refresh.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// requestToolsRefresh records one notice. It runs on the transport's read
// goroutine, so it starts the cycle rather than listing here.
func (c *Client) requestToolsRefresh() {
	c.refresh.mu.Lock()
	if c.refresh.closed {
		c.refresh.mu.Unlock()
		return
	}
	c.refresh.revision++
	if c.refresh.running {
		c.refresh.mu.Unlock()
		return
	}
	c.refresh.running = true
	ctx, wait, onChanged := c.refresh.ctx, c.refresh.wait, c.refresh.onChanged
	c.refresh.mu.Unlock()
	go c.runToolRefreshes(ctx, wait, onChanged)
}

func (c *Client) runToolRefreshes(ctx context.Context, wait toolRefreshWait, onChanged func([]tool.Tool)) {
	// An abandoned cycle leaves the catalog stale on purpose: the next notice
	// starts a fresh bounded one rather than this cycle retrying forever.
	settled := false
	defer func() {
		if !settled {
			c.refresh.mu.Lock()
			c.refresh.running = false
			c.refresh.mu.Unlock()
		}
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	if wait == nil {
		wait = sleepContext
	}
	delay := toolListRefreshDebounce
	for range toolListRefreshAttempts {
		if err := wait(ctx, delay); err != nil {
			return
		}
		c.refresh.mu.Lock()
		target, closed := c.refresh.revision, c.refresh.closed
		c.refresh.mu.Unlock()
		if closed {
			return
		}
		refreshCtx, cancel := context.WithTimeout(ctx, c.toolListRefreshTimeout())
		tools, changed, err := c.refreshToolCatalog(refreshCtx)
		cancel()
		if err != nil {
			c.refresh.mu.Lock()
			closing := c.refresh.closed
			c.refresh.mu.Unlock()
			if ctx.Err() == nil && !closing {
				slog.Warn("plugin: refresh tools after list_changed", "server", c.name, "err", err)
			}
			return
		}
		if changed {
			// The next session pins its provider surface from this cache, so a
			// catalog that moved has to be what it reads, not the one this
			// session started from.
			c.saveHandshakeSchema(c.spec, tools)
			if onChanged != nil {
				onChanged(tools)
			}
		}
		// Clearing running under the same lock that reads the revision is what
		// keeps a notice arriving now from finding a cycle that is already
		// leaving and starting none of its own.
		c.refresh.mu.Lock()
		if c.refresh.revision == target {
			c.refresh.running = false
			settled = true
			c.refresh.mu.Unlock()
			return
		}
		c.refresh.mu.Unlock()
		delay = nextToolRefreshDelay(delay)
	}
}

func nextToolRefreshDelay(current time.Duration) time.Duration {
	if current <= 0 {
		return toolListRefreshDebounce
	}
	if current >= toolListRefreshBackoff/2 {
		return toolListRefreshBackoff
	}
	return current * 2
}

// toolListRefreshTimeout bounds one re-list by the shorter of this server's
// startup and call budgets. A re-list that outlives the budget to bring the
// server up in the first place is one the next notice can retry.
func (c *Client) toolListRefreshTimeout() time.Duration {
	call := c.callTimeout("tools/list", map[string]any{})
	if call <= 0 {
		call = defaultCallTimeout
	}
	if startup := c.spec.ResolvedStartupTimeout(); startup > 0 && startup < call {
		return startup
	}
	return call
}

// refreshToolCatalog re-reads the catalog and makes it current. The list is
// built before the swap, so a reader sees either the whole old catalog or the
// whole new one, and a failed read leaves the old one in place.
func (c *Client) refreshToolCatalog(ctx context.Context) ([]tool.Tool, bool, error) {
	out, err := c.listToolsRaw(ctx)
	if err != nil {
		return nil, false, err
	}
	out = slices.DeleteFunc(out, func(t mcpTool) bool { return !c.spec.ToolEnabled(t.Name) })
	if err := validateMCPToolNames(out); err != nil {
		return nil, false, fmt.Errorf("plugin %q: %w", c.name, err)
	}
	infos, tools := c.buildToolCatalog(out)
	fingerprint := toolCatalogFingerprint(infos, tools)

	c.toolsMu.Lock()
	changed := !c.toolsListed || fingerprint != c.toolsFingerprint
	if changed {
		c.tools = infos
		c.toolAdapters = append([]tool.Tool(nil), tools...)
		c.toolsFingerprint = fingerprint
		c.toolsListed = true
	}
	current := append([]tool.Tool(nil), c.toolAdapters...)
	c.toolsMu.Unlock()
	return current, changed, nil
}

// toolCatalogFingerprint covers everything a catalog publishes: the schemas the
// model is sent and the facts a status surface shows.
func toolCatalogFingerprint(infos []ToolInfo, tools []tool.Tool) [sha256.Size]byte {
	entries := struct {
		Infos   []ToolInfo        `json:"infos"`
		Schemas map[string]string `json:"schemas"`
	}{Infos: infos, Schemas: make(map[string]string, len(tools))}
	for _, t := range tools {
		entries.Schemas[t.Name()] = string(t.Schema())
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		// Unreachable for these types; a zero digest would read as "unchanged".
		return sha256.Sum256([]byte(fmt.Sprintf("%v", entries)))
	}
	return sha256.Sum256(encoded)
}

// toolListWatchers are the subscribers to every connected server's catalog
// changes, guarded by Host.mu.
type toolListWatchers struct {
	nextID uint64
	byID   map[uint64]func(Spec, []tool.Tool)
}

// SubscribeToolListChanges reports a server's refreshed tool set after it sends
// notifications/tools/list_changed. The returned function unsubscribes, and so
// does ctx ending. Startup catalogs are not replayed: the caller registered
// those when it connected the server.
func (h *Host) SubscribeToolListChanges(ctx context.Context, callback func(Spec, []tool.Tool)) func() {
	if h == nil || callback == nil {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return func() {}
	}
	if h.toolListChanges.byID == nil {
		h.toolListChanges.byID = map[uint64]func(Spec, []tool.Tool){}
	}
	h.toolListChanges.nextID++
	id := h.toolListChanges.nextID
	h.toolListChanges.byID[id] = callback
	h.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.toolListChanges.byID, id)
			h.mu.Unlock()
		})
	}
	stop := context.AfterFunc(ctx, unsubscribe)
	return func() {
		stop()
		unsubscribe()
	}
}

// bindToolListChanges starts watching one published client. Only a client this
// Host still holds publishes, so a replaced connection cannot re-register tools
// over the one that succeeded it.
func (h *Host) bindToolListChanges(c *Client) {
	if h == nil || c == nil {
		return
	}
	c.watchToolListChanges(func(tools []tool.Tool) {
		h.publishToolListChange(c, tools)
	})
}

func (h *Host) publishToolListChange(c *Client, tools []tool.Tool) {
	h.mu.RLock()
	if h.closed || h.lookupClientLocked(c.name) != c {
		h.mu.RUnlock()
		return
	}
	spec := c.spec
	callbacks := make([]func(Spec, []tool.Tool), 0, len(h.toolListChanges.byID))
	for _, callback := range h.toolListChanges.byID {
		callbacks = append(callbacks, callback)
	}
	h.mu.RUnlock()
	for _, callback := range callbacks {
		callback(spec, append([]tool.Tool(nil), tools...))
	}
}
