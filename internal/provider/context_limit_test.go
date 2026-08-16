package provider

import (
	"errors"
	"net/http"
	"testing"
)

func TestParseContextLimitErrorJSONAndEnglish(t *testing.T) {
	body := `{"error":{"message":"This model's maximum context length is 1048576 tokens. However, you requested 1165351 tokens (810882 in the messages, 354469 in the completion)."}}`
	got := ParseContextLimitError(&APIError{Status: 400, Body: body})
	if got == nil || got.WindowTokens != 1_048_576 || got.PromptTokens != 810_882 || got.CompletionTokens != 354_469 || got.RequestedTokens != 1_165_351 {
		t.Fatalf("english/json parse = %+v", got)
	}
	if errors.Unwrap(got) == nil {
		t.Fatal("Unwrap must return the original APIError")
	}
}

func TestParseContextLimitErrorNumericJSON(t *testing.T) {
	body := `{"error":{"context_length":1048576,"requested_tokens":1165351,"prompt_tokens":810882,"completion_tokens":354469}}`
	got := ParseContextLimitError(&APIError{Status: 400, Body: body})
	if got == nil || got.WindowTokens != 1_048_576 || got.PromptTokens != 810_882 || got.CompletionTokens != 354_469 {
		t.Fatalf("numeric json = %+#v", got)
	}
}

func TestParseContextLimitErrorRejectsMalformedAndNonContext(t *testing.T) {
	if ParseContextLimitError(&APIError{Status: 400, Body: `{"error":{"message":"unpaired tool_calls"}}`}) != nil {
		t.Fatal("non-context 400 must stay unparsed")
	}
	if ParseContextLimitError(&APIError{Status: 400, Body: `{"error":{"context_length":-1,"prompt_tokens":10,"completion_tokens":10}}`}) != nil {
		t.Fatal("negative numbers must be rejected")
	}
	if ParseContextLimitError(&APIError{Status: 400, Body: `{"error":{"context_length":100,"prompt_tokens":10,"completion_tokens":10}}`}) != nil {
		t.Fatal("prompt+completion <= window must be rejected")
	}
	if ParseContextLimitError(&APIError{Status: 401, Body: `This model's maximum context length is 1048576 tokens. However, you requested 1165351 tokens (810882 in the messages, 354469 in the completion).`}) != nil {
		t.Fatal("401 must not be treated as a context limit")
	}
}

func TestParseContextLimitErrorAccepts413And422(t *testing.T) {
	text := "This model's maximum context length is 1048576 tokens. However, you requested 1165351 tokens (810882 in the messages, 354469 in the completion)."
	if ParseContextLimitError(&APIError{Status: http.StatusRequestEntityTooLarge, Body: text}) == nil {
		t.Fatal("413 should parse")
	}
	if ParseContextLimitError(&APIError{Status: http.StatusUnprocessableEntity, Body: text}) == nil {
		t.Fatal("422 should parse")
	}
}
