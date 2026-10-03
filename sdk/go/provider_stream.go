package extension

import (
	"context"
	"strings"
)

// pumpStream forwards one provider channel onto the wire: chunks become
// stream/chunk notifications with contiguous 1-based seqs (from SeqBase),
// and exactly one stream/end closes the stream — clean on channel close,
// with error on an error chunk, interrupted on cancel. A cancel processed by
// the SDK is never trailed by another chunk.
func (s *server) pumpStream(ctx context.Context, streamID string, seqBase int, chunks <-chan StreamChunk, handle *streamHandle) {
	defer close(handle.done)
	defer func() {
		handle.cancel()
		s.streamsMu.Lock()
		delete(s.streams, streamID)
		s.streamsMu.Unlock()
	}()
	seq := int64(seqBase)
	if seq < 1 {
		seq = 1
	}
	var lastSeq int64
	end := StreamEndParams{StreamID: streamID}
	for {
		// A cancel must never be trailed by one more chunk, so check before
		// every receive and again before every send.
		select {
		case <-ctx.Done():
			end.LastSeq, end.Interrupted = lastSeq, true
			s.sendStreamEnd(&end)
			return
		default:
		}
		select {
		case <-ctx.Done():
			end.LastSeq, end.Interrupted = lastSeq, true
			s.sendStreamEnd(&end)
			return
		case chunk, ok := <-chunks:
			if !ok {
				end.LastSeq = lastSeq
				s.sendStreamEnd(&end)
				return
			}
			if chunk.Type == ChunkError {
				end.LastSeq = lastSeq
				end.Error = frozenErrorSpecs[ErrProviderFailed].Message
				if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
					end.Error = chunk.Error.Message
				}
				s.sendStreamEnd(&end)
				return
			}
			if err := chunk.Validate(); err != nil {
				s.log.Printf("extension: provider stream %q produced an invalid chunk: %v", streamID, err)
				end.LastSeq = lastSeq
				end.Error = "the extension provider produced an invalid chunk"
				s.sendStreamEnd(&end)
				return
			}
			if err := s.conn.notify(MethodExtensionProviderStreamChunk, StreamChunkParams{
				StreamID: streamID, Seq: seq, Chunk: chunk,
			}); err != nil {
				s.log.Printf("extension: provider stream %q could not deliver chunk %d: %v", streamID, seq, err)
				return
			}
			lastSeq = seq
			seq++
		}
	}
}
