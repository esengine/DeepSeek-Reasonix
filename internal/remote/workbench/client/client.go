// Package client is the Desktop-side typed Remote Workbench peer.
package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/remote/broker"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/workbench/transport"
	"reasonix/internal/rpcwire"
)

type State struct {
	Initialized     bool                     `json:"initialized"`
	Generation      uint64                   `json:"generation"`
	HostEpoch       protocol.HostEpoch       `json:"hostEpoch"`
	LeaseID         protocol.LeaseID         `json:"leaseId"`
	WorkspaceID     protocol.WorkspaceID     `json:"workspaceId"`
	Target          protocol.RuntimeTarget   `json:"target"`
	RuntimeEpoch    protocol.RuntimeEpoch    `json:"runtimeEpoch"`
	SubscriptionID  protocol.SubscriptionID  `json:"subscriptionId"`
	SnapshotID      protocol.SnapshotID      `json:"snapshotId"`
	CurrentTurnID   protocol.TurnID          `json:"currentTurnId"`
	Capabilities    protocol.Capabilities    `json:"capabilities"`
	ResolvedProfile protocol.ResolvedProfile `json:"resolvedProfile"`
}

type Callbacks struct {
	OnSessionEvent   func(protocol.SessionEvent)
	OnResyncRequired func(protocol.SessionResyncRequired)
	OnCatalogChanged func(protocol.CatalogChanged)
	OnDisconnected   func()
}

type Client struct {
	conn   *rpcwire.Conn
	stream transport.Stream
	broker *broker.Desktop
	gen    uint64
	cancel context.CancelFunc

	mu             sync.Mutex
	subscribeMu    sync.Mutex
	projectionMu   sync.Mutex
	closed         bool
	state          State
	authorityRev   uint64
	snapshotRev    uint64
	callbacks      Callbacks
	notifyCh       chan any
	completedTurns map[protocol.TurnID]struct{}
	overflowed     atomic.Bool
}

// Connect performs a strict typed initialize before activating the local
// Provider Broker. callbacks is optional for backward-compatible callers.
func Connect(ctx context.Context, factory transport.Factory, gen uint64, brokerOpts broker.Options, buildID map[string]any, workspace string, callbacks ...Callbacks) (*Client, error) {
	if factory == nil {
		return nil, fmt.Errorf("transport factory required")
	}
	var id protocol.BuildID
	rawID, err := json.Marshal(buildID)
	if err != nil || json.Unmarshal(rawID, &id) != nil || id.Validate() != nil {
		return nil, fmt.Errorf("invalid Remote Build ID")
	}
	stream, err := factory.Open(ctx)
	if err != nil {
		return nil, err
	}
	wire := rpcwire.NewConn(stream, stream, rpcwire.Options{
		Name: "workbench-desktop", StrictJSONRPC: true,
		MaxInboundBytes: protocol.FrameBytes, MaxOutboundBytes: protocol.FrameBytes,
		MaxQueuedNotifications: protocol.RPCQueuedNotifications,
	})
	desktopBroker, err := broker.Attach(wire, brokerOpts)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	serveCtx, cancel := context.WithCancel(ctx)
	c := &Client{
		conn: wire, stream: stream, broker: desktopBroker, gen: gen, cancel: cancel,
		state: State{Generation: gen}, notifyCh: make(chan any, 128), completedTurns: make(map[protocol.TurnID]struct{}),
	}
	if len(callbacks) > 0 {
		c.callbacks = callbacks[0]
	}
	wire.HandleNotify(string(protocol.MethodSessionEvent), c.handleSessionEvent)
	wire.HandleNotify(string(protocol.MethodSessionResyncRequired), c.handleResync)
	wire.HandleNotify(string(protocol.MethodCatalogChanged), c.handleCatalogChanged)
	go c.deliveryLoop(serveCtx)
	go func() {
		_ = wire.Serve(serveCtx)
		c.close(false)
	}()

	initParams := protocol.InitializeParams{
		BuildID: id, ClientInstanceID: protocol.ClientInstanceID(fmt.Sprintf("desktop_%d", gen)), Workspace: strings.TrimSpace(workspace),
	}
	initRaw, err := wire.Request(ctx, string(protocol.MethodRemoteInitialize), initParams)
	if err != nil {
		c.close(false)
		return nil, fmt.Errorf("initialize: %w", err)
	}
	decoded, err := protocol.DecodeResult(protocol.MethodRemoteInitialize, initRaw)
	if err != nil {
		c.close(false)
		return nil, fmt.Errorf("initialize result: %w", err)
	}
	initialized := decoded.(protocol.InitializeResult)
	if err := protocol.CompareBuildID(id, initialized.BuildID); err != nil {
		c.close(false)
		return nil, fmt.Errorf("initialize build compatibility: %w", err)
	}
	c.mu.Lock()
	c.state.Initialized = true
	c.state.HostEpoch = initialized.HostEpoch
	c.state.LeaseID = initialized.Lease.LeaseID
	c.state.Capabilities = initialized.Capabilities
	c.mu.Unlock()
	if err := desktopBroker.Activate(); err != nil {
		c.close(false)
		return nil, fmt.Errorf("activate provider broker: %w", err)
	}

	listRaw, err := c.Request(ctx, string(protocol.MethodWorkspaceList), map[string]any{})
	if err != nil {
		c.close(false)
		return nil, fmt.Errorf("workspace list: %w", err)
	}
	listDecoded, _ := protocol.DecodeResult(protocol.MethodWorkspaceList, listRaw)
	list := listDecoded.(protocol.WorkspaceListResult)
	var selected protocol.WorkspaceID
	for _, item := range list.Items {
		if filepathEquivalent(item.DisplayPath, workspace) {
			selected = item.WorkspaceID
			break
		}
	}
	if selected == "" && len(list.Items) == 1 {
		selected = list.Items[0].WorkspaceID
	}
	if selected == "" {
		c.close(false)
		return nil, fmt.Errorf("workspace %q is not open on the Host", workspace)
	}
	c.mu.Lock()
	c.state.WorkspaceID = selected
	c.mu.Unlock()
	go c.leaseLoop(serveCtx, initialized.Lease.PingIntervalMs)
	return c, nil
}

