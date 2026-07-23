// Package target implements TargetManager: Local + at most one Remote adapter
// with a single active projection for the main workbench.
package target

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Kind classifies the active workbench projection.
type Kind string

const (
	KindLocal  Kind = "local"
	KindRemote Kind = "ssh"
)

// Identity is a stable key for last-click-wins fencing.
type Identity struct {
	Kind      Kind
	HostID    string // empty for local
	Workspace string // empty for local
}

func (i Identity) String() string {
	if i.Kind != KindRemote {
		return "local"
	}
	return string(i.Kind) + ":" + i.HostID + ":" + i.Workspace
}

// RemoteHint is the last connected remote shown after Desktop restart (no auto-connect).
type RemoteHint struct {
	HostID    string
	Workspace string
	Label     string
}

// Manager maintains Local always-on, optional Remote, and one active projection.
type Manager struct {
	mu sync.Mutex

	active      Identity
	identityGen atomic.Uint64
	attachGen   atomic.Uint64
	requestSeq  atomic.Uint64
	remote      *RemoteState
	connecting  *RemoteState
	lastRemote  RemoteHint
	// busyRemote is true when the remote adapter is running a turn/mutation.
	busyRemote bool
}

// RemoteState is the background remote adapter lifecycle (may be inactive projection).
type RemoteState struct {
	Identity Identity
	// Connected is true while the SSH/rpcwire channel is live.
	Connected bool
	// Generation is the attach generation; responses for older gens are dropped.
	Generation uint64
}

// New returns a Manager that always starts on Local.
func New() *Manager {
	m := &Manager{active: Identity{Kind: KindLocal}}
	m.identityGen.Store(1)
	return m
}

// Active returns the current projection identity and fencing tokens.
func (m *Manager) Active() (Identity, uint64, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active, m.identityGen.Load(), m.requestSeq.Load()
}

// Connecting reports whether a candidate Remote adapter is being prepared.
// The committed active identity remains authoritative until activation, but
// callers can use this bit to fence mutations during the transition.
func (m *Manager) Connecting() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connecting != nil
}

// LastRemoteHint is the reconnect entry shown after restart (never auto-SSH).
func (m *Manager) LastRemoteHint() RemoteHint {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRemote
}

// RememberRemote updates the post-restart reconnect hint only.
func (m *Manager) RememberRemote(hint RemoteHint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastRemote = hint
}

// SwitchLocal projects the permanent local adapter. Returns new fencing tokens.
func (m *Manager) SwitchLocal() (Identity, uint64, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = Identity{Kind: KindLocal}
	gen := m.identityGen.Add(1)
	seq := m.requestSeq.Add(1)
	return m.active, gen, seq
}

// BeginRemoteConnect records a candidate connection without replacing the
// committed Remote adapter. Returns error if another remote is busy or a
// candidate is already being connected.
func (m *Manager) BeginRemoteConnect(hostID, workspace string) (Identity, uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connecting != nil {
		return Identity{}, 0, fmt.Errorf("remote connection already in progress")
	}
	if m.remote != nil && m.busyRemote {
		return Identity{}, 0, fmt.Errorf("remote target is busy; cancel the turn before switching hosts")
	}
	id := Identity{Kind: KindRemote, HostID: hostID, Workspace: workspace}
	gen := m.attachGen.Add(1)
	m.connecting = &RemoteState{Identity: id, Connected: false, Generation: gen}
	return id, gen, nil
}

// MarkRemoteConnected marks the remote adapter online for the given generation.
func (m *Manager) MarkRemoteConnected(gen uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connecting == nil || m.connecting.Generation != gen {
		return fmt.Errorf("stale remote generation")
	}
	m.connecting.Connected = true
	return nil
}

// MarkRemoteDisconnected fences a dead transport, clears the remote busy bit,
// and returns the main window to Local without discarding the reconnect hint.
// Late disconnect callbacks from an older connection are ignored.
func (m *Manager) MarkRemoteDisconnected(gen uint64) (Identity, uint64, uint64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.remote == nil || m.remote.Generation != gen || !m.remote.Connected {
		return m.active, m.identityGen.Load(), m.requestSeq.Load(), false
	}
	m.remote.Connected = false
	m.busyRemote = false
	if m.active.Kind == KindRemote {
		m.active = Identity{Kind: KindLocal}
		return m.active, m.identityGen.Add(1), m.requestSeq.Add(1), true
	}
	return m.active, m.identityGen.Load(), m.requestSeq.Load(), true
}

// Remote returns a copy of the current remote lifecycle, including its attach
// generation. Projection identity generations deliberately advance again on
// ActivateRemote, so callers must compare a remote client with this token—not
// with Active's projection token.
func (m *Manager) Remote() *RemoteState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.remote == nil {
		return nil
	}
	cp := *m.remote
	return &cp
}

// AbortRemoteConnect clears only the generation that failed. It is a no-op for
// a newer replacement, preventing a late failure from tearing down the winner.
func (m *Manager) AbortRemoteConnect(gen uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connecting == nil || m.connecting.Generation != gen {
		return false
	}
	m.connecting = nil
	return true
}

// ActivateRemote projects the connected remote. Drops stale if gen mismatches.
func (m *Manager) ActivateRemote(gen uint64) (Identity, uint64, uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connecting != nil && m.connecting.Generation == gen && m.connecting.Connected {
		m.remote = m.connecting
		m.connecting = nil
		m.lastRemote = RemoteHint{HostID: m.remote.Identity.HostID, Workspace: m.remote.Identity.Workspace}
	} else if m.remote == nil || m.remote.Generation != gen || !m.remote.Connected {
		return Identity{}, 0, 0, fmt.Errorf("remote not connected")
	}
	m.active = m.remote.Identity
	idGen := m.identityGen.Add(1)
	seq := m.requestSeq.Add(1)
	return m.active, idGen, seq, nil
}

// SetRemoteBusy records whether the remote adapter is mid-turn (blocks host swap).
func (m *Manager) SetRemoteBusy(busy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.busyRemote = busy
}

// DetachRemote clears the remote adapter when idle. Busy remote refuses detach.
func (m *Manager) DetachRemote() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busyRemote {
		return fmt.Errorf("cannot disconnect while a remote turn is running")
	}
	if m.active.Kind == KindRemote {
		m.active = Identity{Kind: KindLocal}
		m.identityGen.Add(1)
		m.requestSeq.Add(1)
	}
	m.remote = nil
	m.connecting = nil
	return nil
}

// IsStale reports whether a completion for (identityGen, requestSeq) is obsolete.
func (m *Manager) IsStale(identityGen, requestSeq uint64) bool {
	return identityGen != m.identityGen.Load() || requestSeq != m.requestSeq.Load()
}

// RemoteBackground returns the non-active remote if it is still connected.
func (m *Manager) RemoteBackground() *RemoteState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.remote == nil {
		return nil
	}
	if m.active.Kind == KindRemote && m.active.String() == m.remote.Identity.String() {
		return nil
	}
	cp := *m.remote
	return &cp
}
