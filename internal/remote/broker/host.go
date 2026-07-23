package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

// Host is the credential-free provider resolver owned by a long-lived remote
// workspace runtime. Attach replaces its transport generation atomically;
// controllers can survive an SSH reconnect and use the next generation on
// their following provider call.
type Host struct {
	mu sync.Mutex

	conn       *rpcwire.Conn
	generation uint64
	catalog    []provider.Descriptor
	streams    map[string]*hostStream
}

type hostStream struct {
	generation    uint64
	conn          *rpcwire.Conn
	out           chan provider.Chunk
	done          chan struct{}
	abortDelivery chan struct{}
	nextSeq       int64
	pending       map[int64]provider.Chunk
	ended         bool
	endSeq        int64
	endError      string
	interrupted   bool
	gapTimer      bool
	closeOnce     sync.Once
	delivery      []provider.Chunk
	deliveryWake  chan struct{}
	deliveryFinal bool
}

// Keep delivery bounded without ever applying output backpressure while the
// Host-wide mutex is held. One slot is reserved for a terminal error.
const hostDeliveryQueueLimit = 256

func NewHost() *Host { return &Host{streams: make(map[string]*hostStream)} }

// Bind registers one prospective Desktop connection without granting it Broker
// ownership. Runtime connections use this split phase so a socket probe or a
// failed initialize cannot revoke the currently committed Desktop capability.
// When ready is non-nil, notifications wait until the runtime has committed or
// rejected the connection; generation checks discard rejected/stale traffic.
func (h *Host) Bind(conn *rpcwire.Conn, generation uint64, ready <-chan struct{}) error {
	if h == nil || conn == nil {
		return fmt.Errorf("broker host: connection required")
	}
	wait := func(ctx context.Context) bool {
		if ready == nil {
			return true
		}
		select {
		case <-ready:
			return true
		case <-ctx.Done():
			return false
		}
	}
	conn.HandleNotify(string(protocol.MethodBrokerStreamChunk), func(ctx context.Context, raw json.RawMessage) {
		if wait(ctx) {
			h.handleChunk(generation, raw)
		}
	})
	conn.HandleNotify(string(protocol.MethodBrokerStreamEnd), func(ctx context.Context, raw json.RawMessage) {
		if wait(ctx) {
			h.handleEnd(generation, raw)
		}
	})
	conn.HandleNotify(string(protocol.MethodBrokerCatalogChanged), func(ctx context.Context, raw json.RawMessage) {
		if wait(ctx) {
			h.handleCatalogChanged(generation, raw)
		}
	})
	return nil
}

// Activate grants a successfully initialized connection Broker ownership. Any
// stream still owned by the previous connection is completed as interrupted
// instead of being silently spliced across generations.
func (h *Host) Activate(conn *rpcwire.Conn, generation uint64) error {
	if h == nil || conn == nil {
		return fmt.Errorf("broker host: connection required")
	}
	h.mu.Lock()
	if generation <= h.generation {
		h.mu.Unlock()
		return fmt.Errorf("broker host: stale generation %d", generation)
	}
	for id, stream := range h.streams {
		if stream.generation != generation {
			h.finishLocked(id, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: errors.New("provider broker connection replaced")}})
		}
	}
	h.conn = conn
	h.generation = generation
	h.catalog = nil
	h.mu.Unlock()
	return nil
}

// Attach is the single-phase helper used by callers that do not need a
// provisional handshake. Runtime server connections use Bind then Activate.
func (h *Host) Attach(conn *rpcwire.Conn, generation uint64) error {
	if err := h.Bind(conn, generation, nil); err != nil {
		return err
	}
	return h.Activate(conn, generation)
}

// Detach removes ownership only when generation is still current.
func (h *Host) Detach(generation uint64) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.generation != generation {
		return
	}
	for id, stream := range h.streams {
		h.finishLocked(id, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: errors.New("provider broker disconnected")}})
	}
	h.conn = nil
	h.catalog = nil
}

