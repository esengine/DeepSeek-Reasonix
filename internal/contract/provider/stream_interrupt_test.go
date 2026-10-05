package provider

import (
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
	"time"
)

// The classifier used to match phrases like "stalled" and "forcibly closed".
// Both are wording: the first is ours and free to be reworded, the second is
// Windows' and rendered in the system language.
func TestClassifyStreamInterruptIgnoresWording(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"eof", fmt.Errorf("read: %w", io.ErrUnexpectedEOF), StreamInterruptPrematureEOF},
		{"reset", fmt.Errorf("read: %w", syscall.ECONNRESET), StreamInterruptConnectionReset},
		{"closed", fmt.Errorf("read: %w", net.ErrClosed), StreamInterruptConnectionReset},
		{"says stalled but is not", errors.New("stream stalled — no data for 30s"), StreamInterruptPrematureEOF},
		{"localized reset", errors.New("远程主机强迫关闭了一个现有的连接。"), StreamInterruptPrematureEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyStreamInterrupt(tc.err); got != tc.want {
				t.Fatalf("ClassifyStreamInterrupt = %q, want %q", got, tc.want)
			}
		})
	}
}

// An unset, zero or negative idle_timeout_seconds must keep the built-in
// default rather than becoming a zero window that trips on the first byte, and a
// value past the ceiling must clamp to it rather than overflow int64 negative.
func TestIdleTimeoutFromExtra(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra map[string]any
		want  time.Duration
	}{
		{"absent", map[string]any{}, StreamIdleTimeout},
		{"nil map", nil, StreamIdleTimeout},
		{"zero", map[string]any{IdleTimeoutSecondsKey: 0}, StreamIdleTimeout},
		{"negative", map[string]any{IdleTimeoutSecondsKey: -5}, StreamIdleTimeout},
		{"seconds", map[string]any{IdleTimeoutSecondsKey: 8}, 8 * time.Second},
		{"at ceiling", map[string]any{IdleTimeoutSecondsKey: MaxIdleTimeoutSeconds}, MaxIdleTimeoutSeconds * time.Second},
		{"past ceiling", map[string]any{IdleTimeoutSecondsKey: 10_000_000_000}, MaxIdleTimeoutSeconds * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IdleTimeoutFromExtra(tc.extra); got != tc.want {
				t.Fatalf("IdleTimeoutFromExtra = %v, want %v", got, tc.want)
			}
		})
	}
}

// An idle timeout is not visible in a transport error, so the read loop that
// set the deadline has to say so. Inferring it from the message is what this
// replaced, and a reason attached at the emit site must survive.
func TestStreamInterruptReasonPrefersTheEmitSite(t *testing.T) {
	stall := StreamInterrupt(fmt.Errorf("stream stalled: %w", io.ErrUnexpectedEOF), StreamInterruptIdleTimeout)
	if got := StreamInterruptReason(stall); got != StreamInterruptIdleTimeout {
		t.Fatalf("reason = %q, want %q", got, StreamInterruptIdleTimeout)
	}
	if got := ClassifyStreamInterrupt(fmt.Errorf("stream stalled: %w", io.ErrUnexpectedEOF)); got == StreamInterruptIdleTimeout {
		t.Fatal("an idle timeout was inferred from the message again")
	}
	unlabelled := &StreamInterruptedError{Err: fmt.Errorf("read: %w", syscall.ECONNRESET)}
	if got := StreamInterruptReason(unlabelled); got != StreamInterruptConnectionReset {
		t.Fatalf("unlabelled reason = %q, want %q", got, StreamInterruptConnectionReset)
	}
}
