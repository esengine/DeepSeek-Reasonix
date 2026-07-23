// Package broker implements the Desktop side of the bidirectional Provider
// Broker over rpcwire. Host → Desktop requests open catalog/stream; Desktop →
// Host notifications deliver chunks. API keys never leave this process.
package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

// CatalogSource returns non-secret descriptors authorized for a Host scope.
type CatalogSource func(ctx context.Context, allowed map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error)

// StreamOpener resolves a provider ref and starts a stream. Implementations
// must use the local Provider (keys on Desktop only).
type StreamOpener func(ctx context.Context, ref, effort string, req provider.Request) (<-chan provider.Chunk, error)

// Desktop is the Desktop-side Broker endpoint bound to one SSH/rpcwire connection.
type Desktop struct {
	conn      *rpcwire.Conn
	catalog   CatalogSource
	open      StreamOpener
	authorize func() error

	mu      sync.Mutex
	streams map[string]*streamState
	// maxConcurrent bounds simultaneous provider streams per connection.
	maxConcurrent int
	// gen increments on catalog-changed notifications.
	gen             atomic.Int64
	active          atomic.Bool
	closeOnce       sync.Once
	lifecycle       context.Context
	cancelLifecycle context.CancelFunc
	// closed is closed when the connection is torn down.
	closed chan struct{}
}

type streamState struct {
	cancel          context.CancelFunc
	seq             atomic.Int64
	cancelRequested atomic.Bool
}

const (
	ErrorNotReady         = -32050
	ErrorUnavailable      = -32051
	ErrorConcurrencyLimit = -32052
)

// Options configures a Desktop Broker endpoint.
type Options struct {
	Catalog CatalogSource
	Open    StreamOpener
	// Authorize runs immediately before the endpoint becomes active. Desktop
	// transports use it to bind the Broker capability to the authenticated SSH
	// peer; returning an error keeps every Broker method unavailable.
	Authorize     func() error
	MaxConcurrent int
}

// Attach registers Host-request handlers on conn and returns the Desktop Broker.
func Attach(conn *rpcwire.Conn, opts Options) (*Desktop, error) {
	if conn == nil {
		return nil, fmt.Errorf("broker: nil conn")
	}
	if opts.Catalog == nil || opts.Open == nil {
		return nil, fmt.Errorf("broker: catalog and open are required")
	}
	max := opts.MaxConcurrent
	if max <= 0 {
		max = 4
	}
	lifecycle, cancelLifecycle := context.WithCancel(context.Background())
	d := &Desktop{
		conn:            conn,
		catalog:         opts.Catalog,
		open:            opts.Open,
		authorize:       opts.Authorize,
		streams:         map[string]*streamState{},
		maxConcurrent:   max,
		closed:          make(chan struct{}),
		lifecycle:       lifecycle,
		cancelLifecycle: cancelLifecycle,
	}
	conn.Handle(string(protocol.MethodBrokerCatalog), d.handleCatalog)
	conn.Handle(string(protocol.MethodBrokerStreamOpen), d.handleStreamOpen)
	conn.Handle(string(protocol.MethodBrokerStreamCancel), d.handleStreamCancel)
	return d, nil
}

// Activate makes Broker methods available after the Remote initialize
// handshake and its protocol/schema checks have succeeded.
func (d *Desktop) Activate() error {
	if d.authorize != nil {
		if err := d.authorize(); err != nil {
			return fmt.Errorf("broker: authorization failed: %w", err)
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	select {
	case <-d.closed:
		return fmt.Errorf("broker: endpoint closed")
	default:
		d.active.Store(true)
		return nil
	}
}

// Close cancels all streams for this connection.
func (d *Desktop) Close() {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.active.Store(false)
		close(d.closed)
		d.cancelLifecycle()
		states := make([]*streamState, 0, len(d.streams))
		for id, st := range d.streams {
			states = append(states, st)
			delete(d.streams, id)
		}
		d.mu.Unlock()
		for _, st := range states {
			st.cancel()
		}
	})
}

// NotifyCatalogChanged pushes a generation bump after re-authorization.
func (d *Desktop) NotifyCatalogChanged() error {
	if err := d.requireActive(); err != nil {
		return err
	}
	gen := d.gen.Add(1)
	return d.conn.Notify(string(protocol.MethodBrokerCatalogChanged), protocol.BrokerCatalogChangedParams{
		Generation: gen,
	})
}

func (d *Desktop) handleCatalog(ctx context.Context, params json.RawMessage) (any, error) {
	if err := d.requireActive(); err != nil {
		return nil, err
	}
	decoded, err := protocol.DecodeBrokerRequestParams(protocol.MethodBrokerCatalog, params)
	if err != nil {
		return nil, &rpcwire.RPCError{Code: rpcwire.ErrInvalidParams, Message: "invalid catalog params"}
	}
	p := decoded.(protocol.BrokerCatalogParams)
	allowed := map[string]struct{}{}
	for _, ref := range p.AllowedRefs {
		if ref = strings.TrimSpace(ref); ref != "" {
			allowed[ref] = struct{}{}
		}
	}
	operationCtx, cancel := d.operationContext(ctx)
	defer cancel()
	list, err := d.catalog(operationCtx, allowed)
	if err != nil {
		return nil, &rpcwire.RPCError{Code: ErrorUnavailable, Message: "provider catalog unavailable"}
	}
	if list == nil {
		list = []protocol.BrokerProviderDescriptor{}
	}
	return protocol.BrokerCatalogResult{Providers: list}, nil
}

