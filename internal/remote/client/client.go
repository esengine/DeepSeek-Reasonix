package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/nilutil"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

const (
	defaultSubscriptionBuffer = 256
	defaultCatalogBuffer      = 64
	defaultFaultBuffer        = 8
	maxPendingNotifications   = 1024
	heartbeatRequestTimeout   = 10 * time.Second
)

type connectionState struct {
	generation uint64
	raw        Transport
	wire       *rpcwire.Conn

	closeOnce  sync.Once
	terminated chan struct{}
	serveDone  chan error

	initialized       bool
	initializeBacklog []notificationEnvelope
	protocolFault     error
	detachState       detachState
	detachDone        chan struct{}
	ended             bool
	terminalErr       error
	heartbeatStop     chan struct{}
	heartbeatOnce     sync.Once

	subscriptions     map[protocol.SubscriptionID]*subscriptionState
	pending           map[protocol.SubscriptionID][]notificationEnvelope
	pendingCount      int
	pendingSubscribes int
}

type detachState uint8

const (
	detachNone detachState = iota
	detachInFlight
	detachSucceeded
	detachFailed
)

func (c *connectionState) close() {
	c.closeOnce.Do(func() {
		c.heartbeatOnce.Do(func() { close(c.heartbeatStop) })
		_ = c.raw.Close()
	})
}

// Client is safe for concurrent business requests. Connect and Detach are
// serialized, while responses and notifications are generation-checked.
type Client struct {
	factory          TransportFactory
	buildID          protocol.BuildID
	clientInstanceID protocol.ClientInstanceID

	operationMu sync.Mutex
	mu          sync.Mutex
	state       ConnectionState
	closed      bool
	nextGen     uint64
	lastGen     uint64
	active      *connectionState
	lastInit    protocol.InitializeResult
	leaseID     protocol.LeaseID
	recovery    map[protocol.RuntimeTarget]subscriptionRecoveryRecord

	catalogs chan CatalogNotification
	faults   chan ConnectionFault

	newTicker func(time.Duration) ticker
}

type subscriptionRecoveryRecord struct {
	SubscriptionRecovery
	generation   uint64
	subscription protocol.SubscriptionID
}

// New validates immutable Host-entry identity without opening a transport.
func New(options Options) (*Client, error) {
	if nilutil.IsNil(options.Factory) {
		return nil, errors.New("Remote transport factory is required")
	}
	if err := options.BuildID.Validate(); err != nil {
		return nil, fmt.Errorf("Remote client Build ID: %w", err)
	}
	if strings.TrimSpace(string(options.ClientInstanceID)) == "" {
		return nil, errors.New("Remote clientInstanceId is required")
	}
	if options.ResumeLeaseID != "" && strings.TrimSpace(string(options.ResumeLeaseID)) == "" {
		return nil, errors.New("Remote resumeLeaseId must be a non-empty opaque string when supplied")
	}
	identityJSON, err := json.Marshal(protocol.InitializeParams{
		BuildID: options.BuildID, ClientInstanceID: options.ClientInstanceID, ResumeLeaseID: options.ResumeLeaseID,
	})
	if err != nil {
		return nil, fmt.Errorf("encode Remote client identity: %w", err)
	}
	if _, err := protocol.DecodeRequestParams(protocol.MethodRemoteInitialize, identityJSON); err != nil {
		return nil, fmt.Errorf("validate Remote client identity: %w", err)
	}
	return &Client{
		factory: options.Factory, buildID: options.BuildID, clientInstanceID: options.ClientInstanceID,
		state: StateDisconnected, leaseID: options.ResumeLeaseID,
		recovery:  make(map[protocol.RuntimeTarget]subscriptionRecoveryRecord),
		catalogs:  make(chan CatalogNotification, defaultCatalogBuffer),
		faults:    make(chan ConnectionFault, defaultFaultBuffer),
		newTicker: func(interval time.Duration) ticker { return realTicker{time.NewTicker(interval)} },
	}, nil
}

