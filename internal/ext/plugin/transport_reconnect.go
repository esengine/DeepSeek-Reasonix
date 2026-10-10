// Keeping one MCP server reachable across the death of its connection. A stdio
// child can exit long after a healthy handshake — a crash, an OOM kill, a
// database driver aborting — and its transport latches that read error for
// good. Without a replacement the Host keeps handing out the dead connection
// and every later call repeats the same EOF, carrying the stderr tail from
// startup, which reads like a server that never started. It is serverProxy's
// trade one layer down: a stable handle, a rolling backend.
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"reasonix/internal/contract/tool"
)

// errTransportGone marks a connection that ended before the request was
// written: that request never reached the server, so a replacement connection
// can carry it without repeating a side effect. A death mid-request is
// deliberately unmarked — the server may have run the call already.
var errTransportGone = errors.New("mcp connection ended")

type goneError struct{ error }

func (e goneError) Unwrap() []error { return []error{e.error, errTransportGone} }

// markGone tags err without changing the message a caller would see.
func markGone(err error) error {
	if err == nil {
		return nil
	}
	return goneError{err}
}

// reconnectingTransport is the stable handle consumers hold while the
// connection underneath rolls. dial opens a replacement and handshake brings it
// to a usable MCP session before any caller can reach it.
type reconnectingTransport struct {
	dial      func(lifeCtx, callCtx context.Context) (transport, error)
	handshake func(ctx context.Context, next transport) error
	// lifeCtx owns replacement children, so a cancelled turn can never kill a
	// server the session still holds. budget caps a single dial.
	lifeCtx context.Context
	budget  time.Duration

	mu       sync.Mutex
	active   transport
	progress map[string]*progressRegistration
	dialing  chan struct{} // non-nil while a dial runs; closed when it settles
	closed   bool
}

// progressRegistration is one live progress token: the sink to re-attach to a
// replacement connection, and the current connection's own unregister hook.
type progressRegistration struct {
	sink       tool.ProgressFunc
	unregister func()
}

func newReconnectingTransport(lifeCtx context.Context, active transport, budget time.Duration,
	dial func(lifeCtx, callCtx context.Context) (transport, error),
	handshake func(ctx context.Context, next transport) error,
) *reconnectingTransport {
	return &reconnectingTransport{
		dial: dial, handshake: handshake,
		lifeCtx: lifeCtx, budget: budget, active: active,
	}
}

// call dispatches once, and on a connection that was already gone dials a
// replacement and dispatches again. Exactly one replacement per call: a second
// death is returned, so a server dying on every spawn costs one attempt per
// tool call rather than a loop.
func (t *reconnectingTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	active, err := t.current()
	if err != nil {
		return nil, err
	}
	res, err := active.call(ctx, method, params)
	if !errors.Is(err, errTransportGone) {
		return res, err
	}
	next, dialErr := t.renew(ctx, active)
	if dialErr != nil {
		return nil, fmt.Errorf("%w; reconnect failed: %w", err, dialErr)
	}
	return next.call(ctx, method, params)
}

// notify forwards without reconnecting: a lost fire-and-forget message is not
// worth a process spawn, and the next call reconnects anyway.
func (t *reconnectingTransport) notify(ctx context.Context, method string, params any) error {
	active, err := t.current()
	if err != nil {
		return err
	}
	return active.notify(ctx, method, params)
}

func (t *reconnectingTransport) close() {
	t.mu.Lock()
	t.closed = true
	active, dialing := t.active, t.dialing
	t.active = nil
	t.mu.Unlock()
	if active != nil {
		active.close()
	}
	if dialing == nil {
		return
	}
	// A dial in flight owns a child that must not outlive teardown; it sees the
	// closed flag and closes whatever it produced. Budgeted so one wedged spawn
	// cannot stall a session close.
	select {
	case <-dialing:
	case <-time.After(closeWaitBudget):
	}
}

