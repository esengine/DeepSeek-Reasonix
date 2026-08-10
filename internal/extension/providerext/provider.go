package providerext

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/providerconv"
	"reasonix/internal/provider"
)

// Provider is the host-side handle for one extension-hosted provider ref.
// Streams run on the owning sidecar, which holds the credentials: the open
// params carry only the request, the ref, the model, and the effort — the
// host never sends another provider's keys across the extension boundary.
// The effort from the resolving Selection is baked in at construction, the
// way boot bakes effort into local providers.
type Provider struct {
	resolver   *Resolver
	client     ProviderClient
	owner      string // plugin package id; used to re-resolve live backends
	ref        string
	effort     *string
	descriptor provider.Descriptor
}

var _ provider.Provider = (*Provider)(nil)

// Name returns the provider instance name: the ref's first segment, mirroring
// the broker's hostProvider ("plugin" for extension refs).
func (p *Provider) Name() string {
	if p == nil {
		return "extension"
	}
	if i := strings.IndexByte(p.ref, '/'); i > 0 {
		return p.ref[:i]
	}
	return p.ref
}

// RequiresToolCallReasoning reports the descriptor's replay policy, mirroring
// the broker's hostProvider.
func (p *Provider) RequiresToolCallReasoning() bool {
	return p != nil && p.descriptor.ToolCallReasoning
}

// RequiresReasoningRoundTrip reports the descriptor's round-trip policy.
func (p *Provider) RequiresReasoningRoundTrip() bool {
	return p != nil && p.descriptor.ReasoningRoundTrip
}

// WarnOnMissingToolCallReasoning reports the descriptor's warning policy.
func (p *Provider) WarnOnMissingToolCallReasoning() bool {
	return p != nil && p.descriptor.WarnOnMissingToolCallReasoning
}

// MissingToolCallReasoningWarningIdentity supplies the stable, non-credential
// configuration identity used to rate-limit missing-reasoning diagnostics.
func (p *Provider) MissingToolCallReasoningWarningIdentity() string {
	if p == nil {
		return ""
	}
	effort := ""
	if p.effort != nil {
		effort = strings.TrimSpace(*p.effort)
	}
	return strings.Join([]string{
		"extension-sidecar", strings.TrimSpace(p.client.PluginID()), strings.TrimSpace(p.ref),
		strings.TrimSpace(p.descriptor.Model), effort,
	}, "\x00")
}

// Stream opens one sidecar stream. Each call re-resolves the live backend so
// rolling replacement keeps the same provider-visible ref (cache-stable).
func (p *Provider) Stream(ctx context.Context, request provider.Request) (<-chan provider.Chunk, error) {
	if p == nil || p.resolver == nil {
		return nil, fmt.Errorf("extension provider is unavailable")
	}
	client := p.client
	if live := p.resolver.liveClient(p.owner); live != nil {
		client = live
	}
	if client == nil {
		return nil, fmt.Errorf("extension provider is unavailable")
	}
	return p.resolver.open(ctx, p, client, request)
}