// Connect opens a fresh transport and performs the mandatory first initialize
// request. A saved lease is resumed when possible. If another current
// transport exists it remains usable until the replacement handshake succeeds.
func (c *Client) Connect(ctx context.Context) (Connection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.operationMu.Lock()
	defer c.operationMu.Unlock()

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return Connection{}, ErrClientClosed
	}
	prior := c.active
	if prior != nil || c.leaseID != "" || len(c.recovery) != 0 {
		c.state = StateReconnecting
	} else {
		c.state = StateConnecting
	}
	c.nextGen++
	generation := c.nextGen
	resumeLease := c.leaseID
	c.mu.Unlock()

	raw, err := c.factory.Open(ctx)
	if err != nil {
		if !nilutil.IsNil(raw) {
			_ = raw.Close()
		}
		c.restoreStateAfterFailedConnect(prior)
		return Connection{}, fmt.Errorf("open Remote transport: %w", err)
	}
	if nilutil.IsNil(raw) {
		c.restoreStateAfterFailedConnect(prior)
		return Connection{}, errors.New("Remote transport factory returned nil")
	}

	conn := &connectionState{
		generation: generation, raw: raw, terminated: make(chan struct{}), serveDone: make(chan error, 1),
		heartbeatStop: make(chan struct{}), subscriptions: make(map[protocol.SubscriptionID]*subscriptionState),
		pending: make(map[protocol.SubscriptionID][]notificationEnvelope),
	}
	conn.wire = rpcwire.NewConn(raw, raw, rpcwire.Options{
		Name: "remote-client", MaxInboundBytes: protocol.FrameBytes, MaxOutboundBytes: protocol.FrameBytes,
		StrictJSONRPC: true,
		BeforeNotification: func(method string, params json.RawMessage) error {
			return c.beforeNotification(conn, protocol.Method(method), params)
		},
	})
	go func() { conn.serveDone <- conn.wire.Serve(context.Background()) }()
	go c.watchTransport(conn)

	value, _, err := c.requestOn(ctx, conn, protocol.MethodRemoteInitialize, protocol.InitializeParams{
		BuildID: c.buildID, ClientInstanceID: c.clientInstanceID, ResumeLeaseID: resumeLease,
	}, false)
	if err != nil {
		conn.close()
		<-conn.terminated
		err = c.connectAttemptError(ctx, conn, err)
		c.restoreStateAfterFailedConnect(prior)
		return Connection{}, err
	}
	result, ok := value.(protocol.InitializeResult)
	if !ok {
		err = &ProtocolError{Stage: "initialize", Err: fmt.Errorf("decoded result type %T", value)}
		conn.close()
		<-conn.terminated
		c.restoreStateAfterFailedConnect(prior)
		return Connection{}, err
	}
	if err := validateInitialize(c.buildID, result); err != nil {
		err = c.protocolViolation(conn, "initialize", err)
		<-conn.terminated
		c.restoreStateAfterFailedConnect(prior)
		return Connection{}, err
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		conn.close()
		<-conn.terminated
		return Connection{}, ErrClientClosed
	}
	if conn.ended {
		terminalErr := conn.terminalErr
		c.mu.Unlock()
		if terminalErr == nil {
			terminalErr = ErrTransportLost
		}
		c.restoreStateAfterFailedConnect(prior)
		return Connection{}, terminalErr
	}
	// Notifications are permitted immediately after the initialize response.
	// rpcwire's reader can observe them before this goroutine resumes, so drain
	// that small wire-ordered backlog only after the result is fully validated.
	old := c.active
	if old != nil {
		for target, sub := range recoveryFromSubscriptions(old.subscriptions) {
			c.recovery[target] = sub
		}
	}
	c.active = conn
	c.state = StateConnected
	c.lastGen = generation
	c.lastInit = result
	c.leaseID = result.Lease.LeaseID
	c.mu.Unlock()

	if old != nil && old != conn {
		c.retireConnection(old, ErrStaleGeneration)
	}
	// Keep later notifications in the same backlog until all earlier entries
	// have been routed. This closes the goroutine-scheduling window between the
	// response reader and the Connect caller without losing wire order.
	for {
		c.mu.Lock()
		backlog := append([]notificationEnvelope(nil), conn.initializeBacklog...)
		conn.initializeBacklog = nil
		if len(backlog) == 0 {
			conn.initialized = true
			c.mu.Unlock()
			break
		}
		c.mu.Unlock()
		for _, notification := range backlog {
			c.routeNotification(conn, notification)
		}
	}
	c.startHeartbeat(conn, result)
	return Connection{Generation: generation, Initialize: result}, nil
}

