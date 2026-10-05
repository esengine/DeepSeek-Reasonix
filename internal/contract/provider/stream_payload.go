// A gateway reporting its own failure inside a stream it already answered 200
// to. The status ladder never sees it — the headers succeeded — so it travels
// as a type rather than being recognised by its wording.
package provider

// StreamInterruptUpstreamError is the reason for one of these: the attempt
// failed on the far side, not on the wire between here and the gateway.
const StreamInterruptUpstreamError = "upstream_error"

type StreamPayloadError struct {
	Provider string
	Message  string
	Code     string
	Type     string
	Param    string
}

func (e *StreamPayloadError) Error() string {
	if e == nil {
		return "stream payload error"
	}
	return e.Provider + ": " + e.Message
}
