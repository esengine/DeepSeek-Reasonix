package host

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"reasonix/internal/remote/protocol"
)

// LeaseBinding is the daemon-private identity assigned to one attach
// transport. Generation is deliberately not part of the Remote wire contract:
// it lets a resumed client replace an older transport without transferring
// ownership of any workspace or Session runtime.
type LeaseBinding struct {
	ClientInstanceID protocol.ClientInstanceID
	LeaseID          protocol.LeaseID
	Generation       uint64
}

// LeaseGrant is returned after initialize has acquired or resumed the sole
// Host lease.
type LeaseGrant struct {
	Binding      LeaseBinding
	TTL          time.Duration
	PingInterval time.Duration
	Resumed      bool
}

type leaseState struct {
	binding   LeaseBinding
	expiresAt time.Time
}

// LeaseManagerOptions exists so the state machine can be tested without wall
// clock sleeps or predictable production identifiers.
type LeaseManagerOptions struct {
	Now          func() time.Time
	NewLeaseID   func() (protocol.LeaseID, error)
	TTL          time.Duration
	PingInterval time.Duration
}

// LeaseManager owns the daemon's one client-control lease. It never owns or
// cancels runtime work; expiration and detach only clear client control.
type LeaseManager struct {
	mu           sync.Mutex
	now          func() time.Time
	newLeaseID   func() (protocol.LeaseID, error)
	ttl          time.Duration
	pingInterval time.Duration
	current      *leaseState
	issued       map[protocol.LeaseID]struct{}
}

func NewLeaseManager(opts LeaseManagerOptions) *LeaseManager {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	newLeaseID := opts.NewLeaseID
	if newLeaseID == nil {
		newLeaseID = randomLeaseID
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = time.Duration(protocol.LeaseTTLMillis) * time.Millisecond
	}
	pingInterval := opts.PingInterval
	if pingInterval <= 0 {
		pingInterval = time.Duration(protocol.LeasePingIntervalMillis) * time.Millisecond
	}
	return &LeaseManager{
		now:          now,
		newLeaseID:   newLeaseID,
		ttl:          ttl,
		pingInterval: pingInterval,
		issued:       make(map[protocol.LeaseID]struct{}),
	}
}

// Acquire grants an idle Host lease or resumes the exact
// clientInstanceId+leaseId pair. A successful resume increments the internal
// connection generation, making all late requests from the old transport
// stale. An invalid old resumeLeaseId is ignored only when the Host is idle.
func (m *LeaseManager) Acquire(clientID protocol.ClientInstanceID, resumeLeaseID protocol.LeaseID) (LeaseGrant, error) {
	if strings.TrimSpace(string(clientID)) == "" {
		return LeaseGrant{}, invalidLeaseArgument("clientInstanceId is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.expireLocked(now)

	if m.current != nil {
		current := m.current
		if current.binding.ClientInstanceID == clientID && resumeLeaseID != "" && current.binding.LeaseID == resumeLeaseID {
			current.binding.Generation++
			current.expiresAt = now.Add(m.ttl)
			return m.grantLocked(current.binding, true), nil
		}
		remaining := current.expiresAt.Sub(now).Milliseconds()
		if remaining < 0 {
			remaining = 0
		}
		return LeaseGrant{}, protocol.MustRemoteError(protocol.ErrHostBusy, protocol.ErrorOptions{RetryAfterMs: &remaining})
	}

	leaseID, err := m.nextLeaseIDLocked()
	if err != nil {
		return LeaseGrant{}, err
	}
	binding := LeaseBinding{ClientInstanceID: clientID, LeaseID: leaseID, Generation: 1}
	m.current = &leaseState{binding: binding, expiresAt: now.Add(m.ttl)}
	return m.grantLocked(binding, false), nil
}

func (m *LeaseManager) nextLeaseIDLocked() (protocol.LeaseID, error) {
	for attempt := 0; attempt < 8; attempt++ {
		leaseID, err := m.newLeaseID()
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(string(leaseID)) == "" {
			return "", invalidLeaseArgument("generated leaseId is empty")
		}
		if _, issued := m.issued[leaseID]; issued {
			continue
		}
		// Lease identities are never recycled during a daemon lifetime. A
		// detached or expired binding can therefore never match a fresh grant
		// whose connection generation has reset to one.
		m.issued[leaseID] = struct{}{}
		return leaseID, nil
	}
	return "", invalidLeaseArgument("generated leaseId was already issued")
}

// Validate checks that binding still controls the Host. When renew is true, a
// valid inbound request refreshes the fixed TTL. A lease mismatch is distinct
// from an old transport generation so clients can choose reconnect vs replace.
func (m *LeaseManager) Validate(binding LeaseBinding, renew bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.expireLocked(now)
	if err := m.validateLocked(binding); err != nil {
		return err
	}
	if renew {
		m.current.expiresAt = now.Add(m.ttl)
	}
	return nil
}

// Ping validates both the transport binding and the explicit wire leaseId,
// then renews the TTL and returns its fixed duration.
func (m *LeaseManager) Ping(binding LeaseBinding, leaseID protocol.LeaseID) (time.Duration, error) {
	if leaseID != binding.LeaseID {
		return 0, protocol.MustRemoteError(protocol.ErrLeaseNotHeld, protocol.ErrorOptions{})
	}
	if err := m.Validate(binding, true); err != nil {
		return 0, err
	}
	return m.ttl, nil
}

// Detach releases only the exact current transport's lease. Callers must write
// the successful RPC response before invoking this method.
func (m *LeaseManager) Detach(binding LeaseBinding, leaseID protocol.LeaseID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.expireLocked(now)
	if leaseID != binding.LeaseID {
		return protocol.MustRemoteError(protocol.ErrLeaseNotHeld, protocol.ErrorOptions{})
	}
	if err := m.validateLocked(binding); err != nil {
		return err
	}
	m.current = nil
	return nil
}

// Held reports whether a non-expired lease currently exists. It is diagnostic
// state only and does not expose the holder identity.
func (m *LeaseManager) Held() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(m.now())
	return m.current != nil
}

func (m *LeaseManager) grantLocked(binding LeaseBinding, resumed bool) LeaseGrant {
	return LeaseGrant{Binding: binding, TTL: m.ttl, PingInterval: m.pingInterval, Resumed: resumed}
}

func (m *LeaseManager) validateLocked(binding LeaseBinding) error {
	if m.current == nil || binding.LeaseID == "" || binding.ClientInstanceID == "" ||
		m.current.binding.LeaseID != binding.LeaseID || m.current.binding.ClientInstanceID != binding.ClientInstanceID {
		return protocol.MustRemoteError(protocol.ErrLeaseNotHeld, protocol.ErrorOptions{})
	}
	if m.current.binding.Generation != binding.Generation {
		return protocol.MustRemoteError(protocol.ErrStaleConnection, protocol.ErrorOptions{})
	}
	return nil
}

func (m *LeaseManager) expireLocked(now time.Time) {
	if m.current != nil && !now.Before(m.current.expiresAt) {
		m.current = nil
	}
}

func randomLeaseID() (protocol.LeaseID, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return protocol.LeaseID("lease_" + hex.EncodeToString(bytes[:])), nil
}

func invalidLeaseArgument(message string) error {
	// Initialize DTO validation normally rejects this before the lease layer.
	// Keeping a typed local error protects direct callers and tests without
	// misclassifying malformed input as a domain state transition.
	return &leaseArgumentError{message: message}
}

type leaseArgumentError struct{ message string }

func (e *leaseArgumentError) Error() string { return e.message }
