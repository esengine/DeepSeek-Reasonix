// Package providerext adapts extension-hosted sidecar providers into the
// host's provider.Resolver surface (Extension Protocol v2, stage 7). Each
// started sidecar holds its own provider credentials and runs streams; the
// host only ever sees the credential-free wire DTOs. The Resolver merges the
// base resolver's catalog with every sidecar's declared catalog and routes
// plugin-namespaced refs (plugin/<plugin>/<provider>/<model>) to the owning
// sidecar's Provider. It mirrors the Remote broker's host-side semantics
// (internal/remote/broker) exactly: 1-based contiguous seq buffering, a
// bounded delivery queue, a gap timer on stream end, cancellation through
// stream/cancel, and interruption via StreamInterruptedError. A selected
// sidecar's crash fails its streams — the adapter never falls back to a
// different provider for plugin refs.
package providerext

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/providerconv"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/provider"
)

// ProviderClient is the slice of a live sidecar connection the adapter needs.
// *sidecar.Client satisfies it; tests substitute fakes.
type ProviderClient interface {
	// PluginID returns the installed plugin package name this client serves.
	PluginID() string
	// Crashed reports whether the connection ended unexpectedly.
	Crashed() bool
	// Disconnected returns a channel closed when the connection's serve loop
	// ends for any reason — crash, orderly shutdown, or transport failure.
	Disconnected() <-chan struct{}
	// Handshake returns the sidecar's validated initialize result; its
	// Providers are the declaration of record for routing and conflicts.
	Handshake() protocol.InitializeResult
	// ProviderCatalog fetches the sidecar's full provider catalog.
	ProviderCatalog(ctx context.Context) ([]protocol.ProviderDescriptor, error)
	// ProviderStreamOpen opens one stream; chunks arrive as notifications.
	ProviderStreamOpen(ctx context.Context, params protocol.StreamOpenParams) (protocol.StreamOpenResult, error)
	// ProviderStreamCancel cancels one in-flight stream, best effort.
	ProviderStreamCancel(streamID string)
}

// ProviderConflict records one sidecar provider ref that collides with a base
// catalog entry without the plugin owning the provider:<ref> replacement slot.
type ProviderConflict struct {
	// Ref is the colliding provider ref (identical in both catalogs).
	Ref string
	// PluginID is the extension declaring Ref.
	PluginID string
	// Slot is the replacement slot the plugin must claim to override legally.
	Slot extension.Slot
}

// ConflictError fails a build whose sidecar providers collide with the base
// catalog without a manifest replacement claim. Boot treats it as fatal, the
// same class as sidecar.RequiredStartError.
type ConflictError struct {
	Conflicts []ProviderConflict
}

func (e *ConflictError) Error() string {
	lines := make([]string, 0, len(e.Conflicts))
	for _, c := range e.Conflicts {
		lines = append(lines, fmt.Sprintf(
			"plugin %q declares provider ref %q that the host provider catalog already serves; declare %q in the plugin manifest's runtime.replaces to override it",
			c.PluginID, c.Ref, string(c.Slot)))
	}
	return "extension provider conflict: " + strings.Join(lines, "; ")
}

// Resolver merges the base provider.Resolver with the extension sidecars'
// provider catalogs. It also implements sidecar.StreamRouter: the sidecar
// clients deliver inbound stream/chunk and stream/end notifications here, and
// the Resolver routes them by stream ID to the owning buffered stream.
type Resolver struct {
	base    provider.Resolver
	clients func() []ProviderClient
	owner   *extension.RuntimeOwner

	// replaced maps a base catalog ref to the plugin ID whose claimed
	// provider:<ref> slot lets its descriptor substitute the base entry.
	replaced map[string]string

	mu           sync.Mutex
	streams      map[string]*extensionStream
	catalogCache map[string][]provider.Descriptor
	catalogCalls map[string]*catalogCall
}

// catalogCall is one in-flight catalog fetch shared by every concurrent
// Catalog caller for the same plugin. The first caller owns the sidecar RPC;
// followers wait for done and then receive defensive copies of the same
// result. This prevents duplicate first-use RPCs and last-completion-wins
// cache contents when a sidecar catalog is dynamic.
type catalogCall struct {
	done        chan struct{}
	descriptors []provider.Descriptor
	ok          bool
}

