package provider

import "errors"

// ErrInvalidFunctionCall identifies an unreadable call in a Responses stream.
var ErrInvalidFunctionCall = errors.New("responses: stream contains an invalid function call")

// ErrUnfinishedFunctionCall identifies a successful terminal with an open call.
var ErrUnfinishedFunctionCall = errors.New("responses: stream ended with an unfinished function call")

// StreamFailureCause is a bounded host attribution, safe to persist and project.
type StreamFailureCause string

const (
	StreamFailureInvalidFunctionCall    StreamFailureCause = "responses.invalid_function_call"
	StreamFailureUnfinishedFunctionCall StreamFailureCause = "responses.unfinished_function_call"
)

// StreamFailureCauseOf preserves producer identities through error wrapping.
func StreamFailureCauseOf(err error) StreamFailureCause {
	switch {
	case errors.Is(err, ErrInvalidFunctionCall):
		return StreamFailureInvalidFunctionCall
	case errors.Is(err, ErrUnfinishedFunctionCall):
		return StreamFailureUnfinishedFunctionCall
	default:
		return ""
	}
}

// Description never incorporates provider payloads, tool arguments or error prose.
func (c StreamFailureCause) Description() string {
	switch c {
	case StreamFailureInvalidFunctionCall:
		return "The Responses stream contained a function call with an invalid wire shape. The host refused the whole response; none of its tool calls ran or entered model history."
	case StreamFailureUnfinishedFunctionCall:
		return "The Responses stream ended successfully while a function call was still unclosed. The host refused the whole response; none of its tool calls ran or entered model history."
	default:
		return ""
	}
}
