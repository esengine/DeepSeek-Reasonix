package providerext

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/providerconv"
	"reasonix/internal/provider"
	"reasonix/internal/secrets"
)

// extensionStream is one in-flight sidecar provider stream. Its fields are
// guarded by Resolver.mu and mirror the broker's hostStream: chunks arrive
// with 1-based seqs, buffer out of order, and flush contiguously; stream/end
// freezes the terminal boundary (LastSeq) and a gap timer converts a missing
// tail chunk into an interruption instead of a hang.
type extensionStream struct {
	client                ProviderClient
	out                   chan provider.Chunk
	done                  chan struct{}
	abortDelivery         chan struct{}
	nextSeq               int64
	pending               map[int64]provider.Chunk
	ended                 bool
	endSeq                int64
	endError              string
	interrupted           bool
	gapTimer              bool
	closeOnce             sync.Once
	delivery              []provider.Chunk
	deliveryWake          chan struct{}
	deliveryFinal         bool
	activity              chan struct{}
	unregisterDrainCancel func()
}

// deliveryQueueLimit bounds the per-stream delivery queue without applying
// backpressure while Resolver.mu is held. One slot is reserved for the
// terminal chunk; mirroring the broker's hostDeliveryQueueLimit.
const deliveryQueueLimit = 256

// pendingWindowLimit bounds out-of-order buffering: chunks with a sequence at
// or beyond nextSeq+pendingWindowLimit never enter pending. A sidecar with a
// sequencing bug (emitting ever-higher seqs without the missing one or an
// end) must not grow host memory without limit — the stream is failed
// interrupted instead.
const pendingWindowLimit = 256

// RouteStreamChunk implements sidecar.StreamRouter. Unknown stream IDs are
// dropped with a debug log — a sidecar can legitimately race a late chunk
// against the host's cancel or its own crash teardown. Stale-generation
// chunks (after publish of a newer runtime) are also dropped.
func (r *Resolver) RouteStreamChunk(p protocol.StreamChunkParams) {
	gen := p.Generation
	if gen == 0 {
		gen = p.Chunk.Generation
	}
	if r.owner.Gate.DropStale(gen, "provider_chunk") {
		slog.Debug("providerext: dropping stale-generation chunk", "stream", p.StreamID, "seq", p.Seq, "generation", gen)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	stream := r.streams[p.StreamID]
	if stream == nil {
		slog.Debug("providerext: dropping chunk for unknown stream", "stream", p.StreamID, "seq", p.Seq)
		return
	}
	signalStreamActivity(stream)
	if p.Seq < stream.nextSeq {
		return // duplicate or already delivered
	}
	if stream.ended && p.Seq > stream.endSeq {
		r.finishLocked(p.StreamID, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{
			Err: fmt.Errorf("extension stream %s: chunk seq %d exceeds frozen LastSeq %d", p.StreamID, p.Seq, stream.endSeq),
		}})
		return
	}
	if p.Seq >= stream.nextSeq+pendingWindowLimit {
		r.finishLocked(p.StreamID, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{
			Err: fmt.Errorf("extension stream %s: chunk seq %d exceeds the pending window of stream seq %d", p.StreamID, p.Seq, stream.nextSeq),
		}})
		return
	}
	stream.pending[p.Seq] = providerconv.ChunkFromProtocol(p.Chunk)
	r.flushLocked(p.StreamID, stream)
}

// RouteStreamEnd implements sidecar.StreamRouter. LastSeq freezes the
// terminal boundary: the stream completes only after chunks 1..LastSeq have
// been delivered, and a missing chunk trips the gap timer.
func (r *Resolver) RouteStreamEnd(p protocol.StreamEndParams) {
	r.mu.Lock()
	stream := r.streams[p.StreamID]
	if stream == nil {
		r.mu.Unlock()
		slog.Debug("providerext: dropping stream end for unknown stream", "stream", p.StreamID)
		return
	}
	signalStreamActivity(stream)
	if stream.ended {
		if stream.endSeq != p.LastSeq || stream.endError != p.Error || stream.interrupted != p.Interrupted {
			r.finishLocked(p.StreamID, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{
				Err: fmt.Errorf("extension stream %s: conflicting duplicate end changed frozen LastSeq or terminal state", p.StreamID),
			}})
			r.mu.Unlock()
			return
		}
		r.flushLocked(p.StreamID, stream)
		r.mu.Unlock()
		return
	}
	stream.ended = true
	stream.endSeq = p.LastSeq
	stream.endError = p.Error
	stream.interrupted = p.Interrupted
	for seq := range stream.pending {
		if seq > stream.endSeq {
			r.finishLocked(p.StreamID, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{
				Err: fmt.Errorf("extension stream %s: buffered chunk seq %d exceeds frozen LastSeq %d", p.StreamID, seq, stream.endSeq),
			}})
			r.mu.Unlock()
			return
		}
	}
	r.flushLocked(p.StreamID, stream)
	if r.streams[p.StreamID] == stream && stream.nextSeq <= stream.endSeq && !stream.gapTimer {
		stream.gapTimer = true
		go r.expireGap(p.StreamID, stream)
	}
	r.mu.Unlock()
}

