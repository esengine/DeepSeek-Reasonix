package runtime

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

type mutationLedger struct {
	entries       map[protocol.RequestID]*mutationEntry
	sessionCounts map[string]int
	completed     list.List
}

type mutationEntry struct {
	requestID   protocol.RequestID
	method      protocol.Method
	targetKey   string
	sessionKey  string
	target      *protocol.RuntimeTarget
	fingerprint [sha256.Size]byte
	result      json.RawMessage
	remoteError *protocol.RemoteError
	completedAt time.Time
	lru         *list.Element
}

type mutationIdentity struct {
	requestID   protocol.RequestID
	targetKey   string
	sessionKey  string
	target      *protocol.RuntimeTarget
	fingerprint [sha256.Size]byte
}

func newMutationLedger() *mutationLedger {
	return &mutationLedger{
		entries:       make(map[protocol.RequestID]*mutationEntry),
		sessionCounts: make(map[string]int),
	}
}

func mutationIdentityFor(method protocol.Method, params any) (mutationIdentity, error) {
	encoded, err := json.Marshal(params)
	if err != nil {
		return mutationIdentity{}, fmt.Errorf("remote idempotency: encode params: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return mutationIdentity{}, fmt.Errorf("remote idempotency: decode params: %w", err)
	}
	var requestID protocol.RequestID
	if err := json.Unmarshal(fields["requestId"], &requestID); err != nil || strings.TrimSpace(string(requestID)) == "" {
		return mutationIdentity{}, errors.New("remote idempotency: requestId is required")
	}
	delete(fields, "requestId")

	identity := mutationIdentity{requestID: requestID, targetKey: "host"}
	if raw := fields["target"]; len(raw) != 0 {
		var target protocol.RuntimeTarget
		if err := json.Unmarshal(raw, &target); err != nil {
			return mutationIdentity{}, fmt.Errorf("remote idempotency: decode target: %w", err)
		}
		if err := target.Validate(); err != nil {
			return mutationIdentity{}, fmt.Errorf("remote idempotency: validate target: %w", err)
		}
		identity.target = &target
		identity.targetKey = "session:" + string(target.WorkspaceID) + ":" + string(target.SessionID)
		identity.sessionKey = identity.targetKey
	} else if raw := fields["workspaceId"]; len(raw) != 0 {
		var workspaceID protocol.WorkspaceID
		if err := json.Unmarshal(raw, &workspaceID); err != nil {
			return mutationIdentity{}, fmt.Errorf("remote idempotency: decode workspaceId: %w", err)
		}
		if strings.TrimSpace(string(workspaceID)) != "" {
			identity.targetKey = "workspace:" + string(workspaceID)
		}
	}
	envelope := struct {
		Method protocol.Method            `json:"method"`
		Target string                     `json:"target"`
		Params map[string]json.RawMessage `json:"params"`
	}{Method: method, Target: identity.targetKey, Params: fields}
	canonical, err := json.Marshal(envelope)
	if err != nil {
		return mutationIdentity{}, fmt.Errorf("remote idempotency: encode fingerprint: %w", err)
	}
	identity.fingerprint = sha256.Sum256(canonical)
	return identity, nil
}

func (l *mutationLedger) replay(method protocol.Method, identity mutationIdentity) (any, error, bool) {
	l.pruneExpired(time.Now())
	entry := l.entries[identity.requestID]
	if entry == nil {
		return nil, nil, false
	}
	if entry.method != method || entry.targetKey != identity.targetKey || entry.fingerprint != identity.fingerprint {
		return nil, protocol.MustRemoteError(protocol.ErrRequestIDConflict, protocol.ErrorOptions{Target: identity.target}), true
	}
	l.completed.MoveToBack(entry.lru)
	if entry.remoteError != nil {
		return nil, cloneRemoteError(entry.remoteError), true
	}
	result, err := protocol.DecodeResult(method, append(json.RawMessage(nil), entry.result...))
	return result, err, true
}

func (l *mutationLedger) record(method protocol.Method, identity mutationIdentity, result any, handlerErr error) error {
	entry := &mutationEntry{
		requestID: identity.requestID, method: method, targetKey: identity.targetKey,
		sessionKey: identity.sessionKey, target: cloneRuntimeTarget(identity.target),
		fingerprint: identity.fingerprint, completedAt: time.Now(),
	}
	if handlerErr != nil {
		var remoteErr *protocol.RemoteError
		if !errors.As(handlerErr, &remoteErr) || preAdmissionError(remoteErr.Code) {
			return nil
		}
		entry.remoteError = cloneRemoteError(remoteErr)
	} else {
		encoded, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("remote idempotency: encode result: %w", err)
		}
		entry.result = encoded
	}
	entry.lru = l.completed.PushBack(entry)
	l.entries[entry.requestID] = entry
	if entry.sessionKey != "" {
		l.sessionCounts[entry.sessionKey]++
	}
	l.pruneExpired(entry.completedAt)
	l.enforceLimits(entry.sessionKey)
	return nil
}

