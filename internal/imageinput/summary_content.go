package imageinput

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"reasonix/internal/provider"
)

func ImageDigest(ref string) string {
	ref = strings.TrimSpace(ref)
	if media, payload, ok := provider.ParseImageDataURL(ref); ok {
		if raw, err := base64.StdEncoding.DecodeString(payload); err == nil {
			h := sha256.New()
			h.Write([]byte(media))
			h.Write(raw)
			return hex.EncodeToString(h.Sum(nil))
		}
	}
	sum := sha256.Sum256([]byte(ref))
	return hex.EncodeToString(sum[:])
}

func visionSummaryContext(summary *provider.VisionSummary) string {
	if summary == nil || strings.TrimSpace(summary.Summary) == "" {
		return ""
	}
	return fmt.Sprintf("<reasonix-image-context version=\"%d\" untrusted=\"true\">\n%s\n</reasonix-image-context>", summary.Version, "Image understanding summary from "+summary.ModelRef+":\n"+strings.TrimSpace(summary.Summary))
}

func AppendSummary(input string, summary *provider.VisionSummary) string {
	block := visionSummaryContext(summary)
	if block == "" {
		return input
	}
	return strings.TrimRight(input, "\n") + "\n\n" + block
}

func sameImageDigests(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func boundedVisionSummary(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= visionSummaryMaxBytes {
		return text
	}
	limit := visionSummaryMaxBytes - len("\n[summary truncated]")
	cut := text[:limit]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "\n[summary truncated]"
}
