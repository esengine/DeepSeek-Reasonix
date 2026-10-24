package secrets

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"reasonix/internal/provider"
)

// FailureDetail preserves a bounded, credential-redacted cause for local
// history. API response envelopes may echo requests; retain only their error
// message, never the entire body. Redact before truncating credential values.
func FailureDetail(err error) string {
	if err == nil {
		return ""
	}
	text := failureText(err)
	text = strings.TrimSpace(RedactCredentials(text))
	runes := []rune(text)
	if len(runes) > 4096 {
		text = string(runes[:4096]) + "…"
	}
	return text
}

func failureText(err error) string {
	var image *provider.ImageInputError
	if errors.As(err, &image) && image.Err != nil {
		return "Image processing failed: " + failureText(image.Err)
	}
	var api *provider.APIError
	if errors.As(err, &api) {
		copy := *api
		copy.Body = ""
		return copy.Error() + failureBodyReason(api.Body)
	}
	var auth *provider.AuthError
	if errors.As(err, &auth) {
		copy := *auth
		copy.Body = ""
		return copy.Error() + failureBodyReason(auth.Body)
	}
	var request *provider.RequestFailure
	if errors.As(err, &request) && request.Err != nil {
		return provider.ProviderDisplayLabel(request.Identity.Provider, request.Identity.DisplayName, request.Identity.Protocol) + ": " + request.Operation + ": " + failureText(request.Err)
	}
	var transport *url.Error
	if errors.As(err, &transport) && transport.Err != nil {
		address := "[invalid URL]"
		if parsed, parseErr := url.Parse(transport.URL); parseErr == nil {
			parsed.User, parsed.RawQuery, parsed.Fragment, parsed.RawFragment = nil, "", "", ""
			address = parsed.String()
		}
		return transport.Op + " " + address + ": " + failureText(transport.Err)
	}
	return err.Error()
}

func failureBodyReason(body string) string {
	var value struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		BaseResp struct {
			StatusMsg string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if json.Unmarshal([]byte(body), &value) != nil {
		return ""
	}
	if value.Error.Message != "" {
		return ": " + value.Error.Message
	}
	if value.BaseResp.StatusMsg != "" {
		return ": " + value.BaseResp.StatusMsg
	}
	return ""
}
