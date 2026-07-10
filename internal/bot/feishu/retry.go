package feishu

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	mrand "math/rand"
	"net"
	"strings"
	"syscall"
	"time"

	"reasonix/internal/bot"
)

// newIdempotencyKey returns a random key for the Feishu create/reply `uuid`
// dedup field. It is generated once per logical send and reused across
// transient retries, so a retry after the request already reached the server
// (response read failed) does not post a duplicate visible message. An empty
// return (rand failure) simply omits the key — retries then behave as before.
func newIdempotencyKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

const (
	transientRetryAttempts  = 3
	transientRetryBaseDelay = 500 * time.Millisecond
	transientRetryMaxDelay  = 5 * time.Second
)

// withTransientRetry retries fn on transport-level failures (connection reset,
// timeout, broken pipe) with exponential backoff and jitter. Feishu API-level
// errors (rate limit, size limit, permission) are returned as-is — retrying
// those blindly would only make things worse.
func withTransientRetry(ctx context.Context, logger *slog.Logger, op string, fn func(context.Context) error) error {
	delay := transientRetryBaseDelay
	for attempt := 1; ; attempt++ {
		err := fn(ctx)
		if err == nil || attempt >= transientRetryAttempts || ctx.Err() != nil || !isTransientError(err) {
			return err
		}
		wait := delay + time.Duration(mrand.Int63n(int64(delay/4)+1))
		logger.Warn("feishu transient error; retrying", "op", op, "attempt", attempt, "wait", wait, "err", err)
		if !bot.SleepCtx(ctx, wait) {
			return err
		}
		delay *= 2
		if delay > transientRetryMaxDelay {
			delay = transientRetryMaxDelay
		}
	}
}

func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"connection reset", "broken pipe", "i/o timeout",
		"tls handshake timeout", "connection refused", "unexpected eof",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