func (c *Client) connectAttemptError(ctx context.Context, conn *connectionState, requestErr error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	// A complete JSON-RPC response is authoritative even though cleanup closes
	// the rejected candidate transport immediately afterwards.
	var remoteError *protocol.RemoteError
	var responseError *rpcwire.ResponseError
	var protocolError *ProtocolError
	if errors.As(requestErr, &remoteError) || errors.As(requestErr, &responseError) || errors.As(requestErr, &protocolError) {
		return requestErr
	}
	c.mu.Lock()
	terminalErr := conn.terminalErr
	c.mu.Unlock()
	if terminalErr != nil {
		return terminalErr
	}
	return requestErr
}

func validateInitialize(expected protocol.BuildID, result protocol.InitializeResult) error {
	if err := protocol.CompareBuildID(expected, result.BuildID); err != nil {
		return err
	}
	if strings.TrimSpace(string(result.HostEpoch)) == "" || strings.TrimSpace(string(result.Lease.LeaseID)) == "" {
		return errors.New("initialize omitted Host or lease identity")
	}
	if result.Lease.TTLMillis != protocol.LeaseTTLMillis || result.Lease.PingIntervalMs != protocol.LeasePingIntervalMillis {
		return errors.New("initialize returned non-frozen lease timing")
	}
	if err := result.Capabilities.Validate(); err != nil {
		return fmt.Errorf("initialize capabilities: %w", err)
	}
	return nil
}

func (c *Client) restoreStateAfterFailedConnect(prior *connectionState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		c.state = StateClosed
	} else if prior != nil && c.active == prior {
		c.state = StateConnected
	} else if c.active != nil {
		c.state = StateConnected
	} else {
		c.state = StateDisconnected
	}
}

// Status returns current local connection identity without I/O.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := Status{State: c.state, Generation: c.lastGen, LeaseID: c.leaseID, HostEpoch: c.lastInit.HostEpoch,
		Host: c.lastInit.Host, Capabilities: c.lastInit.Capabilities}
	return status
}

// RecoveryState is retained across unexpected EOF. A successful graceful
// Detach clears it and the resumable lease.
func (c *Client) RecoveryState() RecoveryState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := RecoveryState{ResumeLeaseID: c.leaseID, HostEpoch: c.lastInit.HostEpoch, Generation: c.lastGen}
	items := make(map[protocol.RuntimeTarget]SubscriptionRecovery, len(c.recovery))
	for target, item := range c.recovery {
		items[target] = item.SubscriptionRecovery
	}
	if c.active != nil {
		for _, sub := range c.active.subscriptions {
			if sub.recoverable {
				items[sub.target] = sub.recovery()
			}
		}
	}
	for _, item := range items {
		state.Subscriptions = append(state.Subscriptions, item)
	}
	sort.Slice(state.Subscriptions, func(i, j int) bool {
		left, right := state.Subscriptions[i].Target, state.Subscriptions[j].Target
		if left.WorkspaceID != right.WorkspaceID {
			return left.WorkspaceID < right.WorkspaceID
		}
		return left.SessionID < right.SessionID
	})
	return state
}

// ForgetSubscriptionRecovery drops one target from the next reconnect plan.
// It is used after a higher layer rejects an attach that had already crossed
// the atomic subscribe boundary. Callers must only use it when that transport
// subscription is no longer current (or after an exact unsubscribe); otherwise
// the still-connected Host would continue sending notifications for an entry
// the client no longer recognizes.
func (c *Client) ForgetSubscriptionRecovery(target protocol.RuntimeTarget) {
	c.mu.Lock()
	delete(c.recovery, target)
	c.mu.Unlock()
}

