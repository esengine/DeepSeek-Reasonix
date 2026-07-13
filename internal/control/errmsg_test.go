package control

import (
	"errors"
	"io"
	"strings"
	"testing"

	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

func TestExplainError(t *testing.T) {
	if explainError(nil) != nil {
		t.Error("nil should stay nil")
	}

	bal := explainError(&provider.APIError{Provider: "deepseek", Status: 402, Body: "Insufficient Balance"})
	if !strings.Contains(bal.Error(), i18n.M.ProviderErrInsufficientBalance) || !strings.Contains(bal.Error(), "Insufficient Balance") {
		t.Errorf("402 = %q, want the insufficient-balance message plus the provider body", bal.Error())
	}

	auth := explainError(&provider.AuthError{Provider: "deepseek", KeyEnv: "DEEPSEEK_API_KEY", Status: 401})
	if !strings.Contains(auth.Error(), "DEEPSEEK_API_KEY") {
		t.Errorf("401 should name the key env: %q", auth.Error())
	}
	if !strings.Contains(auth.Error(), i18n.M.ProviderErrAuth) {
		t.Errorf("401 without a key should use the missing-key message: %q", auth.Error())
	}

	rejected := explainError(&provider.AuthError{Provider: "mimo", KeyEnv: "MIMO_API_KEY", Status: 401, HasKey: true})
	if !strings.Contains(rejected.Error(), i18n.M.ProviderErrAuthRejected) {
		t.Errorf("401 with a key present should use the server-rejected message: %q", rejected.Error())
	}
	if !strings.Contains(rejected.Error(), "MIMO_API_KEY") {
		t.Errorf("401 should still name the key env: %q", rejected.Error())
	}

	sourced := explainError(&provider.AuthError{Provider: "deepseek", KeyEnv: "DEEPSEEK_API_KEY", KeySource: "project .env", Status: 401, HasKey: true})
	if !strings.Contains(sourced.Error(), "DEEPSEEK_API_KEY from project .env") {
		t.Errorf("401 should name the key source: %q", sourced.Error())
	}

	authBody := explainError(&provider.AuthError{Provider: "relay", KeyEnv: "RELAY_API_KEY", Status: 401, HasKey: true, Body: `{"error":{"message":"令牌已过期","type":"new_api_error"}}`})
	for _, want := range []string{i18n.M.ProviderErrAuthRejected, "RELAY_API_KEY", "令牌已过期"} {
		if !strings.Contains(authBody.Error(), want) {
			t.Errorf("401 with a body = %q, want it to contain %q", authBody.Error(), want)
		}
	}

	authEcho := explainError(&provider.AuthError{Provider: "deepseek", KeyEnv: "DEEPSEEK_API_KEY", Status: 401, HasKey: true, Body: `{"error":{"message":"Authentication Fails, Your api key: ****ae54 is invalid"}}`})
	if !strings.Contains(authEcho.Error(), "Authentication Fails") {
		t.Errorf("401 should keep the readable reason, got %q", authEcho.Error())
	}
	if strings.Contains(authEcho.Error(), "ae54") {
		t.Errorf("401 must not surface the masked key tail, got %q", authEcho.Error())
	}

	for _, status := range []int{400, 422, 429, 500, 503} {
		got := explainError(&provider.APIError{Provider: "p", Status: status})
		if got.Error() == "" || got.Error() == (&provider.APIError{Provider: "p", Status: status}).Error() {
			t.Errorf("status %d should map to a localized message, got %q", status, got.Error())
		}
	}

	jsonBody := explainError(&provider.APIError{Provider: "deepseek", Status: 400, Body: `{"error":{"message":"This model's maximum context length is 65536 tokens.","type":"invalid_request_error"}}`})
	if !strings.Contains(jsonBody.Error(), i18n.M.ProviderErrBadRequest) || !strings.Contains(jsonBody.Error(), "maximum context length") {
		t.Errorf("400 should append the provider reason from a JSON body, got %q", jsonBody.Error())
	}

	toolSchema := explainError(&provider.APIError{
		Provider:    "mimo",
		Status:      400,
		Body:        `{"error":{"message":"Tool 197 function has invalid 'parameters' schema"}}`,
		ToolContext: `Provider tool 197 maps to Reasonix tool "mcp__files__search" (MCP server "files", tool "search").`,
	})
	for _, want := range []string{"invalid 'parameters' schema", `MCP server "files"`} {
		if !strings.Contains(toolSchema.Error(), want) {
			t.Errorf("400 tool schema error = %q, want %q", toolSchema.Error(), want)
		}
	}

	rawBody := explainError(&provider.APIError{Provider: "deepseek", Status: 422, Body: "some unparseable detail"})
	if !strings.Contains(rawBody.Error(), "some unparseable detail") {
		t.Errorf("422 should fall back to the raw body, got %q", rawBody.Error())
	}

	rate := explainError(&provider.APIError{Provider: "deepseek", Status: 429, Body: `{"error":{"message":"slow down"}}`})
	if !strings.Contains(rate.Error(), i18n.M.ProviderErrRateLimited) || !strings.Contains(rate.Error(), "slow down") {
		t.Errorf("429 should append the provider reason, got %q", rate.Error())
	}

	// Relay gateways (one-api/new-api style) wrap the real failure — dead
	// upstream channel, unsupported tools, exhausted quota — in a 5xx JSON
	// body; the category line alone made those undiagnosable.
	relay := explainError(&provider.APIError{Provider: "relay", Status: 500, Body: `{"error":{"message":"no available channel for model claude-fable-5 in group default","type":"new_api_error"}}`})
	if !strings.Contains(relay.Error(), i18n.M.ProviderErrServer) || !strings.Contains(relay.Error(), "no available channel") {
		t.Errorf("500 should append the provider reason from a JSON body, got %q", relay.Error())
	}

	busy := explainError(&provider.APIError{Provider: "relay", Status: 503, Body: "upstream unavailable"})
	if !strings.Contains(busy.Error(), i18n.M.ProviderErrServerBusy) || !strings.Contains(busy.Error(), "upstream unavailable") {
		t.Errorf("503 should fall back to the raw body, got %q", busy.Error())
	}

	bare := explainError(&provider.APIError{Provider: "relay", Status: 500})
	if bare.Error() != i18n.M.ProviderErrServer {
		t.Errorf("500 without a body = %q, want exactly the localized message", bare.Error())
	}

	interrupted := explainError(&provider.StreamInterruptedError{Err: io.ErrUnexpectedEOF})
	if !strings.Contains(interrupted.Error(), "model stream interrupted") || !strings.Contains(interrupted.Error(), "continue") {
		t.Errorf("stream interruption should be actionable, got %q", interrupted.Error())
	}

	disconnected := explainError(io.ErrUnexpectedEOF)
	if !strings.Contains(disconnected.Error(), "model stream disconnected") || !strings.Contains(disconnected.Error(), "retry") {
		t.Errorf("connection reset should be actionable, got %q", disconnected.Error())
	}

	plain := errors.New("some other failure")
	if explainError(plain) != plain {
		t.Error("unknown errors should pass through unchanged")
	}
}

func TestRedactAuthReason(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"masked tail", "Your api key: ****ae54 is invalid", "Your api key: **** is invalid"},
		{"masked prefix form", "key sk-ab**** was rejected", "key **** was rejected"},
		{"full key echoed by a relay", "Invalid key sk-proj-abc123def456ghi789 provided", "Invalid key **** provided"},
		{"digit-free sk key via secrets.Redact", "api key: sk-proj-abcdefghijklmnop is invalid", "api key: **** is invalid"},
		{"digit-free value after credential word", "api key: relaykey_abcdefghijklmn rejected", "api key: **** rejected"},
		{"bearer value collapses fully", "Bearer abc.def-ghijklmnopqrs rejected", "Bearer **** rejected"},
		{"mixed-case token without context", "rejected AbCdEfGhIjKlMnOpQr", "rejected ****"},
		{"digit-free identifier survives", "code: invalid_authentication_token", "code: invalid_authentication_token"},
		{"all-caps code survives", "code INVALID_AUTHENTICATION_TOKEN", "code INVALID_AUTHENTICATION_TOKEN"},
		{"short tokens survive", "token expired at gateway", "token expired at gateway"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		if got := redactAuthReason(c.in); got != c.want {
			t.Errorf("%s: redactAuthReason(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