func (h *Host) Catalog() []provider.Descriptor {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, _ := h.catalogFor(ctx)
	return list
}

func (h *Host) catalogFor(ctx context.Context) ([]provider.Descriptor, error) {
	h.mu.Lock()
	if len(h.catalog) > 0 {
		out := append([]provider.Descriptor(nil), h.catalog...)
		h.mu.Unlock()
		return out, nil
	}
	conn := h.conn
	h.mu.Unlock()
	if conn == nil {
		return nil, fmt.Errorf("provider broker is not attached")
	}
	raw, err := conn.Request(ctx, string(protocol.MethodBrokerCatalog), protocol.BrokerCatalogParams{})
	if err != nil {
		return nil, fmt.Errorf("provider broker catalog: %w", err)
	}
	var result protocol.BrokerCatalogResult
	if err := decodeBrokerResult(protocol.MethodBrokerCatalog, raw, &result); err != nil {
		return nil, err
	}
	out := make([]provider.Descriptor, 0, len(result.Providers))
	for _, item := range result.Providers {
		out = append(out, provider.Descriptor{
			Ref: item.Ref, DisplayName: item.DisplayName, Model: item.Model,
			ContextWindow: item.ContextWindow, PricingCurrency: item.PricingCurrency,
			CacheHitPerMillion: item.CacheHitPerMillion, InputPerMillion: item.InputPerMillion, OutputPerMillion: item.OutputPerMillion,
			Vision: item.SupportsVision, Tools: true,
			Reasoning:                      len(item.SupportedEfforts) > 0 || item.ToolCallReasoning,
			Efforts:                        append([]string(nil), item.SupportedEfforts...),
			DefaultEffort:                  item.DefaultEffort,
			ToolCallReasoning:              item.ToolCallReasoning,
			WarnOnMissingToolCallReasoning: item.WarnOnMissingToolCallReasoning,
		})
	}
	h.mu.Lock()
	if h.conn == conn {
		h.catalog = append([]provider.Descriptor(nil), out...)
	}
	h.mu.Unlock()
	return out, nil
}

func (h *Host) Resolve(selection provider.Selection) (provider.Provider, error) {
	ref := strings.TrimSpace(selection.Ref)
	if ref == "" {
		return nil, fmt.Errorf("provider selection ref is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	catalog, err := h.catalogFor(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range catalog {
		if item.Ref == ref || item.DisplayName == ref || item.Model == ref || strings.HasPrefix(item.Ref, ref+"/") {
			return &hostProvider{host: h, ref: item.Ref, effort: selection.Effort, descriptor: item}, nil
		}
	}
	return nil, fmt.Errorf("provider %q is not authorized by the Desktop Broker", ref)
}

type hostProvider struct {
	host       *Host
	ref        string
	effort     *string
	descriptor provider.Descriptor
}

func (p *hostProvider) Name() string {
	if p == nil {
		return "remote"
	}
	if i := strings.IndexByte(p.ref, '/'); i > 0 {
		return p.ref[:i]
	}
	return p.ref
}

func (p *hostProvider) RequiresToolCallReasoning() bool {
	return p != nil && p.descriptor.ToolCallReasoning
}

func (p *hostProvider) WarnOnMissingToolCallReasoning() bool {
	return p != nil && p.descriptor.WarnOnMissingToolCallReasoning
}

func (p *hostProvider) Stream(ctx context.Context, request provider.Request) (<-chan provider.Chunk, error) {
	if p == nil || p.host == nil {
		return nil, fmt.Errorf("provider broker is unavailable")
	}
	return p.host.open(ctx, p.ref, p.effort, request)
}

func (h *Host) open(ctx context.Context, ref string, effort *string, request provider.Request) (<-chan provider.Chunk, error) {
	h.mu.Lock()
	conn, generation := h.conn, h.generation
	if conn == nil {
		h.mu.Unlock()
		return nil, fmt.Errorf("provider broker is not attached")
	}
	id := "bs_" + randomID(12)
	stream := &hostStream{
		generation: generation, conn: conn,
		out: make(chan provider.Chunk, 64), done: make(chan struct{}), abortDelivery: make(chan struct{}),
		deliveryWake: make(chan struct{}, 1),
		nextSeq:      1, pending: make(map[int64]provider.Chunk),
	}
	h.streams[id] = stream
	h.mu.Unlock()
	go h.deliverStream(stream)

	effortValue := ""
	if effort != nil {
		effortValue = *effort
	}
	raw, err := conn.Request(ctx, string(protocol.MethodBrokerStreamOpen), protocol.BrokerStreamOpenParams{
		StreamID: id, ProviderRef: ref,
		Request: protocol.BrokerProviderRequestFromProvider(request), Effort: effortValue,
	})
	if err != nil {
		h.removeStream(id, stream)
		go requestStreamCancel(conn, id)
		return nil, fmt.Errorf("provider broker open: %w", err)
	}
	var opened protocol.BrokerStreamOpenResult
	if err := decodeBrokerResult(protocol.MethodBrokerStreamOpen, raw, &opened); err != nil {
		h.removeStream(id, stream)
		go requestStreamCancel(conn, id)
		return nil, err
	}
	if !opened.Accepted {
		h.removeStream(id, stream)
		go requestStreamCancel(conn, id)
		return nil, fmt.Errorf("provider broker declined stream")
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-stream.done:
			return
		}
		h.mu.Lock()
		current := h.streams[id]
		h.mu.Unlock()
		if current != stream {
			return
		}
		h.mu.Lock()
		if h.streams[id] == stream {
			h.abortDeliveryLocked(stream)
			h.finishLocked(id, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: ctx.Err()}})
		}
		h.mu.Unlock()
		requestStreamCancel(conn, id)
	}()
	return stream.out, nil
}

