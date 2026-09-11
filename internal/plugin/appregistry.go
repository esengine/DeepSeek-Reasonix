package plugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"reasonix/internal/tool"
)

// AppInstance is one live MCP Apps surface: an unguessable token binding the
// Host, server, catalog generation, originating tool call, and the resource
// the App renders. Tokens are capability handles — possession alone authorizes
// nothing beyond reading that instance's identity; every App tool call still
// walks the full permission pipeline.
type AppInstance struct {
	Token       string
	Server      string
	Tool        string
	Generation  uint64
	CallID      string
	ResourceURI string

	resourceContent string
	resourceMIME    string
	resourceDigest  string
	resourceCSP     map[string][]string
	resourceBytes   int
	callCtx         context.Context
	cancelCalls     context.CancelFunc
}

// AppResourceSnapshot is the immutable resource bound to one live App
// instance. Desktop serves this copy instead of re-reading a mutable upstream
// resource after the instance has been authorized.
type AppResourceSnapshot struct {
	Content string
	MIME    string
	Digest  string
	CSP     map[string][]string
}

// appInstanceRegistry is the host's bounded set of live App instances. Max 32:
// beyond that the oldest instance is reclaimed, so a runaway App cannot pin
// memory. Server disconnect reclaims every instance of that server.
type appInstanceRegistry struct {
	mu        sync.Mutex
	instances map[string]*AppInstance
	order     []string
	bytes     int
}

const (
	maxAppInstances             = 32
	maxAppResourceSnapshotBytes = 4 << 20
	maxAppResourceRegistryBytes = 16 << 20
)

func newAppInstanceRegistry() *appInstanceRegistry {
	return &appInstanceRegistry{instances: map[string]*AppInstance{}}
}