func (c *Client) Generation() uint64 { return c.gen }

func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Client) SetCallbacks(callbacks Callbacks) {
	c.mu.Lock()
	c.callbacks = callbacks
	c.mu.Unlock()
}

// NotifyProviderCatalogChanged invalidates the Host-side Broker catalog after
// Desktop configuration or authorization changes. The Broker remains the sole
// owner of provider credentials and transport details.
func (c *Client) NotifyProviderCatalogChanged() error {
	c.mu.Lock()
	closed, desktopBroker := c.closed, c.broker
	c.mu.Unlock()
	if closed || desktopBroker == nil {
		return fmt.Errorf("Remote provider broker is unavailable")
	}
	return desktopBroker.NotifyCatalogChanged()
}

// SelectSession adopts a Host-advertised session before Subscribe. It is used
// after transport reconnect so the detached runtime and transcript are reused
// instead of silently creating a fresh conversation.
func (c *Client) SelectSession(target protocol.RuntimeTarget, runtimeEpoch protocol.RuntimeEpoch) error {
	if err := target.Validate(); err != nil || runtimeEpoch == "" {
		return fmt.Errorf("invalid Remote session selection")
	}
	c.projectionMu.Lock()
	defer c.projectionMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || !c.state.Initialized {
		return fmt.Errorf("client closed")
	}
	if target.WorkspaceID != c.state.WorkspaceID {
		return fmt.Errorf("Remote session belongs to a different workspace")
	}
	c.state.Target = target
	c.state.RuntimeEpoch = runtimeEpoch
	c.state.SubscriptionID = ""
	c.state.SnapshotID = ""
	c.state.CurrentTurnID = ""
	c.authorityRev++
	return nil
}

type requestAuthority struct {
	revision uint64
}

// Request validates the frozen result DTO and automatically adds authority
// fields from the client's current state. React callers never construct epochs,
// targets, leases, or request IDs themselves.
func (c *Client) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c == nil || c.conn == nil {
		return nil, fmt.Errorf("client closed")
	}
	name := protocol.Method(method)
	if name == protocol.MethodSessionSubscribe {
		c.subscribeMu.Lock()
		defer c.subscribeMu.Unlock()
	}
	for {
		authorized, authority, err := c.authorizeRequest(name, params)
		if err != nil {
			return nil, err
		}
		raw, err := c.conn.Request(ctx, method, authorized)
		if err != nil {
			return nil, err
		}
		decoded, err := protocol.DecodeResult(name, raw)
		if err != nil {
			return nil, fmt.Errorf("%s result: %w", method, err)
		}
		if c.applyRequestResultProjected(name, decoded, authority) {
			return raw, nil
		}
		result, ok := decoded.(protocol.SessionSubscribeResult)
		if !ok {
			return nil, fmt.Errorf("stale Remote result for %s", method)
		}
		// A session mutation completed while this snapshot was in flight. Remove
		// the unused server-side subscription, then retry under the now-current
		// authority while subscribeMu keeps competing refreshes serialized.
		_, _ = c.conn.Request(ctx, string(protocol.MethodSessionUnsubscribe), protocol.SessionUnsubscribeParams{SubscriptionID: result.SubscriptionID})
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
}