func preAdmissionError(code protocol.ReasonixErrorCode) bool {
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

func (l *mutationLedger) enforceLimits(sessionKey string) {
	if sessionKey != "" {
		for l.sessionCounts[sessionKey] > protocol.IdempotencySessionEntries {
			if !l.evictOldest(sessionKey) {
				break
			}
		}
	}
	for len(l.entries) > protocol.IdempotencyHostEntries {
		if !l.evictOldest("") {
			break
		}
	}
}

func (l *mutationLedger) pruneExpired(now time.Time) {
	for element := l.completed.Front(); element != nil; {
		next := element.Next()
		entry := element.Value.(*mutationEntry)
		if !now.Before(entry.completedAt.Add(protocol.IdempotencyRetention)) {
			l.remove(entry)
		}
		element = next
	}
}

func (l *mutationLedger) evictOldest(sessionKey string) bool {
	for element := l.completed.Front(); element != nil; element = element.Next() {
		entry := element.Value.(*mutationEntry)
		if sessionKey != "" && entry.sessionKey != sessionKey {
			continue
		}
		l.remove(entry)
		return true
	}
	return false
}

func (l *mutationLedger) remove(entry *mutationEntry) {
	if l.entries[entry.requestID] != entry {
		return
	}
	delete(l.entries, entry.requestID)
	if entry.sessionKey != "" {
		l.sessionCounts[entry.sessionKey]--
		if l.sessionCounts[entry.sessionKey] == 0 {
			delete(l.sessionCounts, entry.sessionKey)
		}
	}
	if entry.lru != nil {
		l.completed.Remove(entry.lru)
		entry.lru = nil
	}
}

func cloneRuntimeTarget(target *protocol.RuntimeTarget) *protocol.RuntimeTarget {
	if target == nil {
		return nil
	}
	copyTarget := *target
	return &copyTarget
}

func cloneRemoteError(remoteErr *protocol.RemoteError) *protocol.RemoteError {
	if remoteErr == nil {
		return nil
	}
	data := remoteErr.Data
	return protocol.MustRemoteError(remoteErr.Code, protocol.ErrorOptions{
		Target: cloneRuntimeTarget(data.Target), Expected: data.Expected, Actual: data.Actual,
		RetryAfterMs: cloneInt64(data.RetryAfterMs), WorkspaceMayHaveChanged: cloneBool(data.WorkspaceMayHaveChanged),
		ConversationMayHaveChanged: cloneBool(data.ConversationMayHaveChanged), SnapshotRequired: cloneBool(data.SnapshotRequired),
	})
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func (s *Server) serializeHandlers(handlers protocol.HandlerSet) protocol.HandlerSet {
	serialized := make(protocol.HandlerSet, len(handlers))
	for method, handler := range handlers {
		method, handler := method, handler
		spec, _ := protocol.LookupMethod(method)
		if spec.Class == protocol.ClassConnection {
			serialized[method] = handler
			continue
		}
		serialized[method] = func(ctx context.Context, params any) (any, error) {
			s.requestMu.Lock()
			locked := true
			defer func() {
				if locked {
					s.requestMu.Unlock()
				}
			}()

			mutation := spec.Class == protocol.ClassHostMutation || spec.Class == protocol.ClassSessionMutation || spec.Class == protocol.ClassSessionRecordMutation
			var identity mutationIdentity
			if mutation {
				var err error
				identity, err = mutationIdentityFor(method, params)
				if err != nil {
					return nil, err
				}
				if result, err, found := s.mutations.replay(method, identity); found {
					return result, err
				}
			}

			result, handlerErr := handler(ctx, params)
			if mutation {
				if err := s.mutations.record(method, identity, result, handlerErr); err != nil {
					return nil, err
				}
			}
			if handlerErr != nil {
				return nil, handlerErr
			}
			switch response := result.(type) {
			case rpcwire.HandlerResponse:
				original := response.AfterWrite
				response.AfterWrite = func(writeErr error) {
					defer s.requestMu.Unlock()
					original(writeErr)
				}
				locked = false
				return response, nil
			case *rpcwire.HandlerResponse:
				if response == nil {
					return nil, errors.New("remote runtime: nil handler response")
				}
				copyResponse := *response
				original := copyResponse.AfterWrite
				copyResponse.AfterWrite = func(writeErr error) {
					defer s.requestMu.Unlock()
					original(writeErr)
				}
				locked = false
				return &copyResponse, nil
			default:
				return result, nil
			}
		}
	}
	return serialized
}
