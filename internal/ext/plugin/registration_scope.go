package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// HostClientRef identifies one live Client instance on a Host. Desktop
// generation-rollback uses RemoveIfInstance so a lost build cannot tear down
// sibling or newer-generation connections that only share a server name.
type HostClientRef struct {
	Name string
	ID   uint64
}

// ErrRegistrationScopeAborted is returned when a connection completes after
// its owning build scope was aborted (generation loss / superseded build).
var ErrRegistrationScopeAborted = errors.New("plugin: registration scope aborted")

type registrationScopeKey struct{}

type registrationScopeState uint8

const (
	registrationScopeActive registrationScopeState = iota
	registrationScopeCommitted
	registrationScopeAborted
)

type registrationRecordState uint8

const (
	registrationRecordRejected registrationRecordState = iota
	registrationRecordActive
	registrationRecordCommitted
)

// RegistrationScope is a per-build ownership token for Host client
// registrations. Only connections that carry this scope via context are
// attributed to the build; sibling hot-adds omit it. Scopes do not serialize
// Host mutations. Abort rejects late LazyToolset registrations.
type RegistrationScope struct {
	host *Host
	id   uint64

	mu    sync.Mutex
	refs  []HostClientRef
	state registrationScopeState
}

// BeginRegistrationScope creates an independent ownership token for one
// controller build. Callers must propagate it with ContextWithRegistrationScope
// on both synchronous and asynchronous MCP connection paths.
func (h *Host) BeginRegistrationScope() *RegistrationScope {
	if h == nil {
		return &RegistrationScope{}
	}
	return &RegistrationScope{
		host: h,
		id:   h.nextScopeID.Add(1),
	}
}

// ContextWithRegistrationScope attaches scope to ctx for EnsureConnected /
// LazyToolset / ReplaceServerBackend ownership attribution.
func ContextWithRegistrationScope(ctx context.Context, scope *RegistrationScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if scope == nil {
		return ctx
	}
	return context.WithValue(ctx, registrationScopeKey{}, scope)
}

// RegistrationScopeFromContext returns the build scope on ctx, if any.
func RegistrationScopeFromContext(ctx context.Context) *RegistrationScope {
	if ctx == nil {
		return nil
	}
	scope, _ := ctx.Value(registrationScopeKey{}).(*RegistrationScope)
	return scope
}

// ID returns the Host-local scope identifier (0 when Host was nil).
func (s *RegistrationScope) ID() uint64 {
	if s == nil {
		return 0
	}
	return s.id
}

// Aborted reports whether AbortAndRollback has been called.
func (s *RegistrationScope) Aborted() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == registrationScopeAborted
}

// Committed reports whether the owning controller build was published. Late
// LazyToolset connections are accepted after commit and become Host-owned
// immediately; abort is terminal only for scopes that never published.
func (s *RegistrationScope) Committed() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == registrationScopeCommitted
}

// Snapshot returns the client instances attributed to this scope.
func (s *RegistrationScope) Snapshot() []HostClientRef {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]HostClientRef(nil), s.refs...)
}

// record appends an active claim, reports that a published scope should commit
// the instance immediately, or rejects a late registration after abort.
func (s *RegistrationScope) record(ref HostClientRef) registrationRecordState {
	if s == nil {
		return registrationRecordCommitted
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state {
	case registrationScopeAborted:
		return registrationRecordRejected
	case registrationScopeCommitted:
		return registrationRecordCommitted
	}
	for _, existing := range s.refs {
		if existing.ID == ref.ID {
			return registrationRecordActive
		}
	}
	s.refs = append(s.refs, ref)
	return registrationRecordActive
}

// Commit publishes every instance used by this build into Host ownership.
// It returns false only when the scope was already aborted. Commit is
// idempotent, and late registrations on a committed scope are committed by
// noteClientLocked/claimClientFromContext as they arrive.
func (s *RegistrationScope) Commit() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	switch s.state {
	case registrationScopeAborted:
		s.mu.Unlock()
		return false
	case registrationScopeCommitted:
		s.mu.Unlock()
		return true
	}
	s.state = registrationScopeCommitted
	refs := append([]HostClientRef(nil), s.refs...)
	s.refs = nil
	s.mu.Unlock()
	if s.host != nil {
		s.host.commitRegistration(s.id, refs)
	}
	return true
}

// AbortAndRollback marks the scope aborted (rejecting late registrations) and
// removes every instance previously recorded under this scope.
func (s *RegistrationScope) AbortAndRollback() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.state != registrationScopeActive {
		s.mu.Unlock()
		return
	}
	s.state = registrationScopeAborted
	refs := append([]HostClientRef(nil), s.refs...)
	s.refs = nil
	s.mu.Unlock()
	if s.host != nil {
		s.host.rollbackRegistration(s.id, refs)
	}
}

