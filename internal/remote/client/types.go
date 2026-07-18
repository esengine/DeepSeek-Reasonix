// Package client implements the transport-neutral Reasonix Remote V1 client.
// It owns the Remote connection handshake, lease heartbeat, strict wire
// decoding, subscription ordering, and contentRef reconstruction. SSH process
// management and Desktop state intentionally live above this package.
package client

import (
	"context"
	"io"
	"time"

	"reasonix/internal/remote/protocol"
)

// Transport is one already-authenticated, bidirectional Remote byte stream.
// A Desktop SSH adapter normally backs it with reasonix remote attach --stdio.
type Transport interface {
	io.Reader
	io.Writer
	io.Closer
}

// TransportFactory opens one fresh transport. Implementations may start SSH,
// use an in-memory test peer, or connect through another platform adapter; the
// Remote client itself never imports os/exec or assumes SSH.
type TransportFactory interface {
	Open(context.Context) (Transport, error)
}

// TransportFactoryFunc adapts a function to TransportFactory.
type TransportFactoryFunc func(context.Context) (Transport, error)

func (f TransportFactoryFunc) Open(ctx context.Context) (Transport, error) { return f(ctx) }

// Options are immutable for the lifetime of one saved Host entry.
type Options struct {
	Factory          TransportFactory
	BuildID          protocol.BuildID
	ClientInstanceID protocol.ClientInstanceID
	ResumeLeaseID    protocol.LeaseID
}

// ConnectionState is the protocol client's local transport state. Target
// switching and Local/Remote product states belong to Desktop TargetManager.
type ConnectionState string

const (
	StateDisconnected ConnectionState = "disconnected"
	StateConnecting   ConnectionState = "connecting"
	StateReconnecting ConnectionState = "reconnecting"
	StateConnected    ConnectionState = "connected"
	StateClosed       ConnectionState = "closed"
)

// Connection is the immutable result of a successful initialize handshake.
type Connection struct {
	Generation uint64
	Initialize protocol.InitializeResult
}

// Status is a race-safe snapshot of current connection identity. LeaseID is
// exposed so the Host-entry persistence layer can save it after Connect.
type Status struct {
	State        ConnectionState
	Generation   uint64
	HostEpoch    protocol.HostEpoch
	LeaseID      protocol.LeaseID
	Host         protocol.HostInfo
	Capabilities protocol.Capabilities
}

// SubscriptionRecovery is the minimum state needed to obtain a new atomic
// snapshot after transport loss. Subscription IDs are transport-scoped and
// deliberately absent: reconnect subscribe must not send replaceSubscriptionId.
type SubscriptionRecovery struct {
	Target               protocol.RuntimeTarget
	PageTurns            int
	PreviousRuntimeEpoch protocol.RuntimeEpoch
	LastSeq              uint64
}

// RecoveryState retains the lease continuation identity and the Session
// targets that need a fresh subscribe after reconnect.
type RecoveryState struct {
	ResumeLeaseID protocol.LeaseID
	HostEpoch     protocol.HostEpoch
	Generation    uint64
	Subscriptions []SubscriptionRecovery
}

// ConnectionFault reports terminal loss of the current local transport. It is
// not a daemon RemoteError and must be presented as a connection failure by the
// caller.
type ConnectionFault struct {
	Generation uint64
	Err        error
}

// CatalogNotification is already strictly decoded and belongs to Generation.
type CatalogNotification struct {
	Generation uint64
	Change     protocol.CatalogChanged
}

// Subscription is an atomic snapshot plus the ordered N+1 notification
// stream. Updates cannot be observed by the caller before this value (and thus
// Snapshot) is returned.
type Subscription struct {
	ID         protocol.SubscriptionID
	Generation uint64
	Snapshot   protocol.SessionSnapshot
	Updates    <-chan SubscriptionUpdate
}

// SubscriptionUpdate is exactly one rehydrated event, one Host-requested
// resync, or one local ordering/content fault requiring a fresh snapshot.
type SubscriptionUpdate struct {
	Event            *protocol.SessionEvent
	Resync           *protocol.SessionResyncRequired
	Err              error
	SnapshotRequired bool
}

// ticker is private so tests can advance the fixed ten-second heartbeat
// without making the frozen interval configurable in production.
type ticker interface {
	C() <-chan time.Time
	Stop()
}

type realTicker struct{ *time.Ticker }

func (t realTicker) C() <-chan time.Time { return t.Ticker.C }
