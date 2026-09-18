package openai

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"reasonix/internal/provider"
)

type wireImageLocation struct {
	WireMessageIndex int
	WireContentIndex int
	Identity         provider.ImageIdentity
}

type trackedImage struct {
	Ref      string
	Identity provider.ImageIdentity
}

var (
	bracketImageLocation = regexp.MustCompile(`(?i)messages\[(\d+)\](?:\.content\[(\d+)\]|\.images\[(\d+)\])`)
	dottedImageLocation  = regexp.MustCompile(`(?i)messages\.(\d+)\.(?:content|images)\.(\d+)`)
)

func (c *client) ImageRequestIdentity() provider.ImageRequestIdentity {
	if c == nil {
		return provider.ImageRequestIdentity{}
	}
	return provider.ImageRequestIdentity{
		Provider: c.name,
		Protocol: c.identity.Protocol,
		Endpoint: imageEndpointIdentity(c.chatURL),
		Model:    c.model,
		Variant:  c.visionDetail,
	}
}

func imageEndpointIdentity(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host) + parsed.EscapedPath()
}

func (c *client) classifyImageRequestError(err error, locations []wireImageLocation) error {
	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) || apiErr == nil || (apiErr.Status != 400 && apiErr.Status != 422) {
		return err
	}
	if !explicitBadImageRejection(apiErr.Body) {
		return err
	}
	messageIndex, contentIndex, located := rejectedImageLocation(apiErr.Body)
	matches := make([]wireImageLocation, 0, len(locations))
	for _, location := range locations {
		if located && location.WireMessageIndex != messageIndex {
			continue
		}
		if located && contentIndex >= 0 && location.WireContentIndex != contentIndex {
			continue
		}
		if strings.TrimSpace(location.Identity.MessageID) != "" {
			matches = append(matches, location)
		}
	}
	if len(matches) == 0 {
		return err
	}
	candidates := make([]provider.ImageIdentity, 0, len(matches))
	seen := map[provider.ImageIdentity]bool{}
	for _, match := range matches {
		if !seen[match.Identity] {
			seen[match.Identity] = true
			candidates = append(candidates, match.Identity)
		}
	}
	identity := c.ImageRequestIdentity()
	imageErr := &provider.ImageRequestError{
		APIError:   apiErr,
		Reason:     "provider_rejected_image",
		Candidates: candidates,
		Scope: provider.ImageIsolationScope{
			Kind: "provider", Provider: identity.Provider, Protocol: identity.Protocol,
			Endpoint: identity.Endpoint, Model: identity.Model, Variant: identity.Variant,
		},
	}
	if len(candidates) == 1 {
		imageErr.Identity = candidates[0]
	}
	return imageErr
}

func explicitBadImageRejection(body string) bool {
	lower := strings.ToLower(strings.TrimSpace(body))
	if lower == "" {
		return false
	}
	for _, excluded := range []string{"content_policy", "content policy", "safety", "moderation", "expired", "file not found", "authentication", "quota", "billing"} {
		if strings.Contains(lower, excluded) {
			return false
		}
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Type    string `json:"type"`
			Param   string `json:"param"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &envelope) == nil {
		lower = strings.ToLower(strings.Join([]string{lower, envelope.Error.Message, envelope.Error.Code, envelope.Error.Type, envelope.Error.Param}, " "))
	}
	for _, marker := range []string{
		"invalid image data", "invalid_image_data", "unsupported image format", "unsupported_image_format",
		"unsupported image type", "corrupt image", "corrupted image", "damaged image", "decode image",
		"image decode", "invalid base64", "invalid image format",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func rejectedImageLocation(body string) (messageIndex, contentIndex int, ok bool) {
	if match := bracketImageLocation.FindStringSubmatch(body); len(match) > 0 {
		message, err := strconv.Atoi(match[1])
		if err != nil {
			return 0, 0, false
		}
		content := -1
		value := match[2]
		if value == "" {
			value = match[3]
		}
		if value != "" {
			content, err = strconv.Atoi(value)
			if err != nil {
				return 0, 0, false
			}
		}
		return message, content, true
	}
	if match := dottedImageLocation.FindStringSubmatch(body); len(match) > 0 {
		message, err1 := strconv.Atoi(match[1])
		content, err2 := strconv.Atoi(match[2])
		return message, content, err1 == nil && err2 == nil
	}
	return 0, 0, false
}
