package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"reasonix/internal/remote/protocol"
)

type notificationEnvelope struct {
	method  protocol.Method
	payload any
	raw     json.RawMessage
}

type subscriptionState struct {
	client       *Client
	connection   *connectionState
	id           protocol.SubscriptionID
	target       protocol.RuntimeTarget
	runtimeEpoch protocol.RuntimeEpoch
	pageTurns    int
	lastSeq      uint64

	input       chan notificationEnvelope
	updates     chan SubscriptionUpdate
	stop        chan SubscriptionUpdate
	stopOnce    sync.Once
	accepting   bool
	recoverable bool
}

func (s *subscriptionState) recovery() SubscriptionRecovery {
	return SubscriptionRecovery{
		Target: s.target, PageTurns: s.pageTurns,
		PreviousRuntimeEpoch: s.runtimeEpoch, LastSeq: s.lastSeq,
	}
}

func (s *subscriptionState) finish(update SubscriptionUpdate) {
	s.stopOnce.Do(func() { s.stop <- update })
}

// beforeNotification runs synchronously on rpcwire's read loop. Decoding and
// queue admission therefore preserve actual wire arrival order even though
// ordinary rpcwire notification handlers are asynchronous.
func (c *Client) beforeNotification(conn *connectionState, method protocol.Method, raw json.RawMessage) error {
	payload, err := protocol.DecodeNotificationParams(method, raw)
	if err != nil {
		c.protocolViolation(conn, "decode "+string(method)+" notification", err)
		return err
	}
	envelope := notificationEnvelope{method: method, payload: payload, raw: append(json.RawMessage(nil), raw...)}
	c.mu.Lock()
	if !conn.initialized {
		if len(conn.initializeBacklog) >= maxPendingNotifications {
			c.mu.Unlock()
			err := errors.New("notification backlog exceeded its bound during initialize")
			c.protocolViolation(conn, "initialize notification ordering", err)
			return err
		}
		conn.initializeBacklog = append(conn.initializeBacklog, envelope)
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	c.routeNotification(conn, envelope)
	return nil
}

func (c *Client) routeNotification(conn *connectionState, envelope notificationEnvelope) {
	c.mu.Lock()
	if c.closed || c.active != conn {
		c.mu.Unlock()
		return // late notification from an old local generation
	}
	switch payload := envelope.payload.(type) {
	case protocol.CatalogChanged:
		if payload.HostEpoch != c.lastInit.HostEpoch {
			c.mu.Unlock()
			c.protocolViolation(conn, "catalog notification", errors.New("Host epoch changed within one transport"))
			return
		}
		notification := CatalogNotification{Generation: conn.generation, Change: payload}
		c.mu.Unlock()
		select {
		case c.catalogs <- notification:
		default:
			c.protocolViolation(conn, "catalog notification", errors.New("catalog notification queue overflow"))
		}
	case protocol.SessionEvent:
		c.routeSubscriptionNotificationLocked(conn, payload.SubscriptionID, envelope)
	case protocol.SessionResyncRequired:
		c.routeSubscriptionNotificationLocked(conn, payload.SubscriptionID, envelope)
	default:
		c.mu.Unlock()
		c.protocolViolation(conn, "notification", fmt.Errorf("decoded payload type %T", envelope.payload))
	}
}

func (c *Client) routeSubscriptionNotificationLocked(conn *connectionState, id protocol.SubscriptionID, envelope notificationEnvelope) {
	if sub := conn.subscriptions[id]; sub != nil {
		if !sub.accepting {
			c.mu.Unlock()
			return
		}
		select {
		case sub.input <- envelope:
			c.mu.Unlock()
			return
		default:
			lastSeq := sub.lastSeq
			c.mu.Unlock()
			sub.finish(SubscriptionUpdate{Err: &SequenceGapError{Expected: lastSeq + 1, Got: 0}, SnapshotRequired: true})
			return
		}
	}
	if conn.pendingSubscribes > 0 && conn.pendingCount < maxPendingNotifications {
		conn.pending[id] = append(conn.pending[id], envelope)
		conn.pendingCount++
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.protocolViolation(conn, "subscription notification", errors.New("notification names an unknown subscription"))
}

// Subscribe establishes the Host's single atomic snapshot/event barrier. It
// buffers notifications whose new subscriptionId arrives before the JSON-RPC
// response, fully rehydrates the snapshot, then releases N+1 events in order.
func (c *Client) Subscribe(ctx context.Context, params protocol.SessionSubscribeParams) (*Subscription, error) {
	conn, err := c.currentConnection()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.active != conn {
		c.mu.Unlock()
		return nil, ErrStaleGeneration
	}
	conn.pendingSubscribes++
	c.mu.Unlock()

	value, rawResult, requestErr := c.requestOn(ctx, conn, protocol.MethodSessionSubscribe, params, true)
	if requestErr != nil {
		c.finishPendingSubscribe(conn, "")
		return nil, requestErr
	}
	result, ok := value.(protocol.SessionSubscribeResult)
	if !ok {
		c.finishPendingSubscribe(conn, "")
		return nil, c.protocolViolation(conn, "subscribe", fmt.Errorf("decoded result type %T", value))
	}
	if result.Snapshot.HostEpoch != params.ExpectedHostEpoch || result.Snapshot.Target != params.Target {
		c.finishPendingSubscribe(conn, result.SubscriptionID)
		return nil, c.protocolViolation(conn, "subscribe", errors.New("snapshot identity differs from request"))
	}
	snapshotRaw, err := extractObjectField(rawResult, "snapshot")
	if err != nil {
		c.finishPendingSubscribe(conn, result.SubscriptionID)
		return nil, c.protocolViolation(conn, "subscribe snapshot", err)
	}
	snapshot, err := c.rehydrateSnapshot(ctx, conn, snapshotRaw, result.Snapshot)
	if err != nil {
		c.finishPendingSubscribe(conn, result.SubscriptionID)
		c.bestEffortUnsubscribe(conn, result.SubscriptionID)
		return nil, err
	}

	sub := &subscriptionState{
		client: c, connection: conn, id: result.SubscriptionID, target: snapshot.Target,
		runtimeEpoch: snapshot.RuntimeEpoch, pageTurns: params.PageTurns, lastSeq: snapshot.BoundarySeq,
		// Capacity covers the entire bounded pre-response queue plus steady
		// state slack. Pending entries are inserted under c.mu before the map is
		// exposed, preserving wire order against later notifications.
		input:   make(chan notificationEnvelope, maxPendingNotifications+defaultSubscriptionBuffer),
		updates: make(chan SubscriptionUpdate, defaultSubscriptionBuffer), stop: make(chan SubscriptionUpdate, 1),
		accepting: true, recoverable: true,
	}
	c.mu.Lock()
	if c.closed || c.active != conn {
		c.mu.Unlock()
		c.finishPendingSubscribe(conn, result.SubscriptionID)
		c.bestEffortUnsubscribe(conn, result.SubscriptionID)
		return nil, ErrStaleGeneration
	}
	if conn.subscriptions[result.SubscriptionID] != nil {
		c.mu.Unlock()
		c.finishPendingSubscribe(conn, result.SubscriptionID)
		return nil, c.protocolViolation(conn, "subscribe", errors.New("Host reused an active subscriptionId"))
	}
	var replaced *subscriptionState
	if params.ReplaceSubscriptionID != "" {
		replaced = conn.subscriptions[params.ReplaceSubscriptionID]
		if replaced == nil {
			c.mu.Unlock()
			c.finishPendingSubscribe(conn, result.SubscriptionID)
			return nil, c.protocolViolation(conn, "subscribe replacement", errors.New("accepted replacement is absent locally"))
		}
	}
	conn.subscriptions[result.SubscriptionID] = sub
	pending := append([]notificationEnvelope(nil), conn.pending[result.SubscriptionID]...)
	conn.pendingCount -= len(pending)
	delete(conn.pending, result.SubscriptionID)
	for _, notification := range pending {
		sub.input <- notification
	}
	conn.pendingSubscribes--
	if replaced != nil {
		delete(conn.subscriptions, replaced.id)
	}
	delete(c.recovery, snapshot.Target)
	leftover := conn.pendingSubscribes == 0 && conn.pendingCount != 0
	c.mu.Unlock()

	go sub.run()
	if replaced != nil {
		replaced.finish(SubscriptionUpdate{Err: ErrSubscriptionReplaced})
	}
	if leftover {
		c.protocolViolation(conn, "subscribe notification ordering", errors.New("unclaimed notification subscriptionId"))
	}
	return &Subscription{
		ID: result.SubscriptionID, Generation: conn.generation, Snapshot: snapshot, Updates: sub.updates,
	}, nil
}

func (c *Client) finishPendingSubscribe(conn *connectionState, claimed protocol.SubscriptionID) {
	c.mu.Lock()
	if conn.pendingSubscribes > 0 {
		conn.pendingSubscribes--
	}
	if claimed != "" {
		conn.pendingCount -= len(conn.pending[claimed])
		delete(conn.pending, claimed)
	}
	leftover := conn.pendingSubscribes == 0 && conn.pendingCount != 0
	c.mu.Unlock()
	if leftover {
		c.protocolViolation(conn, "subscribe notification ordering", errors.New("unclaimed notification subscriptionId"))
	}
}

func (c *Client) bestEffortUnsubscribe(conn *connectionState, id protocol.SubscriptionID) {
	if id == "" || !c.isCurrent(conn) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), heartbeatRequestTimeout)
	defer cancel()
	_, _, _ = c.requestOn(ctx, conn, protocol.MethodSessionUnsubscribe,
		protocol.SessionUnsubscribeParams{SubscriptionID: id}, true)
}

