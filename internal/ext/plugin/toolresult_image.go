// toolresult_image.go — what an MCP tool result's image must satisfy to travel.
package plugin

import (
	"encoding/base64"
	"fmt"
	"strings"

	"reasonix/internal/model/visionimage"
)

// Tool-result images ride to vision models as base64 data URLs, so each item is
// budgeted here rather than trusted from the MCP server: anything oversized,
// unparseable, past the per-result count, or of an unaccepted mime type becomes
// a text placeholder instead of a poisoned provider request.
const (
	maxToolResultImageBytes = 4 << 20 // base64 length; stays under provider per-image and request caps
	maxToolResultImages     = 5
)

var toolResultImageMimes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// toolResultImage validates one MCP image content item and returns its text
// placeholder plus the data URL to forward ("" when the item is dropped).
func toolResultImage(mime, data string, kept int) (placeholder, url string) {
	if kept >= maxToolResultImages {
		return "[image omitted: per-result image limit reached]", ""
	}
	mime = strings.ToLower(strings.TrimSpace(mime))
	if mime == "" {
		mime = "image/png"
	}
	if !toolResultImageMimes[mime] {
		return "[image omitted: unsupported type " + mime + "]", ""
	}
	// Some servers wrap base64 in whitespace; vision APIs reject non-canonical
	// payloads, so normalize before validating.
	data = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t', ' ':
			return -1
		}
		return r
	}, data)
	if data == "" {
		return "[image omitted: no data]", ""
	}
	if len(data) > maxToolResultImageBytes {
		return fmt.Sprintf("[image omitted: %d bytes exceeds the %d-byte limit]", len(data), maxToolResultImageBytes), ""
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "[image omitted: invalid base64]", ""
	}
	// The declared type is a claim; the bytes decide, and an image no host will
	// accept is dropped here rather than after it has poisoned the session.
	fitted, fittedMime, err := visionimage.Fit(raw, visionimage.DetectMime(raw))
	if err != nil {
		return "[image omitted: " + err.Error() + "]", ""
	}
	if fittedMime == mime && len(fitted) == len(raw) {
		return "[image: " + mime + "]", "data:" + mime + ";base64," + data
	}
	return "[image: " + fittedMime + "]", "data:" + fittedMime + ";base64," + base64.StdEncoding.EncodeToString(fitted)
}
