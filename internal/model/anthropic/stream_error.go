package anthropic

import "reasonix/internal/contract/provider"

type streamWireError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// anthropicStreamError types an in-stream `error` event. Before any output the
// attempt can be made again, so it leaves as an upstream interruption; after
// output a replay would show it twice, so it stays a plain payload error.
func anthropicStreamError(name string, wire *streamWireError, emitted bool) error {
	payload := &provider.StreamPayloadError{Provider: name, Message: "stream error"}
	if wire != nil {
		payload.Message, payload.Type = wire.Message, wire.Type
	}
	if !emitted {
		return provider.StreamInterrupt(payload, provider.StreamInterruptUpstreamError)
	}
	return payload
}