func extractObjectField(raw json.RawMessage, field string) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	value := object[field]
	if len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, fmt.Errorf("result omitted %s", field)
	}
	return append(json.RawMessage(nil), value...), nil
}

func (s *subscriptionState) run() {
	defer func() {
		s.client.mu.Lock()
		s.accepting = false
		s.client.mu.Unlock()
		close(s.updates)
	}()
	for {
		select {
		case final := <-s.stop:
			s.deliverFinal(final)
			return
		case envelope := <-s.input:
			if final, stop := s.process(envelope); stop {
				s.deliverFinal(final)
				return
			}
		}
	}
}

func (s *subscriptionState) process(envelope notificationEnvelope) (SubscriptionUpdate, bool) {
	switch payload := envelope.payload.(type) {
	case protocol.SessionEvent:
		if err := s.validateEventIdentity(payload); err != nil {
			return SubscriptionUpdate{Err: err, SnapshotRequired: true}, true
		}
		expected := s.lastSequence() + 1
		if payload.Seq != expected {
			return SubscriptionUpdate{Err: &SequenceGapError{Expected: expected, Got: payload.Seq}, SnapshotRequired: true}, true
		}
		event, err := s.client.rehydrateEvent(context.Background(), s.connection, envelope.raw, payload)
		if err != nil {
			return SubscriptionUpdate{Err: err, SnapshotRequired: true}, true
		}
		s.client.mu.Lock()
		s.lastSeq = event.Seq
		s.client.mu.Unlock()
		update := SubscriptionUpdate{Event: &event}
		select {
		case s.updates <- update:
			return SubscriptionUpdate{}, false
		default:
			return SubscriptionUpdate{Err: &SequenceGapError{Expected: event.Seq, Got: 0}, SnapshotRequired: true}, true
		}
	case protocol.SessionResyncRequired:
		if err := s.validateResyncIdentity(payload); err != nil {
			return SubscriptionUpdate{Err: err, SnapshotRequired: true}, true
		}
		resync := payload
		return SubscriptionUpdate{Resync: &resync, SnapshotRequired: true}, true
	default:
		return SubscriptionUpdate{Err: &ProtocolError{Stage: "subscription notification", Err: fmt.Errorf("payload type %T", envelope.payload)}, SnapshotRequired: true}, true
	}
}

