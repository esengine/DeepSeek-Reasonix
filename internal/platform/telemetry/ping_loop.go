package telemetry

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"
)

// attemptTimeout bounds one request. It runs off the launch path, so it only
// has to be long enough for a slow link, never short enough to be felt.
const attemptTimeout = 10 * time.Second

const (
	pingRetryCap = 15 * time.Minute
	// A sleeping machine stops the monotonic clock a timer counts on, so a wait
	// across midnight can overshoot; waking this often re-reads the wall clock.
	maxWait       = time.Hour
	rolloverSplay = 10 * time.Minute
)

// statusError is a non-2xx answer. The code is the producer's own classification:
// callers ask permanent(), never the message.
type statusError struct{ code int }

func (e *statusError) Error() string { return fmt.Sprintf("telemetry: HTTP %d", e.code) }

// permanent reports a refusal that asking again today cannot change. 429 and
// 5xx say try later and are not in it.
func (e *statusError) permanent() bool {
	switch e.code {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusGone:
		return true
	}
	return false
}

var pingRetryBase = 30 * time.Second

// errPingInFlight means another process holds today's claim and has not
// finished: neither sent nor failed yet, so the caller asks again later.
var errPingInFlight = errors.New("telemetry: daily ping claimed by another process")

func (c *Client) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *Client) pause(ctx context.Context, d time.Duration) bool {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// background drains the counter queue once and, when ping is set, keeps the
// install counted for every UTC day the process is alive: a failed attempt
// backs off and retries, a success waits for the next UTC midnight. ping is
// separate from the drain because the surfaces expose them as separate
// consents. It returns when ctx ends.
func (c *Client) background(ctx context.Context, ping bool) {
	var due time.Time
	failures := 0
	flushed := false
	for ctx.Err() == nil {
		if ping && !c.clock().Before(due) {
			if !c.pingAllowed() {
				failures = 0
				due = c.clock().Add(maxWait)
			} else if err := c.attemptPing(ctx); err == nil {
				failures = 0
				due = c.nextRollover()
			} else if se := (*statusError)(nil); errors.As(err, &se) && se.permanent() {
				failures = 0
				due = c.nextRollover()
			} else {
				failures++
				due = c.clock().Add(retryDelay(failures))
			}
		}
		if !flushed {
			flushed = true
			fctx, cancel := context.WithTimeout(ctx, attemptTimeout)
			_ = c.flushPending(fctx)
			cancel()
		}
		if !ping {
			return
		}
		if !c.pause(ctx, min(max(due.Sub(c.clock()), 0), maxWait)) {
			return
		}
	}
}

func (c *Client) attemptPing(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()
	return c.sendDailyPing(ctx)
}

// pingAllowed re-reads consent before every attempt, so turning telemetry off
// stops the next send without a restart; while it is off the loop only idles.
func (c *Client) pingAllowed() bool {
	return !OptedOut() && (c.allowed == nil || c.allowed())
}

// nextRollover is the start of the next UTC day plus a splay, so installs left
// open overnight do not all report in the same second.
func (c *Client) nextRollover() time.Time {
	y, m, d := c.clock().UTC().Date()
	midnight := time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
	return midnight.Add(rand.N(rolloverSplay))
}

func retryDelay(failures int) time.Duration {
	d := pingRetryBase
	for i := 1; i < failures && d < pingRetryCap; i++ {
		d *= 2
	}
	d = min(d, pingRetryCap)
	return d - d/5 + rand.N(d/5*2+1)
}
