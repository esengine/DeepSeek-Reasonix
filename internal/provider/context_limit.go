package provider

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// ContextLimitError is a trusted shared-window overflow from a provider HTTP
// 400/413/422. Unwrap returns the original APIError so localization, trace IDs,
// and telemetry keep working. The body is never persisted or replayed.
type ContextLimitError struct {
	APIError         *APIError
	WindowTokens     int
	RequestedTokens  int
	PromptTokens     int
	CompletionTokens int
}

func (e *ContextLimitError) Error() string {
	if e == nil {
		return "context limit exceeded"
	}
	if e.APIError != nil {
		return e.APIError.Error()
	}
	return "context limit exceeded"
}

func (e *ContextLimitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.APIError
}

var (
	contextLimitEnglishRe = regexp.MustCompile(`(?i)maximum context length is (\d+) tokens?\.?\s*however,\s*you requested (\d+) tokens? \((\d+) in the (?:messages|prompt), (\d+) in the completion\)`)
	contextLimitPromptRe  = regexp.MustCompile(`(?i)prompt is too long:\s*(\d+) tokens? > (\d+) maximum`)
	contextLimitSumRe     = regexp.MustCompile("(?i)input length and [`']?max_tokens[`']? exceed context limit:\\s*(\\d+)\\s*\\+\\s*(\\d+)\\s*>\\s*(\\d+)")
)

func contextLimitStatusOK(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge || status == http.StatusUnprocessableEntity
}

func positiveToken(n int) bool { return n > 0 }

func contextLimitInvariant(window, requested, prompt, completion int) bool {
	if !positiveToken(window) {
		return false
	}
	if positiveToken(prompt) && positiveToken(completion) {
		if prompt+completion <= window {
			return false
		}
		if requested > 0 && requested != prompt+completion {
			return false
		}
		return true
	}
	if requested > window && (prompt > 0 || completion > 0 || requested > 0) {
		return requested > window
	}
	return false
}

func completeContextLimit(window, requested, prompt, completion int) (int, int, int, int, bool) {
	if window <= 0 {
		return 0, 0, 0, 0, false
	}
	if prompt > 0 && completion > 0 && requested <= 0 {
		requested = prompt + completion
	}
	if requested > 0 && prompt > 0 && completion <= 0 && requested > prompt {
		completion = requested - prompt
	}
	if requested > 0 && completion > 0 && prompt <= 0 && requested > completion {
		prompt = requested - completion
	}
	if !contextLimitInvariant(window, requested, prompt, completion) {
		return 0, 0, 0, 0, false
	}
	if requested <= 0 {
		requested = prompt + completion
	}
	return window, requested, prompt, completion, true
}

type contextLimitJSON struct {
	Error *struct {
		Message          string `json:"message"`
		ContextLength    int    `json:"context_length"`
		MaxContextLength int    `json:"max_context_length"`
		MaxTokens        int    `json:"max_tokens"`
		RequestedTokens  int    `json:"requested_tokens"`
		Requested        int    `json:"requested"`
		PromptTokens     int    `json:"prompt_tokens"`
		InputTokens      int    `json:"input_tokens"`
		CompletionTokens int    `json:"completion_tokens"`
		OutputTokens     int    `json:"output_tokens"`
	} `json:"error"`
	ContextLength    int `json:"context_length"`
	MaxContextLength int `json:"max_context_length"`
	RequestedTokens  int `json:"requested_tokens"`
	PromptTokens     int `json:"prompt_tokens"`
	InputTokens      int `json:"input_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	OutputTokens     int `json:"output_tokens"`
	Usage            *struct {
		InputTokens      int `json:"input_tokens"`
		OutputTokens     int `json:"output_tokens"`
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func firstPositive(values ...int) int {
	for _, n := range values {
		if n > 0 {
			return n
		}
	}
	return 0
}

func parseContextLimitJSON(body string) (window, requested, prompt, completion int, message string, ok bool) {
	var parsed contextLimitJSON
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return 0, 0, 0, 0, "", false
	}
	if parsed.Error != nil {
		message = parsed.Error.Message
		window = firstPositive(parsed.Error.ContextLength, parsed.Error.MaxContextLength)
		requested = firstPositive(parsed.Error.RequestedTokens, parsed.Error.Requested)
		prompt = firstPositive(parsed.Error.PromptTokens, parsed.Error.InputTokens)
		completion = firstPositive(parsed.Error.CompletionTokens, parsed.Error.OutputTokens)
	}
	window = firstPositive(window, parsed.ContextLength, parsed.MaxContextLength)
	requested = firstPositive(requested, parsed.RequestedTokens)
	prompt = firstPositive(prompt, parsed.PromptTokens, parsed.InputTokens)
	completion = firstPositive(completion, parsed.CompletionTokens, parsed.OutputTokens)
	if parsed.Usage != nil {
		prompt = firstPositive(prompt, parsed.Usage.PromptTokens, parsed.Usage.InputTokens)
		completion = firstPositive(completion, parsed.Usage.CompletionTokens, parsed.Usage.OutputTokens)
	}
	if window, requested, prompt, completion, ok = completeContextLimit(window, requested, prompt, completion); ok {
		return window, requested, prompt, completion, message, true
	}
	return 0, 0, 0, 0, message, false
}

func parseContextLimitText(text string) (window, requested, prompt, completion int, ok bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, 0, 0, 0, false
	}
	if m := contextLimitEnglishRe.FindStringSubmatch(text); len(m) == 5 {
		return completeContextLimit(atoiStrict(m[1]), atoiStrict(m[2]), atoiStrict(m[3]), atoiStrict(m[4]))
	}
	if m := contextLimitSumRe.FindStringSubmatch(text); len(m) == 4 {
		return completeContextLimit(atoiStrict(m[3]), 0, atoiStrict(m[1]), atoiStrict(m[2]))
	}
	if m := contextLimitPromptRe.FindStringSubmatch(text); len(m) == 3 {
		prompt = atoiStrict(m[1])
		window = atoiStrict(m[2])
		if prompt > 0 && window > 0 && prompt > window {
			return window, prompt, prompt, 0, true
		}
	}
	return 0, 0, 0, 0, false
}

func atoiStrict(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// ParseContextLimitError extracts a trusted overflow from an APIError.
// Unparseable, non-context, or invariant-breaking bodies return nil.
func ParseContextLimitError(apiErr *APIError) *ContextLimitError {
	if apiErr == nil || !contextLimitStatusOK(apiErr.Status) {
		return nil
	}
	window, requested, prompt, completion, message, jsonOK := parseContextLimitJSON(apiErr.Body)
	if !jsonOK {
		if w, r, p, c, ok := parseContextLimitText(apiErr.Body); ok {
			window, requested, prompt, completion = w, r, p, c
		} else if w, r, p, c, ok := parseContextLimitText(message); ok {
			window, requested, prompt, completion = w, r, p, c
		} else {
			return nil
		}
	}
	if !contextLimitInvariant(window, requested, prompt, completion) &&
		!(window > 0 && requested > window && prompt > 0) {
		return nil
	}
	if requested <= 0 {
		requested = prompt + completion
	}
	return &ContextLimitError{
		APIError:         apiErr,
		WindowTokens:     window,
		RequestedTokens:  requested,
		PromptTokens:     prompt,
		CompletionTokens: completion,
	}
}

// AsContextLimitError unwraps err to a trusted overflow, if any.
func AsContextLimitError(err error) *ContextLimitError {
	var limit *ContextLimitError
	if err != nil && errors.As(err, &limit) {
		return limit
	}
	return nil
}
