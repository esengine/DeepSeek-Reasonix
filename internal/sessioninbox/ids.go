package sessioninbox

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

var processRunID = sync.OnceValue(func() string {
	return newRandomID()
})

// ProcessRunID returns a process-stable run identifier used to detect
// cross-process crash recovery without mistaking tab switches for crashes.
func ProcessRunID() string {
	return processRunID()
}

func newRandomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is exceptional; fall back to a hashed time-ish value.
		sum := sha256.Sum256(fmt.Appendf(nil, "fallback-%p", &b))
		return hex.EncodeToString(sum[:16])
	}
	// UUID-like layout without external deps: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// PreviewText returns a bounded single-line preview without materializing the
// whole body as []rune when unnecessary.
func PreviewText(text string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = DefaultPreviewRunes
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	// Collapse internal whitespace for the shelf preview.
	var b strings.Builder
	b.Grow(min(len(text), maxRunes*4))
	prevSpace := false
	count := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		if r == utf8.RuneError && size == 1 {
			continue
		}
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			if prevSpace || count == 0 {
				continue
			}
			b.WriteByte(' ')
			prevSpace = true
			count++
			if count >= maxRunes {
				break
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
		count++
		if count >= maxRunes {
			// Peek if more content remains.
			if i < len(text) {
				b.WriteString("…")
			}
			break
		}
	}
	return strings.TrimSpace(b.String())
}
