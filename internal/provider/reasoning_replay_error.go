package provider

import (
	"errors"
	"net/http"
	"strings"
)

// ReasoningReplayError is a trusted provider rejection of replayed
// thinking/reasoning history: HTTP 400 whose body says the request's
// thinking/reasoning blocks must be passed back. Unwrap returns the original
// APIError so localization, trace IDs, and telemetry keep working. The body is
// never persisted or replayed.
type ReasoningReplayError struct {
	APIError *APIError
}

func (e *ReasoningReplayError) Error() string {
	if e == nil || e.APIError == nil {
		return "reasoning replay rejected"
	}
	return e.APIError.Error()
}

func (e *ReasoningReplayError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.APIError
}

// ParseReasoningReplayError extracts a trusted thinking-replay rejection from
// an APIError. The match is deliberately exact — DeepSeek's documented shape is
// "The `content[].thinking` in the thinking mode must be passed back to the
// API" — so only a 400 naming thinking/reasoning content AND the pass-back
// obligation qualifies. Any other 400 (and every other status) returns nil so
// the retry budget can never swallow an unrelated client error.
func ParseReasoningReplayError(apiErr *APIError) *ReasoningReplayError {
	if apiErr == nil || apiErr.Status != http.StatusBadRequest {
		return nil
	}
	body := strings.ToLower(apiErr.Body)
	namesReasoning := strings.Contains(body, "content[].thinking") || strings.Contains(body, "reasoning_content") || strings.Contains(body, "reasoning_text")
	if !namesReasoning || !strings.Contains(body, "must be passed back") {
		return nil
	}
	return &ReasoningReplayError{APIError: apiErr}
}

// AsReasoningReplayError unwraps err to a trusted thinking-replay rejection,
// if any.
func AsReasoningReplayError(err error) *ReasoningReplayError {
	var replay *ReasoningReplayError
	if err != nil && errors.As(err, &replay) {
		return replay
	}
	return nil
}
