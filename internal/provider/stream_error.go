package provider

import (
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
