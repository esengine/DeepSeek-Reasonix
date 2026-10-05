package provider

import "strings"

// ParseImageDataURL splits a `data:<media-type>;base64,<payload>` URL into its
// media type and base64 payload. ok is false for anything that isn't a base64
// data URL — providers that need the split (Anthropic) skip those silently.
func ParseImageDataURL(dataURL string) (mediaType, base64Data string, ok bool) {
	rest, found := strings.CutPrefix(dataURL, "data:")
	if !found {
		return "", "", false
	}
	meta, payload, found := strings.Cut(rest, ",")
	if !found {
		return "", "", false
	}
	mt, found := strings.CutSuffix(meta, ";base64")
	if !found || mt == "" {
		return "", "", false
	}
	return mt, payload, true
}