func (d *Desktop) handleStreamOpen(ctx context.Context, params json.RawMessage) (any, error) {
	if err := d.requireActive(); err != nil {
		return nil, err
	}
	decoded, err := protocol.DecodeBrokerRequestParams(protocol.MethodBrokerStreamOpen, params)
	if err != nil {
		return nil, &rpcwire.RPCError{Code: rpcwire.ErrInvalidParams, Message: "invalid stream open params"}
	}
	p := decoded.(protocol.BrokerStreamOpenParams)

	d.mu.Lock()
	select {
	case <-d.closed:
		d.mu.Unlock()
		return nil, &rpcwire.RPCError{Code: ErrorUnavailable, Message: "provider broker unavailable"}
	default:
	}
	if len(d.streams) >= d.maxConcurrent {
		d.mu.Unlock()
		return nil, &rpcwire.RPCError{Code: ErrorConcurrencyLimit, Message: "provider stream limit reached"}
	}
	if _, exists := d.streams[p.StreamID]; exists {
		d.mu.Unlock()
		return nil, &rpcwire.RPCError{Code: rpcwire.ErrInvalidParams, Message: "streamId already in use"}
	}
	streamCtx, cancel := context.WithCancel(d.lifecycle)
	st := &streamState{cancel: cancel}
	d.streams[p.StreamID] = st
	d.mu.Unlock()

	ch, err := d.open(streamCtx, p.ProviderRef, p.Effort, p.Request.ProviderRequest())
	if err != nil {
		d.finishStream(p.StreamID, st)
		return nil, &rpcwire.RPCError{Code: ErrorUnavailable, Message: "provider stream unavailable"}
	}
	if ch == nil {
		d.finishStream(p.StreamID, st)
		return nil, &rpcwire.RPCError{Code: ErrorUnavailable, Message: "provider stream unavailable"}
	}
	go d.pump(p.StreamID, st, ch)
	return protocol.BrokerStreamOpenResult{Accepted: true}, nil
}

func (d *Desktop) handleStreamCancel(ctx context.Context, params json.RawMessage) (any, error) {
	if err := d.requireActive(); err != nil {
		return nil, err
	}
	decoded, err := protocol.DecodeBrokerRequestParams(protocol.MethodBrokerStreamCancel, params)
	if err != nil {
		return nil, &rpcwire.RPCError{Code: rpcwire.ErrInvalidParams, Message: "invalid cancel params"}
	}
	p := decoded.(protocol.BrokerStreamCancelParams)
	d.mu.Lock()
	st, ok := d.streams[p.StreamID]
	d.mu.Unlock()
	if ok {
		st.cancelRequested.Store(true)
		st.cancel()
	}
	return protocol.BrokerStreamCancelResult{Cancelled: ok}, nil
}

func (d *Desktop) pump(streamID string, st *streamState, ch <-chan provider.Chunk) {
	defer d.finishStream(streamID, st)
	for {
		select {
		case <-d.closed:
			return
		case chunk, ok := <-ch:
			if !ok {
				_ = d.notifyEnd(streamID, st.seq.Load(), "", st.cancelRequested.Load())
				return
			}
			seq := st.seq.Add(1)
			err := d.conn.Notify(string(protocol.MethodBrokerStreamChunk), protocol.BrokerStreamChunkParams{
				StreamID: streamID,
				Seq:      seq,
				Chunk:    protocol.BrokerProviderChunkFromProvider(chunk),
			})
			if err != nil {
				return
			}
		}
	}
}

func (d *Desktop) notifyEnd(streamID string, lastSeq int64, errMsg string, interrupted bool) error {
	return d.conn.Notify(string(protocol.MethodBrokerStreamEnd), protocol.BrokerStreamEndParams{
		StreamID:    streamID,
		LastSeq:     lastSeq,
		Error:       errMsg,
		Interrupted: interrupted,
	})
}

func (d *Desktop) finishStream(id string, expected *streamState) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if st, ok := d.streams[id]; ok && st == expected {
		st.cancel()
		delete(d.streams, id)
	}
}

func (d *Desktop) requireActive() error {
	select {
	case <-d.closed:
		return &rpcwire.RPCError{Code: ErrorUnavailable, Message: "provider broker unavailable"}
	default:
	}
	if !d.active.Load() {
		return &rpcwire.RPCError{Code: ErrorNotReady, Message: "provider broker not ready"}
	}
	return nil
}

func (d *Desktop) operationContext(request context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(request)
	stop := context.AfterFunc(d.lifecycle, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// DescriptorFromProvider builds a non-secret catalog row from a live Provider
// and a configured model ref. Does not include base URLs or credentials.
func DescriptorFromProvider(ref, display, model string, p provider.Provider, efforts []string, defaultEffort string, vision bool, contextWindow int, pricing *provider.Pricing) protocol.BrokerProviderDescriptor {
	descriptor := protocol.BrokerProviderDescriptor{
		Ref:                            ref,
		DisplayName:                    display,
		Model:                          model,
		SupportsVision:                 vision,
		SupportedEfforts:               append([]string(nil), efforts...),
		DefaultEffort:                  defaultEffort,
		ToolCallReasoning:              provider.RequiresToolCallReasoning(p),
		WarnOnMissingToolCallReasoning: provider.WarnOnMissingToolCallReasoning(p),
		ContextWindow:                  contextWindow,
	}
	if pricing != nil {
		descriptor.PricingCurrency = pricing.Currency
		descriptor.CacheHitPerMillion = pricing.CacheHit
		descriptor.InputPerMillion = pricing.Input
		descriptor.OutputPerMillion = pricing.Output
	}
	return descriptor
}
