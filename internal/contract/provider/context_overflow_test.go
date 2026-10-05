package provider

import (
	"fmt"
	"net/http"
	"testing"
)

func TestIsContextOverflow(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "openai nested code",
			err:  &APIError{Status: http.StatusBadRequest, Body: `{"error":{"message":"This model's maximum context length is 128000 tokens","type":"invalid_request_error","param":"messages","code":"context_length_exceeded"}}`},
			want: true,
		},
		{
			name: "top-level code",
			err:  &APIError{Status: http.StatusBadRequest, Body: `{"code":"context_window_exceeded","message":"too long"}`},
			want: true,
		},
		{
			name: "wrapped error",
			err:  fmt.Errorf("stream: %w", &APIError{Status: http.StatusRequestEntityTooLarge, Body: `{"error":{"code":"context_length_exceeded"}}`}),
			want: true,
		},
		{
			name: "relay flattens the cause",
			err:  &APIError{Status: http.StatusBadRequest, Body: `{"error":{"message":"ValidationException: input is too long for requested model","type":"invalid_request_error","code":"aws_invoke_error"}}`},
			want: false,
		},
		{
			name: "prose only",
			err:  &APIError{Status: http.StatusBadRequest, Body: `{"error":{"message":"maximum context length exceeded","type":"invalid_request_error","code":null}}`},
			want: false,
		},
		{
			name: "numeric code",
			err:  &APIError{Status: http.StatusBadRequest, Body: `{"object":"error","message":"context length exceeded","code":400}`},
			want: false,
		},
		{
			name: "retryable status",
			err:  &APIError{Status: http.StatusTooManyRequests, Body: `{"error":{"code":"context_length_exceeded"}}`},
			want: false,
		},
		{
			name: "server error",
			err:  &APIError{Status: http.StatusBadGateway, Body: `{"error":{"code":"context_length_exceeded"}}`},
			want: false,
		},
		{
			name: "not json",
			err:  &APIError{Status: http.StatusBadRequest, Body: `<html>413 Request Entity Too Large</html>`},
			want: false,
		},
		{
			name: "auth error",
			err:  &AuthError{Status: http.StatusUnauthorized},
			want: false,
		},
		{name: "nil", err: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsContextOverflow(tc.err); got != tc.want {
				t.Fatalf("IsContextOverflow = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAPIErrorCode(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "nested", body: `{"error":{"code":"invalid_api_key"}}`, want: "invalid_api_key"},
		{name: "top level", body: `{"code":"rate_limit_exceeded"}`, want: "rate_limit_exceeded"},
		{name: "nested wins", body: `{"error":{"code":"context_length_exceeded"},"code":"400"}`, want: "context_length_exceeded"},
		{name: "empty body", body: "", want: ""},
		{name: "no code", body: `{"error":{"message":"nope"}}`, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := &APIError{Status: http.StatusBadRequest, Body: tc.body}
			if got := err.ErrorCode(); got != tc.want {
				t.Fatalf("ErrorCode = %q, want %q", got, tc.want)
			}
		})
	}
	var nilErr *APIError
	if got := nilErr.ErrorCode(); got != "" {
		t.Fatalf("nil ErrorCode = %q, want empty", got)
	}
}
