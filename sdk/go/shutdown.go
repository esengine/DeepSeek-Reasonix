package extension

import (
	"context"
	"encoding/json"
	"io"
	"time"
)

func (s *server) handleShutdown(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ShutdownParams
	if err := strictDecode(raw, &p); err != nil || p.TimeoutMillis < 0 {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	s.shutdownOnce.Do(func() {
		s.mu.Lock()
		s.state = stateShutdown
		s.mu.Unlock()
		if s.opts.Shutdown != nil {
			fnCtx := ctx
			cancel := func() {}
			if p.TimeoutMillis > 0 {
				fnCtx, cancel = context.WithTimeout(ctx, time.Duration(p.TimeoutMillis)*time.Millisecond)
			}
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() {
					if recovered := recover(); recovered != nil {
						s.log.Printf("extension: shutdown handler panic: %v", recovered)
					}
				}()
				s.opts.Shutdown(fnCtx)
			}()
			select {
			case <-done:
			case <-fnCtx.Done():
				s.log.Printf("extension: shutdown function did not return within %dms", p.TimeoutMillis)
			}
		}
	})
	return deferredResult{
		result: ShutdownResult{Accepted: true},
		after: func() {
			// Orderly close: end in-flight calls, then close the read side so
			// the read loop exits and the host sees EOF when the process
			// exits. Serve returns nil.
			s.conn.shutdown(nil)
			if closer, ok := s.conn.r.(io.Closer); ok {
				_ = closer.Close()
			}
		},
	}, nil
}