func (h *Host) handleChunk(generation uint64, raw json.RawMessage) {
	decoded, err := protocol.DecodeBrokerNotificationParams(protocol.MethodBrokerStreamChunk, raw)
	if err != nil {
		return
	}
	p := decoded.(protocol.BrokerStreamChunkParams)
	h.mu.Lock()
	defer h.mu.Unlock()
	stream := h.streams[p.StreamID]
	if generation != h.generation || stream == nil || stream.generation != generation || p.Seq < stream.nextSeq {
		return
	}
	stream.pending[p.Seq] = p.Chunk.ProviderChunk()
	h.flushLocked(p.StreamID, stream)
}

func (h *Host) handleEnd(generation uint64, raw json.RawMessage) {
	decoded, err := protocol.DecodeBrokerNotificationParams(protocol.MethodBrokerStreamEnd, raw)
	if err != nil {
		return
	}
	p := decoded.(protocol.BrokerStreamEndParams)
	h.mu.Lock()
	stream := h.streams[p.StreamID]
	if generation != h.generation || stream == nil || stream.generation != generation {
		h.mu.Unlock()
		return
	}
	stream.ended = true
	stream.endSeq = p.LastSeq
	stream.endError = p.Error
	stream.interrupted = p.Interrupted
	h.flushLocked(p.StreamID, stream)
	if h.streams[p.StreamID] == stream && stream.nextSeq <= stream.endSeq && !stream.gapTimer {
		stream.gapTimer = true
		go h.expireGap(p.StreamID, stream)
	}
	h.mu.Unlock()
}

func (h *Host) handleCatalogChanged(generation uint64, raw json.RawMessage) {
	if _, err := protocol.DecodeBrokerNotificationParams(protocol.MethodBrokerCatalogChanged, raw); err != nil {
		return
	}
	h.mu.Lock()
	if generation == h.generation {
		h.catalog = nil
	}
	h.mu.Unlock()
}