func (s *subscriptionState) lastSequence() uint64 {
	s.client.mu.Lock()
	defer s.client.mu.Unlock()
	return s.lastSeq
}

func (s *subscriptionState) validateEventIdentity(event protocol.SessionEvent) error {
	s.client.mu.Lock()
	hostEpoch := s.client.lastInit.HostEpoch
	s.client.mu.Unlock()
	if event.SubscriptionID != s.id || event.HostEpoch != hostEpoch || event.Target != s.target || event.RuntimeEpoch != s.runtimeEpoch {
		return &ProtocolError{Stage: "session event identity", Err: errors.New("event identity differs from subscription snapshot")}
	}
	return nil
}

func (s *subscriptionState) validateResyncIdentity(resync protocol.SessionResyncRequired) error {
	if err := s.validateEventIdentity(protocol.SessionEvent{
		SubscriptionID: resync.SubscriptionID, HostEpoch: resync.HostEpoch,
		Target: resync.Target, RuntimeEpoch: resync.RuntimeEpoch, Seq: 1,
	}); err != nil {
		return err
	}
	if resync.LastSeq != s.lastSequence() {
		return &SequenceGapError{Expected: s.lastSequence(), Got: resync.LastSeq}
	}
	return nil
}

func (s *subscriptionState) deliverFinal(update SubscriptionUpdate) {
	if update.Err == nil && update.Resync == nil && update.Event == nil {
		return
	}
	select {
	case s.updates <- update:
		return
	default:
	}
	// Preserve the terminal resync signal over buffered animation events.
	for {
		select {
		case <-s.updates:
		default:
			s.updates <- update
			return
		}
	}
}

