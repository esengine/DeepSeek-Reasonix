// How a body-phase failure is classified before it leaves the provider.
package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"reasonix/internal/base/neterr"
	"reasonix/internal/contract/provider"
)

// errEmptyStream: a stream that ended cleanly having carried nothing at all —
// not even a usage record. That is the gateway saying nothing rather than the
// model answering with nothing, and only one is worth another request.
var errEmptyStream = errors.New("the upstream sent an empty stream")

// streamOutcome names what a finished read means. Nothing carried at all is
// the far side's failure; anything else keeps what the read loop decided.
func streamOutcome(name string, emitted bool, err error) error {
	if err == nil && !emitted {
		return provider.StreamInterrupt(fmt.Errorf("%s: %w", name, errEmptyStream), provider.StreamInterruptUpstreamError)
	}
	return err
}

// streamFailure decides whether a stream that stopped can be attempted again.
// A transport cut always can; a gateway reporting its own upstream failure can
// too, but only before it has said anything, since replaying after output
// would show it twice. The budget stays the Agent's — counting here too would
// stack two.
func streamFailure(emitted bool, err error) error {
	// A caller gave up, or a deadline did. Neither is the stream's fault.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	// A cut the read loop named already carries its reason; re-wrapping would
	// bury it under one this cannot infer as well.
	var interrupted *provider.StreamInterruptedError
	if errors.As(err, &interrupted) {
		return err
	}
	// A failed read is named by provider.ReadCut where it happens and leaves
	// through the branch above; this one is for transport errors arriving by
	// another route.
	if neterr.IsConnReset(err) {
		return provider.StreamInterrupt(err, provider.ClassifyStreamInterrupt(err))
	}
	var payload *provider.StreamPayloadError
	if !emitted && errors.As(err, &payload) {
		return provider.StreamInterrupt(err, provider.StreamInterruptUpstreamError)
	}
	return err
}

// stallCut is the end only this read loop can recognise: the deadline it set
// expired. Downstream the error is an unexpected EOF like any other, so the
// reason is attached here rather than inferred from the message later.
func stallCut(name string, idle time.Duration) error {
	err := fmt.Errorf("%s: stream stalled — no data for %s, connection likely dropped; raise idle_timeout_seconds to allow a longer silence: %w", name, idle, io.ErrUnexpectedEOF)
	return provider.StreamInterrupt(err, provider.StreamInterruptIdleTimeout)
}

// prematureCut is a clean close before any terminal the protocol accepts.
func prematureCut(name string) error {
	err := fmt.Errorf("%s: stream ended before completion: %w", name, io.ErrUnexpectedEOF)
	return provider.StreamInterrupt(err, provider.StreamInterruptPrematureEOF)
}