// SuppressSubscriptionRecovery marks one exact transport subscription as
// intentionally abandoned. It is safe after an unsubscribe with unknown
// transport outcome: a still-connected peer may finish sending that old ID,
// but it can never enter the reconnect plan or replace a newer subscription.
func (c *Client) SuppressSubscriptionRecovery(
	generation uint64,
	id protocol.SubscriptionID,
	target protocol.RuntimeTarget,
) bool {
	if generation == 0 || id == "" || target == (protocol.RuntimeTarget{}) {
		return false
	}
	var suppressed *subscriptionState
	deletedRecovery := false
	c.mu.Lock()
	if c.active != nil && c.active.generation == generation {
		if sub := c.active.subscriptions[id]; sub != nil && sub.target == target {
			sub.recoverable = false
			suppressed = sub
			delete(c.recovery, target)
		}
	}
	if item, ok := c.recovery[target]; ok && item.generation == generation && item.subscription == id {
		delete(c.recovery, target)
		deletedRecovery = true
	}
	c.mu.Unlock()
	if suppressed != nil {
		suppressed.finish(SubscriptionUpdate{Err: ErrUnsubscribed})
	}
	return suppressed != nil || deletedRecovery
}

// CatalogNotifications returns strictly validated current-generation
// invalidations. The channel remains valid across reconnects.
func (c *Client) CatalogNotifications() <-chan CatalogNotification { return c.catalogs }

// Faults reports terminal current-generation transport failures. The channel
// remains valid across reconnects.
func (c *Client) Faults() <-chan ConnectionFault { return c.faults }

// Request invokes one ordinary registered request through the current
// generation and returns its exact registered result DTO. Connection,
// subscription, history, and content methods use dedicated helpers so their
// lifecycle and reconstruction invariants cannot be bypassed.
func (c *Client) Request(ctx context.Context, method protocol.Method, params any) (any, error) {
	switch method {
	case protocol.MethodRemoteInitialize, protocol.MethodRemotePing, protocol.MethodRemoteDetach,
		protocol.MethodSessionSubscribe, protocol.MethodSessionUnsubscribe,
		protocol.MethodSessionHistory, protocol.MethodSessionContent:
		return nil, fmt.Errorf("Remote method %s requires its typed client helper", method)
	}
	conn, err := c.currentConnection()
	if err != nil {
		return nil, err
	}
	result, _, err := c.requestOn(ctx, conn, method, params, true)
	return result, err
}

func (c *Client) currentConnection() (*connectionState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClientClosed
	}
	if c.active == nil || c.state != StateConnected {
		return nil, ErrNotConnected
	}
	return c.active, nil
}

func (c *Client) requestOn(ctx context.Context, conn *connectionState, method protocol.Method, params any, requireCurrent bool) (any, json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, nil, fmt.Errorf("encode %s params: %w", method, err)
	}
	normalized, err := protocol.DecodeRequestParams(method, rawParams)
	if err != nil {
		return nil, nil, fmt.Errorf("validate %s params: %w", method, err)
	}
	rawResult, err := conn.wire.Request(ctx, string(method), normalized)
	if requireCurrent && !c.isCurrent(conn) {
		return nil, nil, ErrStaleGeneration
	}
	if err != nil {
		mapped := c.mapResponseError(conn, method, err)
		return nil, nil, mapped
	}
	result, err := protocol.DecodeResult(method, rawResult)
	if err != nil {
		return nil, nil, c.protocolViolation(conn, "decode "+string(method)+" result", err)
	}
	return result, append(json.RawMessage(nil), rawResult...), nil
}

func (c *Client) isCurrent(conn *connectionState) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && c.active == conn && c.state == StateConnected
}

func (c *Client) mapResponseError(conn *connectionState, method protocol.Method, err error) error {
	var response *rpcwire.ResponseError
	if !errors.As(err, &response) {
		return err
	}
	if response.Code != protocol.DomainErrorCode {
		// With an exact Build ID and locally registry-validated params, these
		// standard errors are impossible from a conforming peer. Treat them as a
		// protocol fault instead of inviting a retry loop. -32603 remains a valid
		// generic server-internal failure under the frozen error rules.
		switch response.Code {
		case rpcwire.ErrParse, rpcwire.ErrInvalidRequest, rpcwire.ErrMethodNotFound, rpcwire.ErrInvalidParams:
			return c.protocolViolation(conn, "invoke "+string(method), response)
		}
		return response
	}
	data, decodeErr := decodeRemoteErrorData(response)
	if decodeErr != nil {
		return c.protocolViolation(conn, "decode "+string(method)+" error", decodeErr)
	}
	return &protocol.RemoteError{Code: data.ReasonixCode, Message: response.Message, Data: data}
}

