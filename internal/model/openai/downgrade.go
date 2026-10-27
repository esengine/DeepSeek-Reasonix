package openai

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"

	"reasonix/internal/contract/provider"
)

// endpointFacts are what requests to this endpoint have established. authed is
// one credential's, so it stays with the client; whether the endpoint accepts
// stream_options is the endpoint's own, shared by every client of the same chat
// URL: an endpoint that rejects a body over include_usage is asked once whether
// it answers without it, and never asked again.
type endpointFacts struct {
	authed atomic.Bool // a request has succeeded; gates transient-401 retry
	stream *streamOptionFacts
	local  streamOptionFacts // used when the client was built without a shared record
}

// streamFacts is the record this client reads and writes.
func (f *endpointFacts) streamFacts() *streamOptionFacts {
	if f.stream != nil {
		return f.stream
	}
	return &f.local
}

type streamOptionFacts struct {
	omit  atomic.Bool // this endpoint refused the field and answered without it
	asked atomic.Bool // the question has been put once, however it answered
}

var streamFactsByURL sync.Map

// streamFactsFor is the shared record for one chat URL.
func streamFactsFor(chatURL string) *streamOptionFacts {
	v, _ := streamFactsByURL.LoadOrStore(chatURL, &streamOptionFacts{})
	return v.(*streamOptionFacts)
}

// streamOptions asks for the usage record unless this endpoint refused it.
func (c *client) streamOptions() *streamOptions {
	if c.learned.streamFacts().omit.Load() {
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
	source := req.Messages
	resolved, cached := c.resolveVideos(ctx, source)
	req.Messages = resolved
	stream, err := c.openResolvedChatStream(ctx, req)
	var api *provider.APIError
	if err != nil && len(cached) > 0 && errors.As(err, &api) && api.Status == http.StatusNotFound && ctx.Err() == nil {
		// A vendor that answers 404 to a request naming a file it was given earlier
		// no longer holds it: forget the reference and send the file again, once.
		c.video.forget(cached)
		req.Messages, _ = c.resolveVideos(ctx, source)
		return c.openResolvedChatStream(ctx, req)
	}
	return stream, err
}

func (c *client) openResolvedChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	wire := c.buildRequest(req)
	stream, err := c.openStream(ctx, c.chatURL, wire, req.Tools)
	if err == nil || wire.StreamOptions == nil || !bodyRejected(err) || ctx.Err() != nil ||
		!c.learned.streamFacts().asked.CompareAndSwap(false, true) {
		return stream, err
	}
	wire.StreamOptions = nil
	retried, retryErr := c.openStream(ctx, c.chatURL, wire, req.Tools)
	if retryErr != nil {
		return stream, err
	}
	c.learned.streamFacts().omit.Store(true)
	return retried, nil
}