func (h *Host) flushLocked(id string, stream *hostStream) {
	for {
		chunk, ok := stream.pending[stream.nextSeq]
		if !ok {
			break
		}
		delete(stream.pending, stream.nextSeq)
		stream.nextSeq++
		if !h.enqueueDeliveryLocked(stream, chunk) {
			h.finishLocked(id, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: errors.New("provider broker stream output overflow")}})
			return
		}
	}
	if !stream.ended || stream.nextSeq <= stream.endSeq {
		return
	}
	if stream.endError != "" || stream.interrupted {
		err := errors.New("local provider stream failed")
		if stream.interrupted {
			err = &provider.StreamInterruptedError{Err: errors.New("local provider stream was interrupted")}
		}
		h.finishLocked(id, stream, provider.Chunk{Type: provider.ChunkError, Err: err})
		return
	}
	h.finishLocked(id, stream, provider.Chunk{})
}

func (h *Host) finishLocked(id string, stream *hostStream, terminal provider.Chunk) {
	if h.streams[id] != stream {
		return
	}
	delete(h.streams, id)
	stream.closeOnce.Do(func() {
		if terminal.Err != nil || terminal.Type != 0 {
			stream.delivery = append(stream.delivery, terminal)
		}
		stream.deliveryFinal = true
		close(stream.done)
		h.signalDeliveryLocked(stream)
	})
}

func (h *Host) removeStream(id string, stream *hostStream) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.streams[id] == stream {
		h.abortDeliveryLocked(stream)
		h.finishLocked(id, stream, provider.Chunk{})
	}
}

func (h *Host) abortDeliveryLocked(stream *hostStream) {
	if stream.abortDelivery == nil {
		return
	}
	select {
	case <-stream.abortDelivery:
	default:
		close(stream.abortDelivery)
	}
}

func (h *Host) enqueueDeliveryLocked(stream *hostStream, chunk provider.Chunk) bool {
	if stream.deliveryFinal || len(stream.delivery) >= hostDeliveryQueueLimit-1 {
		return false
	}
	stream.delivery = append(stream.delivery, chunk)
	h.signalDeliveryLocked(stream)
	return true
}

func (h *Host) signalDeliveryLocked(stream *hostStream) {
	select {
	case stream.deliveryWake <- struct{}{}:
	default:
	}
}

// deliverStream is the sole sender and closer of stream.out. It may wait for a
// slow provider consumer, but never while holding h.mu, so reconnect, detach,
// cancellation, gap expiry, and unrelated streams continue to make progress.
func (h *Host) deliverStream(stream *hostStream) {
	defer close(stream.out)
	for {
		h.mu.Lock()
		if len(stream.delivery) > 0 {
			chunk := stream.delivery[0]
			stream.delivery[0] = provider.Chunk{}
			stream.delivery = stream.delivery[1:]
			h.mu.Unlock()
			select {
			case stream.out <- chunk:
			case <-stream.abortDelivery:
				return
			}
			continue
		}
		if stream.deliveryFinal {
			h.mu.Unlock()
			return
		}
		wake := stream.deliveryWake
		h.mu.Unlock()
		select {
		case <-wake:
		case <-stream.abortDelivery:
			return
		}
	}
}

func requestStreamCancel(conn *rpcwire.Conn, id string) {
	if conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = conn.Request(ctx, string(protocol.MethodBrokerStreamCancel), protocol.BrokerStreamCancelParams{StreamID: id})
}

func (h *Host) expireGap(id string, stream *hostStream) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-stream.done:
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.streams[id] != stream || !stream.ended || stream.nextSeq > stream.endSeq {
		return
	}
	h.finishLocked(id, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: fmt.Errorf("provider broker stream missing chunk %d of %d", stream.nextSeq, stream.endSeq)}})
}

func decodeBrokerResult(method protocol.Method, raw json.RawMessage, out any) error {
	decoded, err := protocol.DecodeBrokerResult(method, raw)
	if err != nil {
		return fmt.Errorf("provider broker %s result: %w", method, err)
	}
	b, err := json.Marshal(decoded)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return err
	}
	return nil
}

func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
