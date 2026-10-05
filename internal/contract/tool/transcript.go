package tool

import (
	"context"

	"reasonix/internal/contract/provider"
)

// TranscriptReader is the conversation a tool call belongs to, offered by the
// agent that owns it to a tool that reasons about the whole of it rather than
// one position. It returns a copy; the canonical transcript never leaves.
type TranscriptReader interface {
	Transcript() []provider.Message
}

type transcriptReaderKey struct{}

// WithTranscriptReader binds the active conversation to a tool call.
func WithTranscriptReader(ctx context.Context, r TranscriptReader) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, transcriptReaderKey{}, r)
}

// TranscriptReaderFrom returns the conversation bound to this tool call.
func TranscriptReaderFrom(ctx context.Context) (TranscriptReader, bool) {
	r, ok := ctx.Value(transcriptReaderKey{}).(TranscriptReader)
	return r, ok && r != nil
}