func decodeRemoteErrorData(response *rpcwire.ResponseError) (protocol.RemoteErrorData, error) {
	if response == nil {
		return protocol.RemoteErrorData{}, errors.New("nil JSON-RPC error")
	}
	var data protocol.RemoteErrorData
	decoder := json.NewDecoder(bytes.NewReader(response.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return protocol.RemoteErrorData{}, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		if err == nil {
			return protocol.RemoteErrorData{}, errors.New("multiple JSON error data values")
		}
		return protocol.RemoteErrorData{}, err
	}
	if err := data.Validate(); err != nil {
		return protocol.RemoteErrorData{}, err
	}
	message := ""
	for _, contract := range protocol.ErrorContracts() {
		if contract.ReasonixCode == data.ReasonixCode {
			message = contract.Message
			break
		}
	}
	if message == "" || response.Message != message {
		return protocol.RemoteErrorData{}, errors.New("structured error message differs from the frozen error table")
	}
	return data, nil
}

func (c *Client) protocolViolation(conn *connectionState, stage string, cause error) error {
	err := &ProtocolError{Stage: stage, Err: cause}
	c.mu.Lock()
	if conn.protocolFault == nil {
		conn.protocolFault = err
	}
	c.mu.Unlock()
	conn.close()
	return err
}

func (c *Client) watchTransport(conn *connectionState) {
	serveErr := <-conn.serveDone
	c.mu.Lock()
	terminalErr := conn.protocolFault
	if terminalErr == nil {
		if serveErr == nil {
			terminalErr = ErrTransportLost
		} else {
			terminalErr = fmt.Errorf("%w: %w", ErrTransportLost, serveErr)
		}
	}
	conn.ended = true
	conn.terminalErr = terminalErr
	detachDone := conn.detachDone
	waitForDetach := conn.detachState == detachInFlight && detachDone != nil
	c.mu.Unlock()
	// EOF alone cannot distinguish the frozen response-then-close success path
	// from a real loss before the detach response. The in-flight request is
	// guaranteed to resolve when Serve shuts down, so wait outside c.mu for its
	// observed outcome before deciding whether to preserve recovery and fault.
	if waitForDetach {
		<-detachDone
	}
	c.mu.Lock()
	current := c.active == conn
	detaching := conn.detachState == detachSucceeded
	if current {
		if !detaching {
			for target, sub := range recoveryFromSubscriptions(conn.subscriptions) {
				c.recovery[target] = sub
			}
		}
		c.active = nil
		if !c.closed {
			c.state = StateDisconnected
		}
	}
	c.mu.Unlock()
	if current {
		retireCause := terminalErr
		if detaching {
			retireCause = ErrUnsubscribed
		}
		c.retireConnection(conn, retireCause)
		if !detaching {
			select {
			case c.faults <- ConnectionFault{Generation: conn.generation, Err: terminalErr}:
			default:
			}
		}
	}
	close(conn.terminated)
}

func recoveryFromSubscriptions(subscriptions map[protocol.SubscriptionID]*subscriptionState) map[protocol.RuntimeTarget]subscriptionRecoveryRecord {
	out := make(map[protocol.RuntimeTarget]subscriptionRecoveryRecord, len(subscriptions))
	for _, sub := range subscriptions {
		if sub.recoverable {
			out[sub.target] = subscriptionRecoveryRecord{
				SubscriptionRecovery: sub.recovery(), generation: sub.connection.generation, subscription: sub.id,
			}
		}
	}
	return out
}

func (c *Client) retireConnection(conn *connectionState, cause error) {
	conn.close()
	c.mu.Lock()
	subs := make([]*subscriptionState, 0, len(conn.subscriptions))
	for _, sub := range conn.subscriptions {
		subs = append(subs, sub)
	}
	conn.subscriptions = make(map[protocol.SubscriptionID]*subscriptionState)
	c.mu.Unlock()
	for _, sub := range subs {
		sub.finish(SubscriptionUpdate{Err: cause, SnapshotRequired: true})
	}
}

func (c *Client) startHeartbeat(conn *connectionState, initialize protocol.InitializeResult) {
	t := c.newTicker(time.Duration(initialize.Lease.PingIntervalMs) * time.Millisecond)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-t.C():
				ctx, cancel := context.WithTimeout(context.Background(), heartbeatRequestTimeout)
				_, err := c.pingOn(ctx, conn)
				cancel()
				if err != nil {
					if c.isCurrent(conn) {
						conn.close()
					}
					return
				}
			case <-conn.heartbeatStop:
				return
			}
		}
	}()
}