// Unsubscribe is connection-level idempotent on the Host. The local stream is
// closed only after the typed success response.
func (c *Client) Unsubscribe(ctx context.Context, id protocol.SubscriptionID) (protocol.SessionUnsubscribeResult, error) {
	// Keep a typed success and its local cleanup in one connection lifecycle.
	// In particular, Connect must not restore an EOF-preserved recovery entry
	// between the response and the cleanup below.
	c.operationMu.Lock()
	defer c.operationMu.Unlock()

	conn, err := c.currentConnection()
	if err != nil {
		return protocol.SessionUnsubscribeResult{}, err
	}
	c.mu.Lock()
	observed := conn.subscriptions[id]
	var observedTarget protocol.RuntimeTarget
	if observed != nil {
		observedTarget = observed.target
	}
	c.mu.Unlock()
	value, _, err := c.requestOn(ctx, conn, protocol.MethodSessionUnsubscribe,
		protocol.SessionUnsubscribeParams{SubscriptionID: id}, false)
	if err != nil {
		return protocol.SessionUnsubscribeResult{}, err
	}
	result, ok := value.(protocol.SessionUnsubscribeResult)
	if !ok || !result.Unsubscribed {
		return protocol.SessionUnsubscribeResult{}, c.protocolViolation(conn, "unsubscribe", fmt.Errorf("decoded result type/value %T", value))
	}
	c.mu.Lock()
	sub := conn.subscriptions[id]
	if observed == nil || sub == observed {
		delete(conn.subscriptions, id)
	}
	if observedTarget != (protocol.RuntimeTarget{}) {
		// The EOF watcher may already have moved this exact subscription from
		// conn.subscriptions into recovery after the typed response arrived.
		delete(c.recovery, observedTarget)
	} else if sub != nil {
		delete(c.recovery, sub.target)
	}
	c.mu.Unlock()
	if observed != nil {
		observed.finish(SubscriptionUpdate{Err: ErrUnsubscribed})
	} else if sub != nil {
		sub.finish(SubscriptionUpdate{Err: ErrUnsubscribed})
	}
	return result, nil
}
