package openai

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"
)

// defaultPostFinishGrace is how long a stream may stay open after its
// finish_reason. What can still follow — the usage record and [DONE] — arrives
// within moments, so a relay that goes on sending heartbeats (each of which
// resets the idle watchdog) is held to this window rather than to the idle one.
const defaultPostFinishGrace = 10 * time.Second

// streamWatch closes a response body that the read loop can no longer unblock
// itself on: the context is canceled (user interrupt), the stream goes silent
// past the idle window, or it outlives its own finish_reason by the grace
// window. It owns the timers; the read loop only pings and signals through
// buffered channels, so there is no Timer.Reset race.
type streamWatch struct {
	activity chan struct{}
	finished chan struct{}
	done     chan struct{}
	idle     time.Duration // the silence window, for the error that names it
	stalled  atomic.Bool
	graceCut atomic.Bool
}

func (c *client) watchStream(ctx context.Context, resp *http.Response) *streamWatch {
	idleTimeout := c.idleTimeout
	if idleTimeout <= 0 { // zero-value client (constructed without New)
		idleTimeout = defaultStreamIdleTimeout
	}
	grace := c.postFinishGrace
	if grace <= 0 {
		grace = defaultPostFinishGrace
	}
	w := &streamWatch{activity: make(chan struct{}, 1), finished: make(chan struct{}, 1), done: make(chan struct{}), idle: idleTimeout}
	go w.run(ctx, resp, idleTimeout, grace)
	return w
}

// stop lets the watchdog exit on a normal return; otherwise it would outlive
// the call and block forever on a non-cancellable context whose Done() is nil.
func (w *streamWatch) stop() { close(w.done) }

// ping records that the far side sent something. A full buffer is fine.
func (w *streamWatch) ping() {
	select {
	case w.activity <- struct{}{}:
	default:
	}
}

// finish records that the stream carried its finish_reason. Called once.
func (w *streamWatch) finish() {
	select {
	case w.finished <- struct{}{}:
	default:
	}
}

func (w *streamWatch) run(ctx context.Context, resp *http.Response, idleTimeout, grace time.Duration) {
	idle := time.NewTimer(idleTimeout)
	defer idle.Stop()
	var expiry <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			resp.Body.Close()
			return
		case <-idle.C:
			w.stalled.Store(true)
			resp.Body.Close()
			return
		case <-w.activity:
			if expiry != nil {
				continue
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(idleTimeout)
		case <-w.finished:
			idle.Stop()
			window := time.NewTimer(grace)
			defer window.Stop()
			expiry = window.C
		case <-expiry:
			w.graceCut.Store(true)
			resp.Body.Close()
			return
		case <-w.done:
			return
		}
	}
}
