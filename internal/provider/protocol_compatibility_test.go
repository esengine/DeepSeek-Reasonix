package provider

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

func freshProtocolHints() *protocolHints {
	return &protocolHints{entries: make(map[[32]byte]*protocolHint)}
}

func TestProtocolHintsBoundedConcurrentAndExpiring(t *testing.T) {
	hints := freshProtocolHints()
	key := sha256.Sum256([]byte("path"))
	now := time.Now()
	hints.failedHTTP2(key, "PROTOCOL_ERROR", hints.begin(), now)
	var probes atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if hints.selectHTTP1(key, now) != nil {
				probes.Add(1)
			}
		})
	}
	wg.Wait()
	if probes.Load() != 1 {
		t.Fatalf("concurrent probes = %d", probes.Load())
	}
	old := hints.entries[key]
	hints.settle(key, old, "active")
	if hints.selectHTTP1(key, now) != old {
		t.Fatal("successful path not reused")
	}
	hints.settle(key, old, "pending") // late cancellation must not undo success
	if old.mode != "active" {
		t.Fatal("late cancellation reset active hint")
	}
	hints.settle(key, old, "disabled")
	hints.settle(key, old, "active") // late success must not undo failure
	hints.failedHTTP2(key, "HTTP_1_1_REQUIRED", hints.begin(), now.Add(time.Minute))
	if hints.selectHTTP1(key, now.Add(time.Minute)) != nil || old.reason != "PROTOCOL_ERROR" {
		t.Fatal("failed compatibility was rearmed or overwritten")
	}
	next := now.Add(protocolCompatibilityTTL)
	if hints.selectHTTP1(key, next) != nil {
		t.Fatal("expired hint remained selected")
	}
	hints.failedHTTP2(key, "PROTOCOL_ERROR", hints.begin(), next)
	hints.settle(key, old, "disabled")
	if hints.selectHTTP1(key, next) == nil {
		t.Fatal("old completion disabled new generation")
	}
	for i := range protocolCompatibilityLimit + 10 {
		k := sha256.Sum256([]byte{byte(i), byte(i >> 8)})
		hints.failedHTTP2(k, "INTERNAL_ERROR", hints.begin(), next)
	}
	if len(hints.entries) != protocolCompatibilityLimit {
		t.Fatalf("unbounded hint count: %d", len(hints.entries))
	}
}

func TestInternalErrorNeedsALaterRequest(t *testing.T) {
	hints := freshProtocolHints()
	key := sha256.Sum256([]byte("path"))
	now := time.Now()
	first, sibling := hints.begin(), hints.begin()
	hints.failedHTTP2(key, "INTERNAL_ERROR", first, now)
	// A sibling that started before the observation shares its cause, e.g. one
	// connection-level reset seen by several concurrent streams.
	hints.failedHTTP2(key, "INTERNAL_ERROR", sibling, now.Add(time.Second))
	if hints.selectHTTP1(key, now.Add(time.Second)) != nil {
		t.Fatal("one INTERNAL_ERROR event selected HTTP/1.1")
	}
	later := now.Add(2 * time.Second)
	hints.failedHTTP2(key, "INTERNAL_ERROR", hints.begin(), later)
	h := hints.selectHTTP1(key, later)
	if h == nil || h.reason != "INTERNAL_ERROR" || !h.until.Equal(later.Add(protocolCompatibilityTTL)) {
		t.Fatalf("repeated INTERNAL_ERROR not confirmed: %+v", h)
	}
	other := sha256.Sum256([]byte("other"))
	hints.failedHTTP2(other, "INTERNAL_ERROR", hints.begin(), now)
	hints.failedHTTP2(other, "PROTOCOL_ERROR", hints.begin(), now)
	if h := hints.selectHTTP1(other, now); h == nil || h.reason != "PROTOCOL_ERROR" {
		t.Fatalf("explicit protocol evidence waited for a repeat: %+v", h)
	}
	expired := now.Add(protocolCompatibilityTTL)
	third := sha256.Sum256([]byte("third"))
	hints.failedHTTP2(third, "INTERNAL_ERROR", hints.begin(), now)
	hints.failedHTTP2(third, "INTERNAL_ERROR", hints.begin(), expired)
	if hints.selectHTTP1(third, expired) != nil {
		t.Fatal("expired observation was confirmed")
	}
}

func TestProtocolFailureOnlyUsesTypedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"internal-once", http2.StreamError{Code: http2.ErrCodeInternal}, false},
		{"protocol", http2.ConnectionError(http2.ErrCodeProtocol), true},
		{"requires-http1", http2.StreamError{Code: http2.ErrCodeHTTP11Required}, true},
		{"wording", errors.New("HTTP/2 INTERNAL_ERROR secret-token"), false},
		{"eof", io.ErrUnexpectedEOF, false},
		{"api", &APIError{Status: 401, Body: "INTERNAL_ERROR"}, false},
		{"cancel", context.Canceled, false},
		{"refused", http2.StreamError{Code: http2.ErrCodeRefusedStream}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hints := freshProtocolHints()
			tr := &protocolTransport{hints: hints}
			tr.failed(t.Context(), [32]byte{}, nil, hints.begin(), tc.err)
			if got := hints.selectHTTP1([32]byte{}, time.Now()) != nil; got != tc.want {
				t.Fatalf("selected=%t want=%t", got, tc.want)
			}
		})
	}
}

func TestOnlyCompatibilityTriggersAreTerminal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		terminal bool
	}{
		{"internal", errors.Join(http2.StreamError{Code: http2.ErrCodeInternal}, io.ErrUnexpectedEOF), true},
		{"protocol", http2.ConnectionError(http2.ErrCodeProtocol), true},
		{"requires-http1", http2.StreamError{Code: http2.ErrCodeHTTP11Required}, true},
		// The peer guarantees it never processed a refused stream.
		{"refused", errors.Join(http2.StreamError{Code: http2.ErrCodeRefusedStream}, io.ErrUnexpectedEOF), false},
		{"eof", io.ErrUnexpectedEOF, false},
		{"send-timeout", &RequestSendTimeoutError{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recovery := ClassifyRecovery(tc.err)
			if protocolFailureIsTerminal(tc.err) != tc.terminal || (recovery.Phase == "protocol") != tc.terminal {
				t.Fatalf("terminal=%t phase=%q", protocolFailureIsTerminal(tc.err), recovery.Phase)
			}
			if tc.terminal && (IsConnReset(tc.err) || recovery.Retryable) {
				t.Fatal("terminal protocol failure became retryable")
			}
		})
	}
}

func TestInternalErrorConfirmationIgnoresClockResolution(t *testing.T) {
	hints := freshProtocolHints()
	key := sha256.Sum256([]byte("path"))
	tick := time.Now() // every request below lands in the same clock tick
	first, concurrent := hints.begin(), hints.begin()
	hints.failedHTTP2(key, "INTERNAL_ERROR", first, tick)
	hints.failedHTTP2(key, "INTERNAL_ERROR", concurrent, tick)
	if hints.selectHTTP1(key, tick) != nil {
		t.Fatal("a request already running at the observation confirmed it")
	}
	hints.failedHTTP2(key, "INTERNAL_ERROR", hints.begin(), tick)
	if hints.selectHTTP1(key, tick) == nil {
		t.Fatal("a later request in the same clock tick did not confirm the route")
	}
}