func (c *Client) CreateSession(ctx context.Context, model, effort string) (protocol.SessionCreateResult, error) {
	profile := protocol.ProfileSelection{}
	if strings.TrimSpace(model) != "" {
		profile.Model = stringPtr(strings.TrimSpace(model))
	}
	if strings.TrimSpace(effort) != "" {
		profile.Effort = stringPtr(strings.TrimSpace(effort))
	}
	raw, err := c.Request(ctx, string(protocol.MethodSessionCreate), protocol.SessionCreateParams{
		AdditionalDirectoryRefs: []protocol.DirectoryRef{}, Topic: protocol.TopicSelection{Kind: protocol.TopicNew}, Profile: profile,
	})
	if err != nil {
		return protocol.SessionCreateResult{}, err
	}
	decoded, _ := protocol.DecodeResult(protocol.MethodSessionCreate, raw)
	return decoded.(protocol.SessionCreateResult), nil
}

func (c *Client) Subscribe(ctx context.Context, pageTurns int) (protocol.SessionSubscribeResult, error) {
	if pageTurns <= 0 {
		pageTurns = protocol.HistoryMaxTurns
	}
	raw, err := c.Request(ctx, string(protocol.MethodSessionSubscribe), protocol.SessionSubscribeParams{PageTurns: pageTurns})
	if err != nil {
		return protocol.SessionSubscribeResult{}, err
	}
	decoded, _ := protocol.DecodeResult(protocol.MethodSessionSubscribe, raw)
	return decoded.(protocol.SessionSubscribeResult), nil
}

// SubscribeCommitted keeps session notifications and authority-changing
// results behind the snapshot commit. The caller therefore cannot publish an
// older snapshot after a notification or mutation result has already advanced
// its local projection.
func (c *Client) SubscribeCommitted(ctx context.Context, pageTurns int, commit func(protocol.SessionSubscribeResult) error) (protocol.SessionSubscribeResult, error) {
	if commit == nil {
		return protocol.SessionSubscribeResult{}, fmt.Errorf("snapshot commit required")
	}
	c.projectionMu.Lock()
	defer c.projectionMu.Unlock()
	result, err := c.Subscribe(ctx, pageTurns)
	if err != nil {
		return protocol.SessionSubscribeResult{}, err
	}
	if !c.IsCurrentSnapshot(result) {
		return protocol.SessionSubscribeResult{}, fmt.Errorf("stale Remote snapshot")
	}
	if err := commit(result); err != nil {
		return protocol.SessionSubscribeResult{}, err
	}
	return result, nil
}

func (c *Client) Submit(ctx context.Context, input string) (protocol.SessionSubmitResult, error) {
	raw, err := c.Request(ctx, string(protocol.MethodSessionSubmit), protocol.SessionSubmitParams{Input: input, DisplayText: input})
	if err != nil {
		return protocol.SessionSubmitResult{}, err
	}
	decoded, _ := protocol.DecodeResult(protocol.MethodSessionSubmit, raw)
	return decoded.(protocol.SessionSubmitResult), nil
}

func (c *Client) Cancel(ctx context.Context) (protocol.TurnCancelResult, error) {
	raw, err := c.Request(ctx, string(protocol.MethodTurnCancel), protocol.TurnCancelParams{})
	if err != nil {
		return protocol.TurnCancelResult{}, err
	}
	decoded, _ := protocol.DecodeResult(protocol.MethodTurnCancel, raw)
	return decoded.(protocol.TurnCancelResult), nil
}

func (c *Client) SetProfile(ctx context.Context, patch protocol.ProfilePatch) (protocol.SessionProfileSetResult, error) {
	raw, err := c.Request(ctx, string(protocol.MethodSessionProfileSet), protocol.SessionProfileSetParams{Patch: patch})
	if err != nil {
		return protocol.SessionProfileSetResult{}, err
	}
	decoded, _ := protocol.DecodeResult(protocol.MethodSessionProfileSet, raw)
	return decoded.(protocol.SessionProfileSetResult), nil
}