func (t *reconnectingTransport) current() (transport, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.active == nil {
		return nil, errors.New("mcp connection is closed")
	}
	return t.active, nil
}

// renew replaces stale with a fresh connection. Concurrent callers that all
// found the same dead connection share one dial; a caller whose context ends
// while waiting leaves without disturbing it.
func (t *reconnectingTransport) renew(ctx context.Context, stale transport) (transport, error) {
	for {
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			return nil, errors.New("mcp connection is closed")
		}
		if t.active != nil && t.active != stale {
			active := t.active
			t.mu.Unlock()
			return active, nil
		}
		if wait := t.dialing; wait != nil {
			t.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		done := make(chan struct{})
		t.dialing = done
		t.mu.Unlock()

		next, err := t.dialOne()

		t.mu.Lock()
		t.dialing = nil
		if err == nil && t.closed {
			err = errors.New("mcp connection is closed")
		}
		if err != nil {
			t.mu.Unlock()
			close(done)
			if next != nil {
				next.close()
			}
			return nil, err
		}
		t.active = next
		t.rebindProgressLocked(next)
		t.mu.Unlock()
		close(done)
		if stale != nil {
			stale.close()
		}
		return next, nil
	}
}

// dialOne opens and hands over one replacement. It runs on the caller's
// goroutine under the dialing token, so no spawn outlives the session, and the
// startup budget bounds how long that caller waits.
func (t *reconnectingTransport) dialOne() (transport, error) {
	budget := t.budget
	if budget <= 0 {
		budget = defaultStartTimeout
	}
	callCtx, cancel := context.WithTimeout(t.lifeCtx, budget)
	defer cancel()
	next, err := t.dial(t.lifeCtx, callCtx)
	if err != nil {
		return nil, err
	}
	if err := t.handshake(callCtx, next); err != nil {
		next.close()
		return nil, err
	}
	return next, nil
}

// setProtocolVersion reaches the live connection. A replacement is brought up
// by handshake, which negotiates and sets its own.
func (t *reconnectingTransport) setProtocolVersion(version string) {
	t.mu.Lock()
	active := t.active
	t.mu.Unlock()
	if versioned, ok := active.(protocolVersioned); ok {
		versioned.setProtocolVersion(version)
	}
}

// registerElicitCall reaches the connection live when the call starts; a call
// that outlives its connection has lost the server that would ask.
func (t *reconnectingTransport) registerElicitCall(ctx context.Context) func() {
	t.mu.Lock()
	router, ok := t.active.(elicitTransport)
	t.mu.Unlock()
	if !ok {
		return func() {}
	}
	return router.registerElicitCall(ctx)
}

func (t *reconnectingTransport) registerProgress(token string, sink tool.ProgressFunc) func() {
	if token == "" || sink == nil {
		return func() {}
	}
	registration := &progressRegistration{sink: sink}
	t.mu.Lock()
	if t.progress == nil {
		t.progress = map[string]*progressRegistration{}
	}
	t.progress[token] = registration
	if router, ok := t.active.(progressTransport); ok {
		registration.unregister = router.registerProgress(token, sink)
	}
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		registration, ok := t.progress[token]
		delete(t.progress, token)
		t.mu.Unlock()
		if ok && registration.unregister != nil {
			registration.unregister()
		}
	}
}

// rebindProgressLocked moves live tokens onto a replacement so a call that
// survives a reconnect keeps streaming. Caller holds t.mu.
func (t *reconnectingTransport) rebindProgressLocked(next transport) {
	router, ok := next.(progressTransport)
	if !ok {
		return
	}
	for token, registration := range t.progress {
		registration.unregister = router.registerProgress(token, registration.sink)
	}
}

func (t *reconnectingTransport) startupStderr() int {
	active, err := t.current()
	if err != nil {
		return 0
	}
	if diagnostic, ok := active.(startupDiagnosticTransport); ok {
		return diagnostic.startupStderr()
	}
	return 0
}
