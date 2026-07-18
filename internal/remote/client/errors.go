package client

import (
	"errors"
	"fmt"
)

var (
	ErrNotConnected         = errors.New("Remote client is not connected")
	ErrClientClosed         = errors.New("Remote client is closed")
	ErrTransportLost        = errors.New("Remote transport was lost")
	ErrStaleGeneration      = errors.New("Remote result belongs to an old transport generation")
	ErrUnsubscribed         = errors.New("Remote subscription was removed")
	ErrSubscriptionReplaced = errors.New("Remote subscription was atomically replaced")
)

// ProtocolError means a peer claiming the same Build ID violated the frozen
// wire contract. The client closes that transport; callers must not retry the
// same malformed result as a business error.
type ProtocolError struct {
	Stage string
	Err   error
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Stage == "" {
		return fmt.Sprintf("Remote protocol violation: %v", e.Err)
	}
	return fmt.Sprintf("Remote protocol violation during %s: %v", e.Stage, e.Err)
}

func (e *ProtocolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// SequenceGapError requires a new atomic snapshot. Events after Got must not
// be applied to the old projection.
type SequenceGapError struct {
	Expected uint64
	Got      uint64
}

func (e *SequenceGapError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("Remote Session event sequence gap: expected %d, got %d", e.Expected, e.Got)
}