func (c *Client) History(ctx context.Context, pageTurns int) (protocol.HistoryPage, error) {
	return c.HistoryBefore(ctx, 0, pageTurns)
}

func (c *Client) HistoryBefore(ctx context.Context, beforeTurn, pageTurns int) (protocol.HistoryPage, error) {
	if pageTurns <= 0 {
		pageTurns = protocol.HistoryMaxTurns
	}
	cursor := protocol.Cursor("")
	if beforeTurn > 0 {
		cursor = protocol.Cursor(fmt.Sprintf("turn:%d", beforeTurn))
	}
	raw, err := c.Request(ctx, string(protocol.MethodSessionHistory), protocol.SessionHistoryParams{Cursor: cursor, PageTurns: pageTurns})
	if err != nil {
		return protocol.HistoryPage{}, err
	}
	decoded, _ := protocol.DecodeResult(protocol.MethodSessionHistory, raw)
	return decoded.(protocol.HistoryPage), nil
}

func (c *Client) authorizeParams(method protocol.Method, params any) (any, error) {
	value, _, err := c.authorizeRequest(method, params)
	return value, err
}

func (c *Client) authorizeRequest(method protocol.Method, params any) (any, requestAuthority, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, requestAuthority{}, err
	}
	fields := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, requestAuthority{}, fmt.Errorf("%s params must be an object", method)
		}
	}
	c.mu.Lock()
	state := c.state
	authority := requestAuthority{revision: c.authorityRev}
	c.mu.Unlock()
	set := func(key string, value any) { fields[key] = value }
	putRequestID := func() {
		if existing, ok := fields["requestId"].(string); ok && strings.TrimSpace(existing) != "" {
			return
		}
		fields["requestId"] = protocol.RequestID("request_" + randomID(10))
	}
	spec, ok := protocol.LookupMethod(method)
	if !ok {
		return nil, requestAuthority{}, fmt.Errorf("unregistered Remote method %q", method)
	}
	switch method {
	case protocol.MethodRemotePing, protocol.MethodRemoteDetach:
		set("leaseId", state.LeaseID)
	case protocol.MethodSessionUnsubscribe:
		set("subscriptionId", state.SubscriptionID)
	default:
		if spec.Class != protocol.ClassConnection {
			set("expectedHostEpoch", state.HostEpoch)
		}
		if spec.Class == protocol.ClassHostMutation || spec.Class == protocol.ClassSessionMutation || spec.Class == protocol.ClassSessionRecordMutation {
			putRequestID()
		}
		if spec.Class == protocol.ClassSessionQuery || spec.Class == protocol.ClassSessionMutation || spec.Class == protocol.ClassSessionRecordMutation {
			set("target", state.Target)
		}
		if spec.Class == protocol.ClassSessionQuery || spec.Class == protocol.ClassSessionMutation {
			if method != protocol.MethodSessionSubscribe {
				set("expectedRuntimeEpoch", state.RuntimeEpoch)
			}
		}
		if method == protocol.MethodSessionSubscribe {
			set("replaceSubscriptionId", state.SubscriptionID)
		}
		if method == protocol.MethodSessionCreate || method == protocol.MethodSessionList || method == protocol.MethodCatalogWorkspace {
			set("workspaceId", state.WorkspaceID)
		}
		if method == protocol.MethodSessionHistory {
			set("snapshotId", state.SnapshotID)
			if _, exists := fields["cursor"]; !exists {
				set("cursor", "")
			}
		}
		if method == protocol.MethodTurnCancel {
			set("expectedTurnId", state.CurrentTurnID)
		}
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, requestAuthority{}, err
	}
	decoded, err := protocol.DecodeRequestParams(method, encoded)
	if err != nil {
		return nil, requestAuthority{}, fmt.Errorf("%s params: %w", method, err)
	}
	return decoded, authority, nil
}

func (c *Client) applyResult(method protocol.Method, value any) {
	c.mu.Lock()
	authority := requestAuthority{revision: c.authorityRev}
	c.mu.Unlock()
	c.applyRequestResultProjected(method, value, authority)
}

func (c *Client) applyRequestResultProjected(method protocol.Method, value any, authority requestAuthority) bool {
	if requestInvalidatesSnapshot(method, value) {
		c.projectionMu.Lock()
		defer c.projectionMu.Unlock()
	}
	return c.applyRequestResult(method, value, authority)
}

