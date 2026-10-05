package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"unicode"
	"unicode/utf8"

	"reasonix/internal/base/externalurl"
	"reasonix/internal/contract/tool"
)

func elicitURL(ctx context.Context, e tool.Elicitor, server string, params json.RawMessage) (map[string]any, error) {
	var p struct {
		Message *string         `json:"message"`
		URL     string          `json:"url"`
		Schema  json.RawMessage `json:"requestedSchema"`
	}
	if json.Unmarshal(params, &p) != nil || p.Message == nil || utf8.RuneCountInString(*p.Message) > elicitMaxMessage || len(p.Schema) > 0 {
		return nil, fmt.Errorf("%w: invalid URL-mode request", errBadElicitation)
	}
	if !validElicitURL(p.URL) {
		return nil, fmt.Errorf("%w: invalid external URL", errBadElicitation)
	}
	if e == nil {
		return map[string]any{"action": "decline"}, nil
	}
	reply, err := e.Elicit(ctx, tool.ElicitRequest{Source: server, Message: *p.Message, URL: p.URL})
	if err != nil {
		return map[string]any{"action": "cancel"}, nil
	}
	action := "decline"
	if !reply.Declined && (reply.Action == "accept" || reply.Action == "cancel") {
		action = reply.Action
	}
	return map[string]any{"action": action}, nil
}

func validElicitURL(raw string) bool {
	if raw == "" || len(raw) > 8192 {
		return false
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	u, err := url.Parse(raw)
	_, _, hostErr := externalurl.Host(raw)
	return err == nil && hostErr == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Opaque == ""
}