var (
	_ provider.Resolver    = (*Resolver)(nil)
	_ sidecar.StreamRouter = (*Resolver)(nil)
	_ ProviderClient       = (*sidecar.Client)(nil)
)

// catalogFetchTimeout bounds one sidecar's extension/provider/catalog call,
// mirroring the broker host's catalog budget.
const catalogFetchTimeout = 10 * time.Second

// New builds the merged resolver. clients supplies the live sidecar
// connections (typically the sidecar Manager's client list); claims is the
// build's frozen replacement-slot ownership table (the kernel snapshot's
// Replacements). A sidecar ref colliding exactly with a base catalog ref is
// legal only when the plugin owns the provider:<ref> slot — then the sidecar
// descriptor replaces the base entry — and is a *ConflictError otherwise.
func New(base provider.Resolver, clients func() []ProviderClient, claims map[extension.Slot]extension.ContributionSource, owners ...*extension.RuntimeOwner) (*Resolver, error) {
	if base == nil {
		base = &provider.StaticResolver{}
	}
	if clients == nil {
		clients = func() []ProviderClient { return nil }
	}
	var owner *extension.RuntimeOwner
	if len(owners) > 0 && owners[0] != nil {
		owner = owners[0]
	}
	owner = extension.RuntimeOwnerOrDefault(owner)
	r := &Resolver{
		base:         base,
		clients:      clients,
		owner:        owner,
		replaced:     map[string]string{},
		streams:      make(map[string]*extensionStream),
		catalogCache: make(map[string][]provider.Descriptor),
		catalogCalls: make(map[string]*catalogCall),
	}
	baseRefs := map[string]bool{}
	for _, d := range base.Catalog() {
		baseRefs[d.Ref] = true
	}
	var conflicts []ProviderConflict
	for _, client := range clients() {
		prefix := "plugin/" + client.PluginID() + "/"
		for _, decl := range client.Handshake().Providers {
			ref := strings.TrimSpace(decl.Ref)
			if !strings.HasPrefix(ref, prefix) {
				// The handshake validation already enforces the namespace;
				// skip defensively rather than failing the build twice.
				continue
			}
			if !baseRefs[ref] {
				continue
			}
			slot := extension.SlotProviderRef(ref)
			owner, claimed := claims[slot]
			if !claimed || owner.PluginID != client.PluginID() {
				conflicts = append(conflicts, ProviderConflict{Ref: ref, PluginID: client.PluginID(), Slot: slot})
				continue
			}
			r.replaced[ref] = client.PluginID()
		}
	}
	if len(conflicts) > 0 {
		return nil, &ConflictError{Conflicts: conflicts}
	}
	return r, nil
}

// Catalog returns the base catalog minus claim-replaced entries plus every
// reachable sidecar's catalog. Sidecar catalogs are cached per client for the
// client's lifetime (its generation); a crashed sidecar's cache is dropped
// and its entries stop being offered. Fetch failures skip that sidecar for
// this call, mirroring the broker's best-effort catalog.
func (r *Resolver) Catalog() []provider.Descriptor {
	base := r.base.Catalog()
	out := make([]provider.Descriptor, 0, len(base))
	for _, d := range base {
		if _, replaced := r.replaced[d.Ref]; replaced {
			continue
		}
		out = append(out, d)
	}
	for _, client := range r.clients() {
		descriptors, ok := r.catalogFor(client)
		if !ok {
			continue
		}
		out = append(out, descriptors...)
	}
	return out
}

