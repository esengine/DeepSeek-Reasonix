package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestParseReasoningReplayError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{
			name:   "deepseek anthropic thinking 400",
			status: 400,
			body:   `{"error":{"message":"The ` + "`content[].thinking`" + ` in the thinking mode must be passed back to the API","type":"invalid_request_error"}}`,
			want:   true,
		},
		{
			name:   "openai-style reasoning_content 400",
			status: 400,
			body:   `{"error":{"message":"reasoning_content must be passed back to the API in thinking mode"}}`,
			want:   true,
		},
		{
			name:   "official responses reasoning_text 400",
			status: 400,
			body:   "The `reasoning_text` in the thinking mode must be passed back to the API.",
			want:   true,
		},
		{
			name:   "responses unrelated reasoning error",
			status: 400,
			body:   "reasoning_text is not supported",
			want:   false,
		},
		{
			name:   "case-insensitive",
			status: 400,
			body:   `The CONTENT[].THINKING in the thinking mode MUST BE PASSED BACK to the API`,
			want:   true,
		},
		{
			name:   "400 without pass-back obligation",
			status: 400,
			body:   `{"error":{"message":"content[].thinking is not supported by this model"}}`,
			want:   false,
		},
		{
			name:   "400 pass-back without reasoning reference",
			status: 400,
			body:   `{"error":{"message":"the signed block must be passed back unchanged"}}`,
			want:   false,
		},
		{
			name:   "401 with the same body",
			status: 401,
			body:   `{"error":{"message":"The content[].thinking in the thinking mode must be passed back to the API"}}`,
			want:   false,
		},
		{
			name:   "422 with the same body",
			status: 422,
			body:   `{"error":{"message":"The content[].thinking in the thinking mode must be passed back to the API"}}`,
			want:   false,
		},
		{
			name:   "unrelated 400",
			status: 400,
			body:   `{"error":{"message":"invalid tool schema at index 2"}}`,
			want:   false,
		},
		{
			name:   "empty body",
			status: 400,
			body:   "",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseReasoningReplayError(&APIError{Provider: "p", Status: tc.status, Body: tc.body})
			if (got != nil) != tc.want {
				t.Fatalf("ParseReasoningReplayError = %v, want match=%v", got, tc.want)
			}
			if got != nil && got.APIError == nil {
				t.Fatal("matched error lost the underlying APIError")
			}
		})
	}
	if ParseReasoningReplayError(nil) != nil {
		t.Fatal("nil APIError must not match")
	}
}

func TestAsReasoningReplayError(t *testing.T) {
	apiErr := &APIError{Provider: "p", Status: 400, Body: "The `content[].thinking` in the thinking mode must be passed back to the API"}
	replay := ParseReasoningReplayError(apiErr)
	if replay == nil {
		t.Fatal("fixture did not match")
	}
	if got := AsReasoningReplayError(fmt.Errorf("stream: %w", replay)); got != replay {
		t.Fatalf("AsReasoningReplayError = %v, want the wrapped error", got)
	}
	if got := AsReasoningReplayError(apiErr); got != nil {
		t.Fatalf("bare APIError must not unwrap as ReasoningReplayError: %v", got)
	}
	if got := AsReasoningReplayError(errors.New("other")); got != nil {
		t.Fatalf("unrelated error = %v, want nil", got)
	}
	if got := AsReasoningReplayError(nil); got != nil {
		t.Fatal("nil error must not match")
	}
	if !errors.Is(replay, apiErr) {
		t.Fatal("ReasoningReplayError must unwrap to the APIError")
	}
}