// noteClientLocked assigns an instance ID and records ownership on scope.
// Caller holds h.mu. Aborted scopes return ErrRegistrationScopeAborted and do
// not leave c in h.clients (caller closes c).
func (h *Host) noteClientLocked(c *Client, scope *RegistrationScope) error {
	if c == nil {
		return nil
	}
	if c.instanceID == 0 {
		c.instanceID = h.nextInstanceID.Add(1)
	}
	// Append first, then record. Aborted scopes unpublish immediately. Clients
	// registered outside a build scope are already Host-owned.
	h.clients = append(h.clients, c)
	if scope == nil {
		c.registrationCommitted = true
		return nil
	}
	switch scope.record(HostClientRef{Name: c.name, ID: c.instanceID}) {
	case registrationRecordRejected:
		h.clients = h.clients[:len(h.clients)-1]
		return ErrRegistrationScopeAborted
	case registrationRecordCommitted:
		c.registrationCommitted = true
	case registrationRecordActive:
		if c.registrationClaims == nil {
			c.registrationClaims = make(map[uint64]struct{})
		}
		c.registrationClaims[scope.id] = struct{}{}
	}
	return nil
}

// noteClientFromContext is noteClientLocked using the scope on ctx.
func (h *Host) noteClientFromContext(ctx context.Context, c *Client) error {
	return h.noteClientLocked(c, RegistrationScopeFromContext(ctx))
}

// claimClientFromContext attributes reuse of an existing exact instance to the
// current build. The instance is revalidated under Host.mu so a concurrent
// replace/remove cannot turn a pre-check into a stale claim.
func (h *Host) claimClientFromContext(ctx context.Context, c *Client) error {
	if h == nil || c == nil {
		return errors.New("plugin: client is unavailable")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return errors.New("plugin host is closed")
	}
	if live := h.lookupClientLocked(c.name); live != c {
		return fmt.Errorf("client %q changed while being claimed", c.name)
	}
	scope := RegistrationScopeFromContext(ctx)
	if scope == nil {
		return nil
	}
	switch scope.record(HostClientRef{Name: c.name, ID: c.instanceID}) {
	case registrationRecordRejected:
		return ErrRegistrationScopeAborted
	case registrationRecordCommitted:
		c.registrationCommitted = true
	case registrationRecordActive:
		if c.registrationClaims == nil {
			c.registrationClaims = make(map[uint64]struct{})
		}
		c.registrationClaims[scope.id] = struct{}{}
	}
	return nil
}

// RemoveIfInstance disconnects name only when the live client instance ID still
// matches. Returns whether a matching client was removed.
func (h *Host) RemoveIfInstance(name string, instanceID uint64) bool {
	if h == nil || instanceID == 0 {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	h.mu.Lock()
	idx := -1
	var removed *Client
	for i, c := range h.clients {
		if c != nil && c.name == name && c.instanceID == instanceID {
			idx = i
			removed = c
			break
		}
	}
	if idx < 0 || removed == nil {
		h.mu.Unlock()
		return false
	}
	removed = h.removeClientAtLocked(idx)
	h.mu.Unlock()
	removed.close()
	return true
}

func (h *Host) commitRegistration(scopeID uint64, refs []HostClientRef) {
	if h == nil || scopeID == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ref := range refs {
		_, client := h.findClientInstanceLocked(ref.Name, ref.ID)
		if client == nil {
			continue
		}
		delete(client.registrationClaims, scopeID)
		client.registrationCommitted = true
	}
}

func (h *Host) rollbackRegistration(scopeID uint64, refs []HostClientRef) {
	if h == nil || scopeID == 0 {
		return
	}
	h.mu.Lock()
	removed := make([]*Client, 0, len(refs))
	for _, ref := range refs {
		idx, client := h.findClientInstanceLocked(ref.Name, ref.ID)
		if client == nil {
			continue
		}
		delete(client.registrationClaims, scopeID)
		if client.registrationCommitted || len(client.registrationClaims) > 0 {
			continue
		}
		if removedClient := h.removeClientAtLocked(idx); removedClient != nil {
			removed = append(removed, removedClient)
		}
	}
	h.mu.Unlock()
	for _, client := range removed {
		client.close()
	}
}

func (h *Host) findClientInstanceLocked(name string, instanceID uint64) (int, *Client) {
	for i, client := range h.clients {
		if client != nil && client.name == name && client.instanceID == instanceID {
			return i, client
		}
	}
	return -1, nil
}

// removeClientAtLocked removes one exact live instance without touching
// name-wide deferred startup generations. The owning build context cancels its
// own lazy work; canceling every entry for the name would affect newer builds.
func (h *Host) removeClientAtLocked(idx int) *Client {
	if idx < 0 || idx >= len(h.clients) {
		return nil
	}
	removed := h.clients[idx]
	h.clients = append(h.clients[:idx], h.clients[idx+1:]...)
	if h.proxies != nil {
		if proxy := h.proxies[removed.name]; proxy != nil {
			proxy.detachIf(removed)
		}
	}
	keptPrompts := h.prompts[:0]
	for _, prompt := range h.prompts {
		if prompt.Server != removed.name {
			keptPrompts = append(keptPrompts, prompt)
		}
	}
	h.prompts = keptPrompts
	keptResources := h.resources[:0]
	for _, resource := range h.resources {
		if resource.Server != removed.name {
			keptResources = append(keptResources, resource)
		}
	}
	h.resources = keptResources
	h.clearFailure(removed.name)
	return removed
}

// RollbackRegistration removes every journaled client instance. Safe when a
// ref is already gone (RemoveIfInstance is a no-op).
func (h *Host) RollbackRegistration(refs []HostClientRef) {
	if h == nil {
		return
	}
	for _, ref := range refs {
		h.RemoveIfInstance(ref.Name, ref.ID)
	}
}