func (r *appInstanceRegistry) newToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is fatal-grade; an App token must be unguessable.
		panic("plugin: app instance token entropy unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Register creates and stores a new instance, evicting the oldest when the
// registry is full.
func (r *appInstanceRegistry) Register(server, tool string, generation uint64, callID, resourceURI string) *AppInstance {
	r.mu.Lock()
	defer r.mu.Unlock()
	callCtx, cancelCalls := context.WithCancel(context.Background())
	inst := &AppInstance{
		Token: r.newToken(), Server: server, Tool: tool,
		Generation: generation, CallID: callID, ResourceURI: resourceURI,
		callCtx: callCtx, cancelCalls: cancelCalls,
	}
	r.instances[inst.Token] = inst
	r.order = append(r.order, inst.Token)
	for len(r.order) > maxAppInstances {
		r.releaseOldestLocked()
	}
	return cloneAppInstance(inst)
}

// Lookup resolves a token to its live instance.
func (r *appInstanceRegistry) Lookup(token string) (*AppInstance, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.instances[token]
	return cloneAppInstance(inst), ok
}

func cloneAppInstance(inst *AppInstance) *AppInstance {
	if inst == nil {
		return nil
	}
	copy := *inst
	copy.resourceCSP = cloneAppCSP(inst.resourceCSP)
	copy.callCtx = nil
	copy.cancelCalls = nil
	return &copy
}

func cloneAppCSP(csp map[string][]string) map[string][]string {
	if len(csp) == 0 {
		return nil
	}
	out := make(map[string][]string, len(csp))
	for directive, values := range csp {
		out[directive] = append([]string(nil), values...)
	}
	return out
}

func (r *appInstanceRegistry) releaseOldestLocked() {
	if len(r.order) == 0 {
		return
	}
	oldest := r.order[0]
	r.order = r.order[1:]
	if inst := r.instances[oldest]; inst != nil {
		r.bytes -= inst.resourceBytes
		inst.cancelCalls()
	}
	delete(r.instances, oldest)
}

// BindResource freezes the validated UI resource and CSP onto an instance.
// The registry has a process-memory budget in addition to its instance-count
// bound; oldest instances are reclaimed before the new snapshot is exposed.
func (r *appInstanceRegistry) BindResource(token, content, mime, digest string, csp map[string][]string) bool {
	resourceBytes := len(content) + appCSPBytes(csp)
	if resourceBytes > maxAppResourceSnapshotBytes {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.instances[token]
	if !ok {
		return false
	}
	r.bytes -= inst.resourceBytes
	inst.resourceContent = content
	inst.resourceMIME = mime
	inst.resourceDigest = digest
	inst.resourceCSP = cloneAppCSP(csp)
	inst.resourceBytes = resourceBytes
	r.bytes += resourceBytes
	for r.bytes > maxAppResourceRegistryBytes && len(r.order) > 1 {
		r.releaseOldestLocked()
	}
	_, ok = r.instances[token]
	return ok && r.bytes <= maxAppResourceRegistryBytes
}

func appCSPBytes(csp map[string][]string) int {
	size := 0
	for directive, values := range csp {
		size += len(directive)
		for _, value := range values {
			size += len(value)
		}
	}
	return size
}

func (r *appInstanceRegistry) Resource(token string) (AppResourceSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.instances[token]
	if !ok || inst.resourceDigest == "" {
		return AppResourceSnapshot{}, false
	}
	return AppResourceSnapshot{
		Content: inst.resourceContent,
		MIME:    inst.resourceMIME,
		Digest:  inst.resourceDigest,
		CSP:     cloneAppCSP(inst.resourceCSP),
	}, true
}

func (r *appInstanceRegistry) Context(token string) (context.Context, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.instances[token]
	if !ok || inst.callCtx == nil {
		return nil, false
	}
	return inst.callCtx, true
}

// Release drops one instance (tab closed, component unmounted).
func (r *appInstanceRegistry) Release(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.instances[token]; !ok {
		return
	}
	r.bytes -= r.instances[token].resourceBytes
	r.instances[token].cancelCalls()
	delete(r.instances, token)
	for i, t := range r.order {
		if t == token {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// ReleaseServer drops every instance of one server (disconnect path).
func (r *appInstanceRegistry) ReleaseServer(server string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for token, inst := range r.instances {
		if inst.Server == server {
			r.bytes -= inst.resourceBytes
			inst.cancelCalls()
			delete(r.instances, token)
		}
	}
	filtered := r.order[:0]
	for _, t := range r.order {
		if _, ok := r.instances[t]; ok {
			filtered = append(filtered, t)
		}
	}
	r.order = filtered
}

// Len reports the live instance count.
func (r *appInstanceRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.instances)
}

// RegisterAppInstance creates a live App instance on the host.
func (h *Host) RegisterAppInstance(server, tool string, generation uint64, callID, resourceURI string) *AppInstance {
	return h.appInstances.Register(server, tool, generation, callID, resourceURI)
}

// LookupAppInstance resolves a token against the host registry.
func (h *Host) LookupAppInstance(token string) (*AppInstance, bool) {
	return h.appInstances.Lookup(token)
}

// AppInstanceContext is cancelled when the App closes, is evicted, or its
// server disconnects. App-initiated calls use it as their lifetime owner.
func (h *Host) AppInstanceContext(token string) (context.Context, bool) {
	return h.appInstances.Context(token)
}

// BindAppResource freezes one validated resource onto the live instance.
func (h *Host) BindAppResource(token, content, mime, digest string, csp map[string][]string) bool {
	return h.appInstances.BindResource(token, content, mime, digest, csp)
}

// AppResource resolves the immutable resource snapshot for a live instance.
func (h *Host) AppResource(token string) (AppResourceSnapshot, bool) {
	return h.appInstances.Resource(token)
}

// AppInstanceResourceDescriptor validates that the originating tool still
// belongs to the same server/catalog generation and still declares the exact
// ui:// resource before Desktop reads or serves it.
func (h *Host) AppInstanceResourceDescriptor(token string) (map[string][]string, bool) {
	inst, ok := h.LookupAppInstance(token)
	if !ok {
		return nil, false
	}
	h.mu.RLock()
	var client *Client
	for _, c := range h.clients {
		if c.name == inst.Server && !c.closed.Load() {
			client = c
			break
		}
	}
	h.mu.RUnlock()
	if client == nil {
		return nil, false
	}
	if !client.appsNegotiated() {
		return nil, false
	}
	client.toolsMu.RLock()
	defer client.toolsMu.RUnlock()
	if client.toolCatalog.generation != inst.Generation || client.toolCatalogStale() {
		return nil, false
	}
	for _, candidates := range [][]tool.Tool{client.toolCatalog.adapters, client.toolCatalog.appAdapters} {
		for _, candidate := range candidates {
			rt, ok := candidate.(*remoteTool)
			if ok && rt.rawName == inst.Tool && rt.appCallable && rt.uiResourceURI == inst.ResourceURI {
				return cloneAppCSP(rt.uiCSP), true
			}
		}
	}
	return nil, false
}

// ReleaseAppInstance drops one instance.
func (h *Host) ReleaseAppInstance(token string) {
	h.appInstances.Release(token)
}

// AppInstanceTool resolves the App-callable tool an instance may invoke:
// same server, visibility includes "app", catalog generation unchanged.
func (h *Host) AppInstanceTool(token, rawToolName string) (toolRef, bool) {
	inst, ok := h.LookupAppInstance(token)
	if !ok {
		return toolRef{}, false
	}
	h.mu.RLock()
	var client *Client
	for _, c := range h.clients {
		if c.name == inst.Server && !c.closed.Load() {
			client = c
			break
		}
	}
	h.mu.RUnlock()
	if client == nil {
		return toolRef{}, false
	}
	client.toolsMu.RLock()
	defer client.toolsMu.RUnlock()
	if client.toolCatalog.generation != inst.Generation || client.toolCatalogStale() {
		return toolRef{}, false
	}
	for _, t := range client.toolCatalog.appAdapters {
		rt, ok := t.(*remoteTool)
		if ok && rt.rawName == rawToolName && rt.appCallable {
			return toolRef{server: inst.Server, tool: rt}, true
		}
	}
	return toolRef{}, false
}

type toolRef struct {
	server string
	tool   *remoteTool
}

// UITool exposes the App-callable tool for CSP assembly.
func (r toolRef) UITool() *remoteTool { return r.tool }

// ReadResourceForApp reads one ui resource for the Apps channel, returning the
// text content, declared mime type, and resource-level Apps CSP metadata.
func (h *Host) ReadResourceForApp(ctx context.Context, server, uri string) (string, string, map[string][]string, error) {
	h.mu.RLock()
	var client *Client
	for _, c := range h.clients {
		if c.name == server && !c.closed.Load() {
			client = c
			break
		}
	}
	h.mu.RUnlock()
	if client == nil {
		return "", "", nil, fmt.Errorf("server %q not connected", server)
	}
	if !client.appsNegotiated() {
		return "", "", nil, fmt.Errorf("server %q did not negotiate the MCP Apps extension", server)
	}
	return client.readResourceWithMime(ctx, uri)
}

// readResourceWithMime reads one resource and returns its text, mime type, and
// the 2026 Apps resource `_meta.ui.csp` (flat `ui/csp` is accepted too).
func (c *Client) readResourceWithMime(ctx context.Context, uri string) (string, string, map[string][]string, error) {
	res, err := c.call(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return "", "", nil, err
	}
	var wire struct {
		Contents []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Meta     struct {
				UI *struct {
					CSP map[string][]string `json:"csp,omitempty"`
				} `json:"ui,omitempty"`
				FlatCSP map[string][]string `json:"ui/csp,omitempty"`
			} `json:"_meta,omitempty"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(res, &wire); err != nil {
		return "", "", nil, err
	}
	if len(wire.Contents) == 0 {
		return "", "", nil, fmt.Errorf("resource %q returned no contents", uri)
	}
	first := wire.Contents[0]
	if first.URI != "" && first.URI != uri {
		return "", "", nil, fmt.Errorf("resource %q returned mismatched URI %q", uri, first.URI)
	}
	csp := first.Meta.FlatCSP
	if first.Meta.UI != nil && len(first.Meta.UI.CSP) > 0 {
		csp = first.Meta.UI.CSP
	}
	return first.Text, first.MimeType, cloneAppCSP(csp), nil
}
