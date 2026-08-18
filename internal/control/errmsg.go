package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"reasonix/internal/i18n"
	"reasonix/internal/provider"
	"reasonix/internal/secrets"
)

// explainError maps a provider HTTP failure to an actionable, localized message
// so the turn-done error the UI shows is never a bare status code or silent
// failure. Unknown errors (and nil) pass through unchanged.
func explainError(err error) error {
	if err == nil {
		return nil
	}
	if provider.IsStreamInterrupted(err) {
		return fmt.Errorf("model stream interrupted after recovery attempts: %s. The partial response was kept; retry or ask Reasonix to continue", err.Error())
	}
	if provider.IsConnReset(err) {
		return fmt.Errorf("model stream disconnected before completion after retry attempts: %s. Check the provider/proxy connection, then retry or ask Reasonix to continue", err.Error())
	}
	if limit := provider.AsContextLimitError(err); limit != nil {
		msg := fmt.Sprintf(i18n.M.ProviderErrContextOverflowFmt, limit.PromptTokens, limit.CompletionTokens, limit.RequestedTokens, limit.WindowTokens)
		if reason := apiErrorReason(limit.APIError); reason != "" {
			return fmt.Errorf("%s\n%s", msg, reason)
		}
		return errors.New(msg)
	}
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		if msg := providerContentSafetyMessage(apiErr); msg != "" {
			if reason := apiErrorReason(apiErr); reason != "" {
				return fmt.Errorf("%s\n%s", msg, reason)
			}
			return errors.New(msg)
		}
		msg := i18n.M.ProviderStatusMessage(apiErr.Status)
		if msg == "" {
			return err
		}
		if reason := apiErrorReason(apiErr); reason != "" {
			return fmt.Errorf("%s\n%s", msg, reason)
		}
		return errors.New(msg)
	}
	var authErr *provider.AuthError
	if errors.As(err, &authErr) {
		msg := i18n.M.ProviderErrAuth
		if authErr.HasKey {
			msg = i18n.M.ProviderErrAuthRejected
		}
		switch {
		case authErr.KeyEnv != "" && authErr.KeySource != "":
			msg = fmt.Sprintf("%s (%s from %s)", msg, authErr.KeyEnv, authErr.KeySource)
		case authErr.KeyEnv != "":
			msg = fmt.Sprintf("%s (%s)", msg, authErr.KeyEnv)
		}
		// Relays explain *why* auth failed in the body ("token expired", key
		// not entitled to the model) — as diagnostic here as on APIError, but
		// auth bodies also echo credentials, so scrub key material first.
		if reason := redactAuthReason(providerBodyReason(authErr.Body)); reason != "" {
			return fmt.Errorf("%s\n%s", msg, reason)
		}
		return errors.New(msg)
	}
	return err
}

// apiErrorReason returns the provider's verbatim reason for a failed request —
// the localized line names the category, the body names the actual cause
// (context-length exceeded, unpaired tool_calls, a relay's "no available
// channel"). Every mapped status surfaces its body, not just the
// request-shaped 4xx: relay gateways wrap the real failure — dead upstream
// channel, unsupported tools, exhausted quota — in a 402/429/5xx body, and
// without it those errors are undiagnosable from the category line alone.
func apiErrorReason(e *provider.APIError) string {
	details := make([]string, 0, 3)
	if reason := providerBodyReason(e.Body); reason != "" {
		details = append(details, reason)
	}
	if traceID := strings.TrimSpace(e.TraceID); traceID != "" {
		details = append(details, "Trace ID: "+clampRunes(traceID, 200))
	}
	if e.ToolContext != "" {
		details = append(details, e.ToolContext)
	}
	return strings.Join(details, "\n")
}

var (
	miniMax1026CodeRe = regexp.MustCompile(`(^|[^0-9])1026([^0-9]|$)`)
	miniMax1027CodeRe = regexp.MustCompile(`(^|[^0-9])1027([^0-9]|$)`)
)

// providerContentSafetyMessage recognizes MiniMax's provider-specific content
// review failures before the generic HTTP 422 mapping calls them invalid
// parameters. A custom-named MiniMax provider is still recognized by the
// documented status text; numeric-only errors require a MiniMax provider name
// so another OpenAI-compatible API cannot accidentally inherit this meaning.
func providerContentSafetyMessage(e *provider.APIError) string {
	if e == nil || e.Status != 422 {
		return ""
	}
	body := strings.ToLower(e.Body)
	providerName := strings.ToLower(e.Provider)
	isMiniMax := strings.Contains(providerName, "minimax")
	switch {
	case strings.Contains(body, "input new_sensitive") || isMiniMax && miniMax1026CodeRe.MatchString(body):
		return i18n.M.ProviderErrInputSensitive
	case strings.Contains(body, "output new_sensitive") || isMiniMax && miniMax1027CodeRe.MatchString(body):
		return i18n.M.ProviderErrOutputSensitive
	default:
		return ""
	}
}

// redactAuthReason scrubs key material from an auth-failure reason before
// display. Deliberately applied only to 401/403 bodies: other statuses don't
// carry credentials, and 400 schema errors legitimately contain long
// identifiers that this stronger scrub would mangle.
func redactAuthReason(s string) string {
	return secrets.RedactCredentials(s)
}

// providerBodyReason pulls the human reason from an OpenAI/Anthropic-shaped
// error body ({"error":{"message":…}}) or MiniMax's base_resp envelope,
// falling back to the trimmed raw body.
func providerBodyReason(body string) string {
	if body == "" {
		return ""
	}
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		BaseResp struct {
			StatusMsg string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if json.Unmarshal([]byte(body), &parsed) == nil {
		switch {
		case parsed.Error.Message != "":
			return clampRunes(parsed.Error.Message, 800)
		case parsed.BaseResp.StatusMsg != "":
			return clampRunes(parsed.BaseResp.StatusMsg, 800)
		}
	}
	return clampRunes(body, 800)
}

func clampRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
