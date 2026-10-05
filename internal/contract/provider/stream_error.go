package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const streamPayloadExcerpt = 120

// StreamDecodeError reports an SSE data payload that failed to parse, quoting a
// bounded excerpt of it. Gateways put non-JSON on `data:` lines — HTML error
// pages, bare sentinels, proxy notices — and the decoder error alone names only
// the first offending byte, which is not enough to identify the sender.
func StreamDecodeError(provider string, payload string, err error) error {
	return fmt.Errorf("%s: decode stream: %w (payload %s)", provider, err, streamPayloadSnippet(payload))
}

func streamPayloadSnippet(payload string) string {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "(empty)"
	}
	if len(payload) > streamPayloadExcerpt {
		return strconv.Quote(payload[:streamPayloadExcerpt]) + "…"
	}
	return strconv.Quote(payload)
}

// ReadCut reports a read that failed after the response headers succeeded.
// Nothing but transport can fail there — auth, 4xx, and schema rejections are
// all decided before the body opens — so the round never reached a terminal and
// the Agent may replay it. Which transport error it was is deliberately not
// asked; cancellation is, because only it means the caller wanted this stopped.
func ReadCut(provider string, err error) error {
	wrapped := fmt.Errorf("%s: read stream: %w", provider, err)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return wrapped
	}
	return StreamInterrupt(wrapped, ClassifyStreamInterrupt(err))
}