// open registers the buffered stream, asks the sidecar to start it, and arms
// the cancellation/disconnect watcher. The seq buffering, delivery, and gap
// semantics mirror the broker's Host.open exactly.
func (r *Resolver) open(ctx context.Context, p *Provider, client ProviderClient, request provider.Request) (<-chan provider.Chunk, error) {
	if client.Crashed() {
		return nil, &provider.StreamInterruptedError{Err: fmt.Errorf("extension sidecar %s crashed", client.PluginID())}
	}
	id := "es_" + randomID(12)
	stream := &extensionStream{
		client:        client,
		out:           make(chan provider.Chunk, 64),
		done:          make(chan struct{}),
		abortDelivery: make(chan struct{}),
		deliveryWake:  make(chan struct{}, 1),
		nextSeq:       1,
		pending:       make(map[int64]provider.Chunk),
	}
	r.mu.Lock()
	r.streams[id] = stream
	r.mu.Unlock()
	go r.deliverStream(stream)

	gen := r.owner.Gate.Published()
	streamID := id
	streamRef := stream
	// Register before opening: a sidecar may emit stream/end or overflow while
	// ProviderStreamOpen is still returning, and those paths must unregister.
	unregisterDrainCancel := r.owner.Gate.RegisterDrainCancel(gen, func() {
		r.mu.Lock()
		if r.streams[streamID] == streamRef {
			r.abortDeliveryLocked(streamRef)
			r.finishLocked(streamID, streamRef, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{
				Err: fmt.Errorf("extension stream %s: generation %d drain timed out", streamID, gen),
			}})
		}
		r.mu.Unlock()
		go client.ProviderStreamCancel(streamID)
	})
	r.installDrainCancel(id, stream, unregisterDrainCancel)

	effort := ""
	if p.effort != nil {
		effort = *p.effort
	}
	opened, err := client.ProviderStreamOpen(ctx, protocol.StreamOpenParams{
		StreamID:    id,
		ProviderRef: p.ref,
		Model:       p.descriptor.Model,
		Effort:      effort,
		Request:     providerconv.RequestToProtocol(request),
		SeqBase:     1,
	})
	if err != nil {
		r.removeStream(id, stream)
		go client.ProviderStreamCancel(id)
		return nil, mapStreamOpenError(client, err)
	}
	if !opened.Accepted {
		r.removeStream(id, stream)
		go client.ProviderStreamCancel(id)
		return nil, fmt.Errorf("extension %s declined provider stream %q", client.PluginID(), p.ref)
	}
	// Provider request is already submitted to the sidecar — irreversible for
	// recovery (never claim rollback of an in-flight provider call).
	r.owner.RecordProviderSubmit(gen, id, client.PluginID())
	go r.watchStream(ctx, id, stream)
	return stream.out, nil
}

// installDrainCancel publishes the gate unregister callback under Resolver.mu.
// If expiration already finished the stream, unregister immediately instead of
// attaching cleanup state to a completed stream.
func (r *Resolver) installDrainCancel(id string, stream *extensionStream, unregister func()) {
	if unregister == nil {
		return
	}
	r.mu.Lock()
	if r.streams[id] == stream {
		stream.unregisterDrainCancel = unregister
		unregister = nil
	}
	r.mu.Unlock()
	if unregister != nil {
		unregister()
	}
}

// mapStreamOpenError lifts the sidecar's provider_interrupted family into
// StreamInterruptedError so the agent's interruption recovery applies; every
// other failure passes through with its frozen protocol reason intact.
func mapStreamOpenError(client ProviderClient, err error) error {
	var protocolErr *protocol.ProtocolError
	if errors.As(err, &protocolErr) && protocolErr.Reason == protocol.ErrProviderInterrupted {
		return &provider.StreamInterruptedError{Err: errors.New(protocolErr.Message)}
	}
	return fmt.Errorf("extension %s provider stream open: %w", client.PluginID(), err)
}

// watchStream finishes the stream on caller cancellation or sidecar loss. A
// cancel aborts delivery (the consumer is gone) and notifies the sidecar; a
// disconnect keeps draining buffered chunks before the terminal interruption,
// mirroring the broker's detach semantics.
func (r *Resolver) watchStream(ctx context.Context, id string, stream *extensionStream) {
	select {
	case <-stream.done:
		return
	case <-stream.client.Disconnected():
		r.mu.Lock()
		if r.streams[id] == stream {
			r.finishLocked(id, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{
				Err: fmt.Errorf("extension sidecar %s disconnected", stream.client.PluginID()),
			}})
		}
		r.mu.Unlock()
		return
	case <-ctx.Done():
	}
	r.mu.Lock()
	if r.streams[id] == stream {
		r.abortDeliveryLocked(stream)
		r.finishLocked(id, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: ctx.Err()}})
	}
	r.mu.Unlock()
	go stream.client.ProviderStreamCancel(id)
}
