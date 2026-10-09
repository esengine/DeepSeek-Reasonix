package extension

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

func (s *server) handleStreamOpen(ctx context.Context, raw json.RawMessage) (result any, err error) {
	if s.opts.Provider == nil {
		return nil, MustProtocolError(ErrUnknownMethod)
	}
	var p StreamOpenParams
	if err := strictDecode(raw, &p); err != nil {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	if p.SeqBase < 0 {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	if err := p.Validate(); err != nil {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	handle := &streamHandle{cancel: cancel, done: make(chan struct{})}
	s.streamsMu.Lock()
	if _, exists := s.streams[p.StreamID]; exists {
		s.streamsMu.Unlock()
		cancel()
		return nil, &ProtocolError{Reason: ErrProtocolError, Message: "duplicate stream id " + p.StreamID}
	}
	s.streams[p.StreamID] = handle
	s.streamsMu.Unlock()
	defer func() {
		if result != nil {
			return
		}
		cancel()
		s.streamsMu.Lock()
		delete(s.streams, p.StreamID)
		s.streamsMu.Unlock()
		close(handle.done)
	}()
	chunks, err := s.opts.Provider.Stream(streamCtx, StreamRequest{
		StreamID:    p.StreamID,
		ProviderRef: p.ProviderRef,
		Model:       p.Model,
		Effort:      p.Effort,
		Request:     p.Request,
	})
	if err != nil {
		s.log.Printf("extension: provider stream %q failed to open: %v", p.StreamID, err)
		return nil, MustProtocolError(ErrProviderFailed)
	}
	if chunks == nil {
		return nil, errors.New("extension: provider returned a nil chunk channel")
	}
	return deferredResult{
		result: StreamOpenResult{Accepted: true},
		after:  func() { go s.pumpStream(streamCtx, p.StreamID, p.SeqBase, chunks, handle) },
	}, nil
}

func (s *server) handleStreamCancel(_ context.Context, raw json.RawMessage) (any, error) {
	var p StreamCancelParams
	if err := strictDecode(raw, &p); err != nil || strings.TrimSpace(p.StreamID) == "" {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	s.streamsMu.Lock()
	handle := s.streams[p.StreamID]
	s.streamsMu.Unlock()
	if handle == nil {
		return StreamCancelResult{Cancelled: false}, nil
	}
	handle.cancel()
	return StreamCancelResult{Cancelled: true}, nil
}