// Ping renews the current lease and validates the unchanged Host epoch.
func (c *Client) Ping(ctx context.Context) (protocol.PingResult, error) {
	conn, err := c.currentConnection()
	if err != nil {
		return protocol.PingResult{}, err
	}
	return c.pingOn(ctx, conn)
}

func (c *Client) pingOn(ctx context.Context, conn *connectionState) (protocol.PingResult, error) {
	c.mu.Lock()
	leaseID := c.leaseID
	hostEpoch := c.lastInit.HostEpoch
	c.mu.Unlock()
	value, _, err := c.requestOn(ctx, conn, protocol.MethodRemotePing, protocol.PingParams{LeaseID: leaseID}, true)
	if err != nil {
		return protocol.PingResult{}, err
	}
	result, ok := value.(protocol.PingResult)
	if !ok {
		return protocol.PingResult{}, c.protocolViolation(conn, "ping", fmt.Errorf("decoded result type %T", value))
	}
	if result.HostEpoch != hostEpoch || result.LeaseTTL != protocol.LeaseTTLMillis {
		return protocol.PingResult{}, c.protocolViolation(conn, "ping", errors.New("Host epoch or lease TTL changed"))
	}
	return result, nil
}

// Detach sends the response-ordered lease release, then clears all reconnect
// state. It never starts, stops, installs, or repairs the Host service.
func (c *Client) Detach(ctx context.Context) (protocol.DetachResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	conn, err := c.currentConnection()
	if err != nil {
		return protocol.DetachResult{}, err
	}
	c.mu.Lock()
	leaseID := c.leaseID
	conn.detachState = detachInFlight
	conn.detachDone = make(chan struct{})
	c.mu.Unlock()
	// detach is the one request whose successful response is immediately
	// followed by the peer closing this transport. operationMu prevents a
	// concurrent replacement, so accept that response even if the EOF watcher
	// wins the local scheduling race.
	value, _, err := c.requestOn(ctx, conn, protocol.MethodRemoteDetach, protocol.DetachParams{LeaseID: leaseID}, false)
	if err != nil {
		c.completeDetachAttempt(conn, detachFailed)
		return protocol.DetachResult{}, err
	}
	result, ok := value.(protocol.DetachResult)
	if !ok || !result.Detached {
		c.completeDetachAttempt(conn, detachFailed)
		return protocol.DetachResult{}, c.protocolViolation(conn, "detach", fmt.Errorf("decoded result type/value %T", value))
	}
	c.completeDetachAttempt(conn, detachSucceeded)
	c.mu.Lock()
	if c.active == conn || c.active == nil {
		c.active = nil
		c.state = StateDisconnected
		c.leaseID = ""
		c.recovery = make(map[protocol.RuntimeTarget]subscriptionRecoveryRecord)
	}
	c.mu.Unlock()
	c.retireConnection(conn, ErrUnsubscribed)
	return result, nil
}

func (c *Client) completeDetachAttempt(conn *connectionState, outcome detachState) {
	c.mu.Lock()
	if conn.detachState == detachInFlight {
		conn.detachState = outcome
		if conn.detachDone != nil {
			close(conn.detachDone)
		}
	}
	c.mu.Unlock()
}

// Close drops local transports without sending detach. The resumable lease and
// recovery state remain observable, but this Client instance cannot reconnect.
func (c *Client) Close() error {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.state = StateClosed
	conn := c.active
	if conn != nil {
		for target, sub := range recoveryFromSubscriptions(conn.subscriptions) {
			c.recovery[target] = sub
		}
	}
	c.active = nil
	c.mu.Unlock()
	if conn != nil {
		c.retireConnection(conn, ErrClientClosed)
	}
	return nil
}
