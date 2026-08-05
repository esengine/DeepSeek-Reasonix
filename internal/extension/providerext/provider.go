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

// Stream opens one sidecar stream and returns its buffered chunk channel.
// The channel closes on a clean end; failures arrive as a terminal ChunkError
// (StreamInterruptedError for interruptions). Cancelling ctx aborts the
// stream through extension/provider/stream/cancel. A crashed sidecar fails
// fast — there is no fallback to another provider.
func (p *Provider) Stream(ctx context.Context, request provider.Request) (<-chan provider.Chunk, error) {
	if p == nil || p.resolver == nil || p.client == nil {
		return nil, fmt.Errorf("extension provider is unavailable")
	}
	return p.resolver.open(ctx, p, request)
}

// open registers the buffered stream, asks the sidecar to start it, and arms
// the cancellation/disconnect watcher. The seq buffering, delivery, and gap
// semantics mirror the broker's Host.open exactly.
func (r *Resolver) open(ctx context.Context, p *Provider, request provider.Request) (<-chan provider.Chunk, error) {
	client := p.client
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
	go r.watchStream(ctx, id, stream)
	return stream.out, nil
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
