package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"reasonix/internal/i18n"
	"reasonix/internal/provider"
	"reasonix/internal/secrets"
	"reasonix/internal/turnevent"
)

// explainError maps a provider HTTP failure to an actionable, localized message
// so the turn-done error the UI shows is never a bare status code or silent
// failure. Unknown errors (and nil) pass through unchanged.
func explainError(err error) error {
	if err == nil {
		return nil
	}
	// Filesystem errors can satisfy net.Error on some platforms. Preserve the
	// storage sentinel before provider retry classification so a poisoned WAL
	// is never reported as a model-stream disconnect.
	if errors.Is(err, turnevent.ErrTurnLedgerUnavailable) {
		return err
	}
	// The exhausted wait wraps its transport cause; explain the wait itself
	// before the connect/status branches below explain that cause instead.
	if wait := provider.AsRecoveryWaitExhausted(err); wait != nil {
		return &explainedError{msg: explainRecoveryWait(wait), cause: err}
	}
	if provider.IsStreamInterrupted(err) {
		return &explainedError{msg: fmt.Sprintf("model stream interrupted after recovery attempts: %s. The partial response was kept; retry or ask Reasonix to continue", err.Error()), cause: err}
	}
	if provider.IsConnReset(err) {
		return &explainedError{msg: fmt.Sprintf("model stream disconnected before completion after retry attempts: %s. Check the provider/proxy connection, then retry or ask Reasonix to continue", err.Error()), cause: err}
	}
	// An overflow without token numbers has nothing to quote; the generic 400
	// branch below keeps the provider's own reason instead of zeros.
	if limit := provider.AsContextLimitError(err); limit != nil && limit.WindowTokens > 0 {
		msg := fmt.Sprintf(i18n.M.ProviderErrContextOverflowFmt, limit.PromptTokens, limit.CompletionTokens, limit.RequestedTokens, limit.WindowTokens)
		if reason := apiErrorReason(limit.APIError); reason != "" {
			msg = fmt.Sprintf("%s\n%s", msg, reason)
		}
		label := ""
		if limit.APIError != nil {
			label = provider.ProviderDisplayLabel(limit.APIError.Provider, limit.APIError.ProviderDisplayName, limit.APIError.Protocol)
		}
		return &explainedError{msg: providerFailureMessage(label, limit.APIError, msg), cause: err}
	}
	if quota := provider.AsQuotaError(err); quota != nil {
		label := provider.ProviderDisplayLabel(quota.Provider, quota.ProviderDisplayName, quota.Protocol)
		return &explainedError{msg: fmt.Sprintf(i18n.M.ProviderErrQuotaExhaustedFmt, label, quota.Status), cause: err}
	}
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		label := provider.ProviderDisplayLabel(apiErr.Provider, apiErr.ProviderDisplayName, apiErr.Protocol)
		if provider.IsOpaqueBadRequest(err) {
			if trace := provider.DiagnoseFailure(err).TraceID; trace != "" {
				return &explainedError{msg: providerFailureMessage(label, apiErr, fmt.Sprintf("%s\nTrace ID: %s", i18n.M.ProviderErrReasonMissing, trace)), cause: err}
			}
			return &explainedError{msg: providerFailureMessage(label, apiErr, i18n.M.ProviderErrReasonMissing), cause: err}
		}
		if msg := providerContentSafetyMessage(apiErr); msg != "" {
			if reason := apiErrorReason(apiErr); reason != "" {
				msg = fmt.Sprintf("%s\n%s", msg, reason)
			}
			return &explainedError{msg: providerFailureMessage(label, apiErr, msg), cause: err}
		}
		msg := i18n.M.ProviderStatusMessage(apiErr.Status)
		if msg == "" {
			return err
		}
		if reason := apiErrorReason(apiErr); reason != "" {
			msg = fmt.Sprintf("%s\n%s", msg, reason)
		}
		return &explainedError{msg: providerFailureMessage(label, apiErr, msg), cause: err}
	}
	var authErr *provider.AuthError
	if errors.As(err, &authErr) {
		label := provider.ProviderDisplayLabel(authErr.Provider, authErr.ProviderDisplayName, authErr.Protocol)
		reason := redactAuthReason(providerBodyReason(authErr.Body))
		if modelFormatMismatchReason(reason) {
			details := []string{i18n.M.ProviderErrModelFormatMismatch}
			lower := strings.ToLower(reason)
			isOpenCodeGo := strings.Contains(strings.ToLower(authErr.Provider), "opencode-go") || strings.EqualFold(authErr.KeyEnv, "OPENCODE_GO_API_KEY")
			if isOpenCodeGo && strings.Contains(lower, "grok-4.5") && strings.Contains(lower, "format anthropic") {
				details = append(details, i18n.M.ProviderErrOpenCodeGoGrokRoute)
			}
			if reason != "" {
				details = append(details, reason)
			}
			return &explainedError{msg: providerFailureMessage(label, authErr, strings.Join(details, "\n")), cause: err}
		}
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
		if reason != "" {
			msg = fmt.Sprintf("%s\n%s", msg, reason)
		}
		return &explainedError{msg: providerFailureMessage(label, authErr, msg), cause: err}
	}
	return err
}

func providerFailureMessage(label string, source any, message string) string {
	hasDisplayIdentity := false
	switch value := source.(type) {
	case *provider.APIError:
		hasDisplayIdentity = value != nil && (strings.TrimSpace(value.ProviderDisplayName) != "" || strings.TrimSpace(value.Protocol) != "")
	case *provider.AuthError:
		hasDisplayIdentity = value != nil && (strings.TrimSpace(value.ProviderDisplayName) != "" || strings.TrimSpace(value.Protocol) != "")
	}
	if !hasDisplayIdentity || strings.TrimSpace(label) == "" {
		return message
	}
	return label + ": " + message
}

// explainedError shows the localized message while keeping the typed cause
// reachable, so DiagnoseFailure on the TurnDone error still classifies it.
type explainedError struct {
	msg   string
	cause error
}

func (e *explainedError) Error() string { return e.msg }
func (e *explainedError) Unwrap() error { return e.cause }

func explainRecoveryWait(wait *provider.RecoveryWaitExhaustedError) string {
	lines := []string{fmt.Sprintf(i18n.M.ProviderErrWaitExhaustedFmt, wait.Waited.Round(time.Second))}
	var apiErr *provider.APIError
	switch {
	case errors.As(wait.Cause, &apiErr):
		lines = append(lines, fmt.Sprintf("HTTP %d", apiErr.Status))
		if reason := apiErrorReason(apiErr); reason != "" {
			lines = append(lines, reason)
		}
	case wait.Cause != nil:
		lines = append(lines, wait.Cause.Error())
	}
	return strings.Join(lines, "\n")
}

func modelFormatMismatchReason(reason string) bool {
	lower := strings.ToLower(strings.TrimSpace(reason))
	return strings.Contains(lower, "model") && strings.Contains(lower, "not supported for format")
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
