package idempotency

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"reasonix/internal/remote/protocol"
)

// Outcome is the short, immediate RPC admission result retained by the
// registry. It contains either canonical result JSON or one frozen,
// deterministic Reasonix business error. It never contains an event stream or
// a turn's eventual answer.
type Outcome struct {
	result      []byte
	remoteError *protocol.RemoteError
}

// PrepareSuccess canonicalizes a short typed admission result before the
// sequencer commits business state. Preparing first prevents a serialization
// failure from stranding an already accepted mutation in pending state.
func PrepareSuccess(result any) (Outcome, error) {
	canonical, err := CanonicalJSON(result)
	if err != nil {
		return Outcome{}, fmt.Errorf("idempotency: encode admission result: %w", err)
	}
	return Outcome{result: canonical}, nil
}

// PrepareRejection validates and snapshots a deterministic business rejection
// from the frozen Remote error table. Transport, lease, stale-epoch and other
// pre-admission errors belong in Claim.Abort instead.
func PrepareRejection(err error) (Outcome, error) {
	if err == nil {
		return Outcome{}, errors.New("idempotency: deterministic business error is nil")
	}
	var remote *protocol.RemoteError
	if !errors.As(err, &remote) || remote == nil {
		return Outcome{}, fmt.Errorf("idempotency: only a protocol RemoteError can be cached, got %T", err)
	}
	if isPreAdmissionError(remote.Code) {
		return Outcome{}, fmt.Errorf("idempotency: %s is a pre-admission error and must not be cached", remote.Code)
	}
	canonical, canonicalErr := canonicalRemoteError(remote)
	if canonicalErr != nil {
		return Outcome{}, canonicalErr
	}
	return Outcome{remoteError: canonical}, nil
}

func isPreAdmissionError(code protocol.ReasonixErrorCode) bool {
	switch code {
	case protocol.ErrRemoteNotInstalled,
		protocol.ErrHostStopped,
		protocol.ErrVersionMismatch,
		protocol.ErrDaemonRestartRequired,
		protocol.ErrHostBusy,
		protocol.ErrStaleHostEpoch,
		protocol.ErrStaleRuntimeEpoch,
		protocol.ErrRequestIDConflict,
		protocol.ErrLeaseNotHeld,
		protocol.ErrStaleConnection:
		return true
	default:
		return false
	}
}

func (o Outcome) valid() bool {
	return (o.result != nil) != (o.remoteError != nil)
}

// ResultJSON returns a defensive copy of the cached canonical result. It is
// nil for a deterministic business rejection.
func (o Outcome) ResultJSON() json.RawMessage {
	if o.remoteError != nil || o.result == nil {
		return nil
	}
	return append(json.RawMessage(nil), o.result...)
}

// RemoteError returns a defensive copy of the cached deterministic rejection.
func (o Outcome) RemoteError() *protocol.RemoteError {
	return cloneRemoteError(o.remoteError)
}

// Decode decodes a successful result into dst. A rejected outcome returns the
// cached RemoteError instead, so a router can use one path for first responses
// and replays.
func (o Outcome) Decode(dst any) error {
	if remote := o.RemoteError(); remote != nil {
		return remote
	}
	if dst == nil {
		return errors.New("idempotency: result destination is nil")
	}
	decoder := json.NewDecoder(bytes.NewReader(o.result))
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("idempotency: decode admission result: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	return nil
}

func (o Outcome) clone() Outcome {
	return Outcome{result: append([]byte(nil), o.result...), remoteError: cloneRemoteError(o.remoteError)}
}

func canonicalRemoteError(remote *protocol.RemoteError) (*protocol.RemoteError, error) {
	data := remote.Data
	options := protocol.ErrorOptions{
		Target:                     cloneRuntimeTarget(data.Target),
		Expected:                   data.Expected,
		Actual:                     data.Actual,
		RetryAfterMs:               cloneInt64(data.RetryAfterMs),
		WorkspaceMayHaveChanged:    cloneBool(data.WorkspaceMayHaveChanged),
		ConversationMayHaveChanged: cloneBool(data.ConversationMayHaveChanged),
		SnapshotRequired:           cloneBool(data.SnapshotRequired),
	}
	canonical, err := protocol.NewRemoteError(remote.Code, options)
	if err != nil {
		return nil, fmt.Errorf("idempotency: invalid deterministic business error: %w", err)
	}
	if !reflect.DeepEqual(remote, canonical) {
		return nil, errors.New("idempotency: business error does not match the frozen protocol error table")
	}
	return canonical, nil
}

func cloneRemoteError(remote *protocol.RemoteError) *protocol.RemoteError {
	if remote == nil {
		return nil
	}
	clone := *remote
	clone.Data = remote.Data
	clone.Data.Target = cloneRuntimeTarget(remote.Data.Target)
	clone.Data.RetryAfterMs = cloneInt64(remote.Data.RetryAfterMs)
	clone.Data.WorkspaceMayHaveChanged = cloneBool(remote.Data.WorkspaceMayHaveChanged)
	clone.Data.ConversationMayHaveChanged = cloneBool(remote.Data.ConversationMayHaveChanged)
	clone.Data.SnapshotRequired = cloneBool(remote.Data.SnapshotRequired)
	return &clone
}

func cloneRuntimeTarget(target *protocol.RuntimeTarget) *protocol.RuntimeTarget {
	if target == nil {
		return nil
	}
	clone := *target
	return &clone
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("idempotency: admission result contains trailing JSON")
		}
		return fmt.Errorf("idempotency: decode admission result trailer: %w", err)
	}
	return nil
}
