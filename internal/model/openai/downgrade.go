package openai

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"

	"reasonix/internal/contract/provider"
)

// endpointFacts are what requests to this endpoint have established, each
// written once and read by every later request. stream_options is the field a
// gateway most often does not know: include_usage buys a token count and
// nothing a turn needs, so an endpoint that rejects a body over it is asked
// once whether it answers without it, and never asked again.
type endpointFacts struct {
	authed             atomic.Bool // a request has succeeded; gates transient-401 retry
	omitStreamOptions  atomic.Bool // this endpoint refused the field and answered without it
	streamOptionsAsked atomic.Bool // the question has been put once, however it answered
}

// streamOptions asks for the usage record unless this endpoint refused it.
func (c *client) streamOptions() *streamOptions {
	if c.learned.omitStreamOptions.Load() {
		return nil
	}
	return &streamOptions{IncludeUsage: true}
}

// bodyRejected reports a refusal of what the request said rather than of who
// asked, how often, or whether the account can pay — only that kind can be
// answered by sending less. The statuses are named rather than taken from the
// 4xx range, so one this list has not considered spends no second request.
func bodyRejected(err error) bool {
	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// openChatStream opens the turn's stream, and when a gateway rejects the body
// over stream_options asks once without it. A gateway that does not know the
// field would otherwise fail every turn over a token count; the retry is the
// evidence, and CompareAndSwap makes it a question asked once per endpoint
// rather than on every refusal.
func (c *client) openChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	wire := c.buildRequest(req)
	stream, err := c.openStream(ctx, c.chatURL, wire, req.Tools)
	if err == nil || wire.StreamOptions == nil || !bodyRejected(err) || ctx.Err() != nil ||
		!c.learned.streamOptionsAsked.CompareAndSwap(false, true) {
		return stream, err
	}
	wire.StreamOptions = nil
	retried, retryErr := c.openStream(ctx, c.chatURL, wire, req.Tools)
	if retryErr != nil {
		return stream, err
	}
	c.learned.omitStreamOptions.Store(true)
	return retried, nil
}
