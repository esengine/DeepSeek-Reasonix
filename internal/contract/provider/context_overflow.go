package provider

import (
	"encoding/json"
	"errors"
)

// contextOverflowCodes are the codes an error envelope sets when the request's
// input did not fit the model's context. The same rejection also arrives as
// prose from gateways that flatten upstream errors, and matching prose is how a
// classifier starts firing on messages that merely mention a limit — so a code
// enters this set only when a provider documents it.
var contextOverflowCodes = map[string]bool{
	"context_length_exceeded": true,
	"context_window_exceeded": true,
}

// IsContextOverflow reports whether err is a rejection for an input larger than
// the model's context, and therefore recoverable by sending less. It reads the
// envelope's code and nothing else, so an endpoint that reports no code — a
// Bedrock relay flattening everything to aws_invoke_error, say — is not
// classified, and the turn fails as it did before.
func IsContextOverflow(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	// A retryable status is a busy or broken server whatever code rides along;
	// the backoff ladder already owns those.
	if apiErr.Status < 400 || RetryableStatus(apiErr.Status) {
		return false
	}
	return contextOverflowCodes[apiErr.ErrorCode()]
}

// ErrorCode returns the machine-readable code from an OpenAI-shaped error
// envelope, nested under `error` or at the top level. A non-string code —
// gateways put the HTTP status there — is no code at all.
func (e *APIError) ErrorCode() string {
	if e == nil || e.Body == "" {
		return ""
	}
	var body struct {
		Error struct {
			Code json.RawMessage `json:"code"`
		} `json:"error"`
		Code json.RawMessage `json:"code"`
	}
	if json.Unmarshal([]byte(e.Body), &body) != nil {
		return ""
	}
	if code := decodeStringCode(body.Error.Code); code != "" {
		return code
	}
	return decodeStringCode(body.Code)
}

func decodeStringCode(raw json.RawMessage) string {
	var code string
	if len(raw) == 0 || json.Unmarshal(raw, &code) != nil {
		return ""
	}
	return code
}
