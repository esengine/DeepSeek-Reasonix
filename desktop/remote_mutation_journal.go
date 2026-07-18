package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	remoteclient "reasonix/internal/remote/client"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

const remoteMutationJournalLimit = 1024

var ErrRemoteMutationInFlight = errors.New("the same Remote mutation is already in flight")

// RemoteMutationOutcomeUnknownError means the request may have committed on
// the Host but its response was not observed. Repeating the same semantic App
// action while host/runtime epochs still match reuses RequestID; it is never
// retried automatically and never crosses an epoch boundary.
type RemoteMutationOutcomeUnknownError struct {
	RequestID protocol.RequestID
	Err       error
}

func (e *RemoteMutationOutcomeUnknownError) Error() string {
	if e == nil {
		return "Remote mutation outcome is unknown"
	}
	return fmt.Sprintf("Remote mutation outcome is unknown (requestId %s): %v", e.RequestID, e.Err)
}

func (e *RemoteMutationOutcomeUnknownError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *RemoteMutationOutcomeUnknownError) MutationRequestID() string {
	if e == nil {
		return ""
	}
	return string(e.RequestID)
}

type remoteMutationKey struct {
	method       protocol.Method
	hostEpoch    protocol.HostEpoch
	target       protocol.RuntimeTarget
	runtimeEpoch protocol.RuntimeEpoch
	fingerprint  string
}

type remoteMutationEntry struct {
	requestID protocol.RequestID
	inFlight  bool
	ordinal   uint64
}

type remoteMutationJournal struct {
	mu      sync.Mutex
	entries map[remoteMutationKey]*remoteMutationEntry
	next    uint64
}

func newRemoteMutationJournal() remoteMutationJournal {
	return remoteMutationJournal{entries: make(map[remoteMutationKey]*remoteMutationEntry)}
}

type remoteMutationAttempt struct {
	journal   *remoteMutationJournal
	key       remoteMutationKey
	requestID protocol.RequestID
	finished  bool
}

func (j *remoteMutationJournal) begin(
	newRequestID func() (protocol.RequestID, error),
	method protocol.Method,
	hostEpoch protocol.HostEpoch,
	target protocol.RuntimeTarget,
	runtimeEpoch protocol.RuntimeEpoch,
	semantic any,
) (*remoteMutationAttempt, error) {
	if j == nil || newRequestID == nil {
		return nil, errors.New("Remote mutation journal is unavailable")
	}
	raw, err := json.Marshal(semantic)
	if err != nil {
		return nil, fmt.Errorf("encode Remote mutation identity: %w", err)
	}
	sum := sha256.Sum256(raw)
	key := remoteMutationKey{
		method: method, hostEpoch: hostEpoch, target: target, runtimeEpoch: runtimeEpoch,
		fingerprint: hex.EncodeToString(sum[:]),
	}

	j.mu.Lock()
	if j.entries == nil {
		j.entries = make(map[remoteMutationKey]*remoteMutationEntry)
	}
	if entry := j.entries[key]; entry != nil {
		if entry.inFlight {
			j.mu.Unlock()
			return nil, ErrRemoteMutationInFlight
		}
		entry.inFlight = true
		j.next++
		entry.ordinal = j.next
		attempt := &remoteMutationAttempt{journal: j, key: key, requestID: entry.requestID}
		j.mu.Unlock()
		return attempt, nil
	}
	requestID, err := newRequestID()
	if err != nil {
		j.mu.Unlock()
		return nil, err
	}
	j.next++
	j.entries[key] = &remoteMutationEntry{requestID: requestID, inFlight: true, ordinal: j.next}
	j.evictLocked()
	attempt := &remoteMutationAttempt{journal: j, key: key, requestID: requestID}
	j.mu.Unlock()
	return attempt, nil
}

func (j *remoteMutationJournal) evictLocked() {
	for len(j.entries) > remoteMutationJournalLimit {
		var oldestKey remoteMutationKey
		var oldest uint64
		found := false
		for key, entry := range j.entries {
			if entry == nil || entry.inFlight {
				continue
			}
			if !found || entry.ordinal < oldest {
				oldestKey, oldest, found = key, entry.ordinal, true
			}
		}
		if !found {
			return
		}
		delete(j.entries, oldestKey)
	}
}

func (a *remoteMutationAttempt) id() protocol.RequestID {
	if a == nil {
		return ""
	}
	return a.requestID
}

// finish retains the journal entry only when there was no conforming response.
// A received Remote/RPC error is a known outcome; transport, generation,
// cancellation, and protocol-loss errors remain explicitly retryable by the
// user with this exact RequestID.
func (a *remoteMutationAttempt) finish(err error) error {
	if a == nil || a.journal == nil || a.finished {
		return err
	}
	a.finished = true
	unknown := remoteMutationOutcomeUnknown(err)
	a.journal.mu.Lock()
	if entry := a.journal.entries[a.key]; entry != nil && entry.requestID == a.requestID {
		if unknown {
			entry.inFlight = false
			a.journal.next++
			entry.ordinal = a.journal.next
		} else {
			delete(a.journal.entries, a.key)
		}
	}
	a.journal.evictLocked()
	a.journal.mu.Unlock()
	if unknown {
		return &RemoteMutationOutcomeUnknownError{RequestID: a.requestID, Err: err}
	}
	return err
}

func remoteMutationOutcomeUnknown(err error) bool {
	if err == nil {
		return false
	}
	var remoteError *protocol.RemoteError
	if errors.As(err, &remoteError) {
		return false
	}
	var responseError *rpcwire.ResponseError
	if errors.As(err, &responseError) {
		return false
	}
	// A protocol fault can occur only after bytes were exchanged with an exact
	// Build peer. The business mutation may have committed before the malformed
	// result, so it is an unknown outcome even though reconnect is also required.
	var protocolError *remoteclient.ProtocolError
	if errors.As(err, &protocolError) {
		return true
	}
	return true
}
