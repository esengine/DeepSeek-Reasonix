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

func TestParseContextLimitErrorGLMUnnumbered1261(t *testing.T) {
	// Zhipu GLM reports a bare overflow with no token numbers. It must be
	// trusted as a context-limit error with an unknown window rather than
	// failing the same oversized request on every retry.
	body := `{"error":{"code":"1261","message":"Prompt exceeds max length"}}`
	got := ParseContextLimitError(&APIError{Status: 400, Body: body})
	if got == nil {
		t.Fatal("GLM 1261 must be trusted as a context-limit error")
	}
	if got.WindowTokens != 0 || got.RequestedTokens != 0 || got.PromptTokens != 0 || got.CompletionTokens != 0 {
		t.Fatalf("unnumbered overflow must carry zero token fields, got %+v", got)
	}
	if errors.Unwrap(got) == nil {
		t.Fatal("Unwrap must return the original APIError")
	}
	if ParseContextLimitError(&APIError{Status: 400, Body: `{"error":{"message":"prompt exceeds max length"}}`}) == nil {
		t.Fatal("case-insensitive message variant must be trusted")
	}
	if ParseContextLimitError(&APIError{Status: 401, Body: `{"error":{"code":"1261","message":"Prompt exceeds max length"}}`}) != nil {
		t.Fatal("401 must not be treated as a context limit")
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

func TestParseOutputLimitError(t *testing.T) {
	err := ParseOutputLimitError(&APIError{Status: 400, Body: `bad request: max_tokens is too large: 384000. This model supports at most 131072 completion tokens.`})
	if err == nil || err.RequestedTokens != 384000 || err.MaxOutputTokens != 131072 {
		t.Fatalf("ParseOutputLimitError = %+v, want requested 384000 max 131072", err)
	}
	if ParseOutputLimitError(&APIError{Status: 401, Body: `max_tokens is too large: 384000; supports at most 131072`}) != nil {
		t.Fatal("authorization errors must not be treated as output-limit errors")
	}
}