func (c *Client) applyRequestResult(method protocol.Method, value any, authority requestAuthority) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := value.(protocol.SessionSubscribeResult); ok && authority.revision != c.authorityRev {
		return false
	}
	if requestInvalidatesSnapshot(method, value) {
		c.authorityRev++
	}
	switch result := value.(type) {
	case protocol.SessionCreateResult:
		c.state.Target, c.state.RuntimeEpoch, c.state.ResolvedProfile = result.Target, result.RuntimeEpoch, result.ResolvedProfile
	case protocol.SessionSubscribeResult:
		c.state.SubscriptionID = result.SubscriptionID
		c.state.SnapshotID = result.Snapshot.SnapshotID
		c.state.Target = result.Snapshot.Target
		c.state.RuntimeEpoch = result.Snapshot.RuntimeEpoch
		c.state.ResolvedProfile = result.Snapshot.Meta.ResolvedProfile
		c.state.CurrentTurnID = ""
		if result.Snapshot.Runtime.CurrentTurn != nil {
			c.state.CurrentTurnID = result.Snapshot.Runtime.CurrentTurn.TurnID
		}
		c.snapshotRev = c.authorityRev
	case protocol.SessionSubmitResult:
		c.state.Target, c.state.RuntimeEpoch = result.Target, result.RuntimeEpoch
		if _, completed := c.completedTurns[result.TurnID]; !completed {
			c.state.CurrentTurnID = result.TurnID
		}
	case protocol.SessionNewResult:
		c.state.Target, c.state.RuntimeEpoch = result.Target, result.RuntimeEpoch
		c.state.SnapshotID, c.state.CurrentTurnID = "", ""
	case protocol.SessionClearResult:
		c.state.Target, c.state.RuntimeEpoch = result.Target, result.RuntimeEpoch
		c.state.SnapshotID, c.state.CurrentTurnID = "", ""
	case protocol.SessionProfileSetResult:
		c.state.RuntimeEpoch, c.state.ResolvedProfile = result.RuntimeEpoch, result.ResolvedProfile
	case protocol.SessionCloseResult:
		if result.Disposition != protocol.SessionRetainedActive {
			c.state.Target, c.state.RuntimeEpoch, c.state.SubscriptionID, c.state.CurrentTurnID = protocol.RuntimeTarget{}, "", "", ""
		}
	}
	return true
}

func requestInvalidatesSnapshot(method protocol.Method, value any) bool {
	if spec, ok := protocol.LookupMethod(method); ok {
		switch spec.Class {
		case protocol.ClassHostMutation, protocol.ClassSessionMutation, protocol.ClassSessionRecordMutation:
			return true
		}
	}
	switch value.(type) {
	case protocol.SessionCreateResult, protocol.SessionSubmitResult, protocol.SessionNewResult,
		protocol.SessionClearResult, protocol.SessionProfileSetResult, protocol.SessionCloseResult:
		return true
	default:
		return false
	}
}

// IsCurrentSnapshot reports whether a subscribe result still represents the
// client's live authority after the network request returned.
func (c *Client) IsCurrentSnapshot(result protocol.SessionSubscribeResult) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snapshotRev != c.authorityRev ||
		c.state.SubscriptionID != result.SubscriptionID ||
		c.state.Target != result.Snapshot.Target ||
		c.state.RuntimeEpoch != result.Snapshot.RuntimeEpoch ||
		c.state.ResolvedProfile != result.Snapshot.Meta.ResolvedProfile {
		return false
	}
	turnID := protocol.TurnID("")
	if result.Snapshot.Runtime.CurrentTurn != nil {
		turnID = result.Snapshot.Runtime.CurrentTurn.TurnID
	}
	return c.state.CurrentTurnID == turnID
}

func (c *Client) handleSessionEvent(_ context.Context, raw json.RawMessage) {
	decoded, err := protocol.DecodeNotificationParams(protocol.MethodSessionEvent, raw)
	if err == nil {
		c.enqueue(decoded.(protocol.SessionEvent))
	}
}

func (c *Client) handleResync(_ context.Context, raw json.RawMessage) {
	decoded, err := protocol.DecodeNotificationParams(protocol.MethodSessionResyncRequired, raw)
	if err == nil {
		c.enqueue(decoded.(protocol.SessionResyncRequired))
	}
}

func (c *Client) handleCatalogChanged(_ context.Context, raw json.RawMessage) {
	decoded, err := protocol.DecodeNotificationParams(protocol.MethodCatalogChanged, raw)
	if err == nil {
		c.enqueue(decoded.(protocol.CatalogChanged))
	}
}

