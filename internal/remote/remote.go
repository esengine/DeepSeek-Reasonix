// Package remote is the SSH transport for Reasonix's remote module: host
// resolution ([remote] config + ~/.ssh/config), authentication, host-key
// verification (system known_hosts read-only + a Reasonix-managed TOFU file),
// a supervised connection with keepalive and exponential-backoff reconnect,
// shared SFTP access, and port-forward lifecycle. The agent itself never runs
// through this package — remote workspaces are driven by a `reasonix serve`
// process bootstrapped on the remote host (internal/remote/bootstrap) and
// reached through a forwarded loopback port.
//
// The package is frontend-agnostic: all interactivity flows through callbacks
// (HostKeyPrompt, SecretPrompt) and status subscriptions, so the CLI, chat
// TUI, and the Wails desktop consume the same surface.
package remote

import (
	"errors"
	"strings"
	"time"
)

// Status is the supervised connection state.
type Status int

const (
	// StatusIdle: created, Start not yet called.
	StatusIdle Status = iota
	// StatusConnecting: first dial in progress.
	StatusConnecting
	// StatusConnected: SSH established, forwards attached.
	StatusConnected
	// StatusReconnecting: connection lost, supervisor is backing off/redialing.
	StatusReconnecting
	// StatusDegraded: connected, but at least one forward failed to attach.
	StatusDegraded
	// StatusStopped: Close was called, the context ended, or auth became
	// unrecoverable. Terminal.
	StatusStopped
)

func (s Status) String() string {
	switch s {
	case StatusIdle:
		return "idle"
	case StatusConnecting:
		return "connecting"
	case StatusConnected:
		return "connected"
	case StatusReconnecting:
		return "reconnecting"
	case StatusDegraded:
		return "degraded"
	case StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// StatusEvent is one supervisor state transition, delivered to subscribers
// and returned by Client.Status.
type StatusEvent struct {
	Host    string // configured host name (or user@host target)
	Status  Status
	Attempt int   // reconnect attempt counter; 0 on the first connect
	Err     error // last error for Reconnecting/Degraded/Stopped; nil otherwise
	At      time.Time
}

// Typed errors surfaced by dial/auth/host-key verification and the client.
var (
	// ErrNotConnected: the client is not currently connected (SSH/SFTP access
	// while down, or Exec during a reconnect window).
	ErrNotConnected = errors.New("remote: not connected")
	// ErrAuthFailed: every configured auth method was rejected; reconnects
	// stop rather than re-prompting in the background.
	ErrAuthFailed = errors.New("remote: authentication failed")
	// ErrHostKeyMismatch: the presented host key contradicts a recorded one.
	// Never promptable — the user must inspect the named known_hosts line.
	ErrHostKeyMismatch = errors.New("remote: host key mismatch")
	// ErrHostKeyRejected: the user declined a first-seen (TOFU) fingerprint.
	ErrHostKeyRejected = errors.New("remote: host key rejected")
	// ErrDisconnected: a shared resource (SFTP handle) belongs to a previous
	// connection generation; re-fetch it from the client.
	ErrDisconnected = errors.New("remote: connection was re-established, re-fetch the handle")
)

// classifyDialError maps an ssh handshake error to a typed error where the
// distinction matters to the reconnect supervisor: authentication failures are
// unrecoverable (stop rather than loop re-prompting), everything else is a
// transient network error worth retrying.
func classifyDialError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unable to authenticate") ||
		strings.Contains(msg, "no supported methods remain") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "password required but no prompt available") ||
		strings.Contains(msg, "key passphrase required but no prompt available") {
		return errAuth{err}
	}
	return err
}

// errAuth wraps an unrecoverable authentication failure so the supervisor can
// detect it via errors.Is(err, ErrAuthFailed) while preserving the detail.
type errAuth struct{ err error }

func (e errAuth) Error() string { return e.err.Error() }
func (e errAuth) Unwrap() error { return e.err }
func (e errAuth) Is(target error) bool {
	return target == ErrAuthFailed
}

// Clock is the test seam for keepalive and reconnect timing.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// KeepalivePolicy controls liveness probing of an established connection.
type KeepalivePolicy struct {
	Interval  time.Duration // 0 => 30s; <0 disables keepalive
	MaxMisses int           // consecutive failures before declaring the link dead; 0 => 3
	Timeout   time.Duration // per-probe reply timeout; 0 => 10s
}

func (p KeepalivePolicy) interval() time.Duration {
	if p.Interval < 0 {
		return 0
	}
	if p.Interval == 0 {
		return 30 * time.Second
	}
	return p.Interval
}

func (p KeepalivePolicy) maxMisses() int {
	if p.MaxMisses <= 0 {
		return 3
	}
	return p.MaxMisses
}

func (p KeepalivePolicy) timeout() time.Duration {
	if p.Timeout <= 0 {
		return 10 * time.Second
	}
	return p.Timeout
}

// BackoffPolicy controls reconnect pacing: full-jitter exponential backoff.
type BackoffPolicy struct {
	Initial time.Duration // 0 => 1s
	Factor  float64       // 0 => 2
	Max     time.Duration // 0 => 60s
}

func (p BackoffPolicy) initial() time.Duration {
	if p.Initial <= 0 {
		return time.Second
	}
	return p.Initial
}

func (p BackoffPolicy) factor() float64 {
	if p.Factor <= 1 {
		return 2
	}
	return p.Factor
}

func (p BackoffPolicy) max() time.Duration {
	if p.Max <= 0 {
		return 60 * time.Second
	}
	return p.Max
}

// delay computes the ceiling for attempt n (0-based); the supervisor draws a
// full-jitter value in [0, delay] from its rng.
func (p BackoffPolicy) delay(attempt int) time.Duration {
	d := float64(p.initial())
	f := p.factor()
	for i := 0; i < attempt; i++ {
		d *= f
		if d >= float64(p.max()) {
			return p.max()
		}
	}
	if d >= float64(p.max()) {
		return p.max()
	}
	return time.Duration(d)
}
