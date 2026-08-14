package provider

import "context"

// StreamFailure returns a channel carrying a transport failure that happened
// after the request may have reached the provider. The Agent owns replay of
// this uncommitted sampling attempt; provider retry loops must not send the
// same POST again.
func StreamFailure(ctx context.Context, err error) <-chan Chunk {
	out := make(chan Chunk, 1)
	go func() {
		defer close(out)
		chunk := Chunk{
			Type: ChunkError,
			Err:  StreamInterrupt(err, ClassifyStreamInterrupt(err)),
		}
		if ctx == nil {
			out <- chunk
			return
		}
		select {
		case out <- chunk:
		case <-ctx.Done():
		}
	}()
	return out
}