func (c *Client) enqueue(value any) {
	select {
	case c.notifyCh <- value:
	default:
		// The queue is already full, so another write to it cannot reliably carry
		// the resync signal. Deliver one out-of-band signal per overflow burst.
		if !c.overflowed.CompareAndSwap(false, true) {
			return
		}
		state := c.State()
		c.mu.Lock()
		callback := c.callbacks.OnResyncRequired
		c.mu.Unlock()
		if callback == nil {
			c.overflowed.Store(false)
			return
		}
		go func() {
			defer c.overflowed.Store(false)
			c.projectionMu.Lock()
			defer c.projectionMu.Unlock()
			state = c.State()
			c.mu.Lock()
			callback = c.callbacks.OnResyncRequired
			c.mu.Unlock()
			if callback != nil {
				callback(protocol.SessionResyncRequired{
					SubscriptionID: state.SubscriptionID, HostEpoch: state.HostEpoch, Target: state.Target,
					RuntimeEpoch: state.RuntimeEpoch, Reason: protocol.ResyncQueueOverflow,
				})
			}
		}()
	}
}

func (c *Client) deliveryLoop(ctx context.Context) {
	pending := map[uint64]protocol.SessionEvent{}
	var next uint64 = 1
	var activeSubscription protocol.SubscriptionID
	for {
		select {
		case <-ctx.Done():
			return
		case value := <-c.notifyCh:
			switch notification := value.(type) {
			case protocol.SessionEvent:
				c.projectionMu.Lock()
				func() {
					defer c.projectionMu.Unlock()
					state := c.State()
					if notification.SubscriptionID != state.SubscriptionID {
						return
					}
					if activeSubscription != notification.SubscriptionID {
						activeSubscription = notification.SubscriptionID
						pending = map[uint64]protocol.SessionEvent{}
						next = 1
					}
					pending[notification.Seq] = notification
					for {
						event, ok := pending[next]
						if !ok {
							break
						}
						delete(pending, next)
						next++
						c.mu.Lock()
						if event.Event.Kind == "turn_done" {
							c.completedTurns[event.TurnID] = struct{}{}
							c.state.CurrentTurnID = ""
						}
						callback := c.callbacks.OnSessionEvent
						c.mu.Unlock()
						if callback != nil {
							callback(event)
						}
					}
				}()
			case protocol.SessionResyncRequired:
				c.projectionMu.Lock()
				pending = map[uint64]protocol.SessionEvent{}
				next = notification.LastSeq + 1
				c.mu.Lock()
				callback := c.callbacks.OnResyncRequired
				c.mu.Unlock()
				if callback != nil {
					callback(notification)
				}
				c.projectionMu.Unlock()
			case protocol.CatalogChanged:
				c.mu.Lock()
				callback := c.callbacks.OnCatalogChanged
				c.mu.Unlock()
				if callback != nil {
					callback(notification)
				}
			}
		}
	}
}

func (c *Client) leaseLoop(ctx context.Context, intervalMillis int) {
	if intervalMillis <= 0 {
		return
	}
	ticker := time.NewTicker(time.Duration(intervalMillis) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state := c.State()
			raw, err := c.conn.Request(ctx, string(protocol.MethodRemotePing), protocol.PingParams{LeaseID: state.LeaseID})
			if err == nil {
				var decoded any
				decoded, err = protocol.DecodeResult(protocol.MethodRemotePing, raw)
				if err == nil && decoded.(protocol.PingResult).HostEpoch != state.HostEpoch {
					err = fmt.Errorf("Host epoch changed during lease renewal")
				}
			}
			if err != nil {
				c.close(false)
				return
			}
		}
	}
}

func (c *Client) Close() { c.close(true) }

func (c *Client) close(explicit bool) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	lease := c.state.LeaseID
	established := c.state.Initialized
	onDisconnected := c.callbacks.OnDisconnected
	c.mu.Unlock()
	if explicit && c.conn != nil && lease != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = c.conn.Request(ctx, string(protocol.MethodRemoteDetach), protocol.DetachParams{LeaseID: lease})
		cancel()
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.broker != nil {
		c.broker.Close()
	}
	if c.stream != nil {
		_ = c.stream.Close()
	}
	if !explicit && established && onDisconnected != nil {
		onDisconnected()
	}
}

func NextGen(counter *atomic.Uint64) uint64 { return counter.Add(1) }

func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func stringPtr(value string) *string { return &value }

func filepathEquivalent(left, right string) bool {
	clean := func(value string) string {
		return strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"), "/")
	}
	return clean(left) == clean(right)
}
