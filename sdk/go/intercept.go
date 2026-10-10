package extension

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (s *server) handleIntercept(ctx context.Context, raw json.RawMessage) (any, error) {
	var p InterceptParams
	if err := strictDecode(raw, &p); err != nil {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	if !validInterceptEvent(p.Event) || p.Seq < 1 || p.TimeoutMillis < 0 || !jsonKeyPresent(raw, "payload") {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	if p.TimeoutMillis > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(p.TimeoutMillis)*time.Millisecond)
		defer cancel()
	}
	payload, err := s.rehydrate(ctx, p.Payload, p.Externalized, "/payload")
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, MustProtocolError(ErrInterceptTimeout)
		}
		return nil, err
	}
	fn := s.opts.Interceptors[string(p.Event)]
	if fn == nil {
		fn = s.opts.Interceptors["*"]
	}
	if fn == nil {
		return Continue(), nil
	}
	result, err := fn(ctx, string(p.Event), payload)
	if err != nil {
		// The callback's advertised intercept budget expired. Return the
		// frozen timeout reason rather than racing the host's identical timer
		// with a generic internal error response.
		if errors.Is(err, context.DeadlineExceeded) && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, MustProtocolError(ErrInterceptTimeout)
		}
		return nil, err
	}
	if result == nil {
		return Continue(), nil
	}
	if !validInterceptDecision(result.Decision) {
		return nil, fmt.Errorf("extension: interceptor for %q returned invalid decision %q", p.Event, result.Decision)
	}
	return result, nil
}
