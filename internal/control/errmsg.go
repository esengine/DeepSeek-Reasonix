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
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
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
	reason := providerBodyReason(e.Body)
	if e.ToolContext == "" {
		return reason
	}
	if reason == "" {
		return e.ToolContext
	}
	return reason + "\n" + e.ToolContext
}

// Auth failure bodies are where servers echo credentials: providers include a
// masked tail ("Your api key: ****ae54 is invalid") and a sloppy relay can
// reflect the full key it received. Both narrow or reveal the key, and the
// displayed turn error can travel further than the user's terminal (bot
// forwarding, shared screenshots).
var (
	// maskedFragmentRe matches a partially masked credential and any visible
	// prefix/suffix around the stars ("****ae54", "sk-ab****") — including the
	// head/tail remnants secrets.Redact's own mask leaves behind. For auth
	// display even the remnant narrows the key, so the whole run collapses.
	maskedFragmentRe = regexp.MustCompile(`[A-Za-z0-9._-]*\*{2,}[A-Za-z0-9._-]*`)
	// credContextRe masks any long value that follows a credential word.
	// secrets.Redact's KEY=value rule only matches API_KEY/APIKEY-style names;
	// providers write prose forms like "api key: <value>", where the value must
	// go regardless of its composition.
	credContextRe = regexp.MustCompile(`(?i)\b(api[ _-]?key|access[ _-]?key|secret|token|authorization|bearer|credential)s?\b(['"]?\s*[:=]?\s*['"]?)([A-Za-z0-9._~+/-]{12,})`)
	// keyTokenRe matches key-shaped runs for the no-context fallback; a run is
	// treated as a credential when it carries a digit or mixed case, so
	// single-case digit-free identifiers like "invalid_authentication_token"
	// stay readable.
	keyTokenRe = regexp.MustCompile(`[A-Za-z0-9_-]{16,}`)
	digitRe    = regexp.MustCompile(`[0-9]`)
)

// redactAuthReason scrubs key material from an auth-failure reason before
// display, in layers: secrets.Redact for known key shapes (sk-/rk- prefixes,
// Bearer, JWT, KEY=value forms), the credential-context rule for prose forms,
// full collapse of masked fragments, then the key-shaped-token fallback. The
// residual blind spot is a long single-case digit-free token with no context
// word — indistinguishable from an identifier. Deliberately applied only to
// 401/403 bodies: other statuses don't carry credentials, and 400 schema
// errors legitimately contain long identifiers that this scrub would mangle.
func redactAuthReason(s string) string {
	if s == "" {
		return s
	}
	s = secrets.Redact(s)
	s = credContextRe.ReplaceAllString(s, "${1}${2}****")
	s = maskedFragmentRe.ReplaceAllString(s, "****")
	return keyTokenRe.ReplaceAllStringFunc(s, func(tok string) string {
		mixedCase := strings.ToLower(tok) != tok && strings.ToUpper(tok) != tok
		if digitRe.MatchString(tok) || mixedCase {
			return "****"
		}
		return tok
	})
}

// providerBodyReason pulls the human reason from an OpenAI/Anthropic-shaped error
// body ({"error":{"message":…}}), falling back to the trimmed raw body.
func providerBodyReason(body string) string {
	if body == "" {
		return ""
	}
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &parsed) == nil && parsed.Error.Message != "" {
		return clampRunes(parsed.Error.Message, 800)
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