func signalStreamActivity(stream *extensionStream) {
	if stream == nil || stream.activity == nil {
		return
	}
	select {
	case stream.activity <- struct{}{}:
	default:
	}
}

// flushLocked delivers every contiguous pending chunk, then completes the
// stream once the end boundary is fully delivered. The terminal chunk mirrors
// the broker: a reported error is defensively credential-redacted even though
// the protocol also requires producer-side redaction, an interruption becomes
// StreamInterruptedError, and a clean end closes the channel.
func (r *Resolver) flushLocked(id string, stream *extensionStream) {
	for !stream.ended || stream.nextSeq <= stream.endSeq {
		chunk, ok := stream.pending[stream.nextSeq]
		if !ok {
			break
		}
		delete(stream.pending, stream.nextSeq)
		stream.nextSeq++
		if !r.enqueueDeliveryLocked(stream, chunk) {
			r.finishLocked(id, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{
				Err: errors.New("extension provider stream output overflow"),
			}})
			return
		}
	}
	if !stream.ended || stream.nextSeq <= stream.endSeq {
		return
	}
	if stream.endError != "" || stream.interrupted {
		redactedEndError := secrets.RedactCredentials(stream.endError)
		err := errors.New(redactedEndError)
		if stream.endError == "" {
			err = errors.New("extension provider stream failed")
		}
		if stream.interrupted {
			message := redactedEndError
			if message == "" {
				message = "extension provider stream was interrupted"
			}
			err = &provider.StreamInterruptedError{Err: errors.New(message)}
		}
		r.finishLocked(id, stream, provider.Chunk{Type: provider.ChunkError, Err: err})
		return
	}
	r.finishLocked(id, stream, provider.Chunk{})
}

// finishLocked appends the terminal chunk (when non-zero) and marks delivery
// final. The first finish wins; later finishes for a replaced or completed
// stream are no-ops.
func (r *Resolver) finishLocked(id string, stream *extensionStream, terminal provider.Chunk) {
	if r.streams[id] != stream {
		return
	}
	delete(r.streams, id)
	stream.closeOnce.Do(func() {
		if stream.unregisterDrainCancel != nil {
			stream.unregisterDrainCancel()
			stream.unregisterDrainCancel = nil
		}
		if terminal.Err != nil || terminal.Type != 0 {
			stream.delivery = append(stream.delivery, terminal)
		}
		stream.deliveryFinal = true
		close(stream.done)
		r.signalDeliveryLocked(stream)
	})
}

// removeStream tears down a stream whose open never completed, aborting the
// delivery loop without a terminal chunk.
func (r *Resolver) removeStream(id string, stream *extensionStream) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.streams[id] == stream {
		r.abortDeliveryLocked(stream)
		r.finishLocked(id, stream, provider.Chunk{})
	}
}

func (r *Resolver) abortDeliveryLocked(stream *extensionStream) {
	if stream.abortDelivery == nil {
		return
	}
	select {
	case <-stream.abortDelivery:
	default:
		close(stream.abortDelivery)
	}
}

func (r *Resolver) enqueueDeliveryLocked(stream *extensionStream, chunk provider.Chunk) bool {
	if stream.deliveryFinal || len(stream.delivery) >= deliveryQueueLimit-1 {
		return false
	}
	stream.delivery = append(stream.delivery, chunk)
	r.signalDeliveryLocked(stream)
	return true
}

func (r *Resolver) signalDeliveryLocked(stream *extensionStream) {
	select {
	case stream.deliveryWake <- struct{}{}:
	default:
	}
}

// deliverStream is the sole sender and closer of stream.out. It may wait for
// a slow consumer, but never while holding Resolver.mu, so routing,
// cancellation, disconnects, gap expiry, and unrelated streams keep moving.
func (r *Resolver) deliverStream(stream *extensionStream) {
	defer close(stream.out)
	for {
		r.mu.Lock()
		if len(stream.delivery) > 0 {
			chunk := stream.delivery[0]
			stream.delivery[0] = provider.Chunk{}
			stream.delivery = stream.delivery[1:]
			r.mu.Unlock()
			select {
			case stream.out <- chunk:
			case <-stream.abortDelivery:
				return
			}
			continue
		}
		if stream.deliveryFinal {
			r.mu.Unlock()
			return
		}
		wake := stream.deliveryWake
		r.mu.Unlock()
		select {
		case <-wake:
		case <-stream.abortDelivery:
			return
		}
	}
}

// expireGap converts a missing tail chunk into an interruption one second
// after stream/end: the LastSeq boundary froze, so the absent seq will never
// legitimately arrive.
func (r *Resolver) expireGap(id string, stream *extensionStream) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-stream.done:
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.streams[id] != stream || !stream.ended || stream.nextSeq > stream.endSeq {
		return
	}
	r.finishLocked(id, stream, provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{
		Err: fmt.Errorf("extension provider stream missing chunk %d of %d", stream.nextSeq, stream.endSeq),
	}})
}

func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