// catalogFor returns one sidecar's converted catalog, serving the per-client
// cache when warm. Entries outside the plugin's own namespace are skipped:
// the catalog RPC result must honor the same contract the handshake enforced.
func (r *Resolver) catalogFor(client ProviderClient) ([]provider.Descriptor, bool) {
	pluginID := client.PluginID()
	if client.Crashed() {
		r.mu.Lock()
		delete(r.catalogCache, pluginID)
		r.mu.Unlock()
		return nil, false
	}
	r.mu.Lock()
	if cached, ok := r.catalogCache[pluginID]; ok {
		out := append([]provider.Descriptor(nil), cached...)
		r.mu.Unlock()
		return out, true
	}
	if call := r.catalogCalls[pluginID]; call != nil {
		r.mu.Unlock()
		<-call.done
		return append([]provider.Descriptor(nil), call.descriptors...), call.ok
	}
	call := &catalogCall{done: make(chan struct{})}
	r.catalogCalls[pluginID] = call
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), catalogFetchTimeout)
	defer cancel()
	declared, err := client.ProviderCatalog(ctx)
	if err != nil {
		slog.Debug("providerext: sidecar catalog fetch failed", "plugin", pluginID, "err", err)
		r.finishCatalogCall(pluginID, call, nil, false)
		return nil, false
	}
	prefix := "plugin/" + pluginID + "/"
	out := make([]provider.Descriptor, 0, len(declared))
	for _, d := range declared {
		if !strings.HasPrefix(d.Ref, prefix) {
			slog.Debug("providerext: skipping catalog ref outside the plugin namespace", "plugin", pluginID, "ref", d.Ref)
			continue
		}
		out = append(out, providerconv.DescriptorFromProtocol(d))
	}
	ok := !client.Crashed()
	r.finishCatalogCall(pluginID, call, out, ok)
	return append([]provider.Descriptor(nil), out...), ok
}

// finishCatalogCall publishes one fetch atomically before waking followers.
// Only a live client's successful result enters the generation-local cache.
func (r *Resolver) finishCatalogCall(pluginID string, call *catalogCall, descriptors []provider.Descriptor, ok bool) {
	r.mu.Lock()
	call.descriptors = append([]provider.Descriptor(nil), descriptors...)
	call.ok = ok
	if ok {
		r.catalogCache[pluginID] = append([]provider.Descriptor(nil), descriptors...)
	}
	delete(r.catalogCalls, pluginID)
	close(call.done)
	r.mu.Unlock()
}

// Resolve routes a plugin-namespaced ref to the owning sidecar's Provider and
// everything else to the base resolver. A plugin ref whose plugin is not
// running, or that the plugin never declared, is an unknown-model-style
// error: the adapter NEVER falls back to a different provider for it.
func (r *Resolver) Resolve(selection provider.Selection) (provider.Provider, error) {
	ref := strings.TrimSpace(selection.Ref)
	if ref == "" {
		return nil, fmt.Errorf("provider selection ref is required")
	}
	pluginID := PluginRefOwner(ref)
	if pluginID == "" {
		return r.base.Resolve(selection)
	}
	client := r.liveClient(pluginID)
	if client == nil {
		return nil, fmt.Errorf("unknown provider ref %q: extension plugin %q is not running", ref, pluginID)
	}
	descriptor, ok := declaredDescriptor(client, ref)
	if !ok {
		return nil, fmt.Errorf("unknown provider ref %q: extension plugin %q does not declare it", ref, pluginID)
	}
	return &Provider{
		resolver:   r,
		client:     client,
		owner:      pluginID,
		ref:        descriptor.Ref,
		effort:     selection.Effort,
		descriptor: descriptor,
	}, nil
}

// liveClient returns the current backend for pluginID, or nil when the plugin
// is not registered. Crashed clients are still returned so Stream can surface
// StreamInterruptedError; only a missing plugin yields "not running".
func (r *Resolver) liveClient(pluginID string) ProviderClient {
	if r == nil || pluginID == "" {
		return nil
	}
	for _, client := range r.clients() {
		if client.PluginID() == pluginID {
			return client
		}
	}
	return nil
}

// PluginRefOwner extracts the plugin ID from a plugin-namespaced ref
// (plugin/<pluginID>/<rest...>). It re-exports the protocol package's
// canonical namespace helper; see protocol.PluginRefOwner.
func PluginRefOwner(ref string) string {
	return protocol.PluginRefOwner(ref)
}

// declaredDescriptor finds the plugin's handshake declaration for ref: an
// exact match, or the broker-style prefix form where ref names a provider and
// the declaration adds the model segment. The returned descriptor carries the
// full declared ref.
func declaredDescriptor(client ProviderClient, ref string) (provider.Descriptor, bool) {
	for _, decl := range client.Handshake().Providers {
		if decl.Ref == ref || strings.HasPrefix(decl.Ref, ref+"/") {
			return providerconv.DescriptorFromProtocol(decl), true
		}
	}
	return provider.Descriptor{}, false
}
