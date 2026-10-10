package textutil

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// PreviewMarker ends every field cut short by a PreviewLimit.
const PreviewMarker = "…"

// PreviewLimit bounds one displayed field. Graphemes counts user-perceived
// characters, marker included; Lines counts newline-separated rows.
type PreviewLimit struct {
	Graphemes int
	Lines     int
}

// BoundProse prepares author-supplied prose for an approval surface. Escape
// sequences and invisible characters are removed, line breaks survive up to
// the limit, and the second result reports whether anything was cut off or
// removed, since the reader cannot tell that text was deleted. Output is a pure
// function of the input and comes back unchanged when none of that applies.
func BoundProse(s string, lim PreviewLimit) (string, bool) {
	s, cut := capInput(s, lim)
	valid := strings.ToValidUTF8(s, "\uFFFD")
	s = ansi.Strip(valid)
	removed := s != valid
	rs := []rune(s)
	hidden := hiddenMask(rs)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range rs {
		switch {
		case r == '\n', r == '\u2028', r == '\u2029', r == '\u0085':
			b.WriteByte('\n')
		case r == '\r':
			if i+1 == len(rs) || rs[i+1] != '\n' {
				b.WriteByte('\n')
			}
		case r == '\t':
			b.WriteByte('\t')
		case hidden[i]:
			removed = true
		default:
			b.WriteRune(r)
		}
	}
	out, clipped := clipLines(b.String(), lim)
	return markCapped(out, lim, cut && !clipped), cut || clipped || removed
}

// BoundLiteral prepares text that names or runs something. Nothing is removed:
// every control, escape, line break and invisible character is rendered as a
// visible \u{hex} escape, so what the reader sees is what is there. A literal
// backslash that begins "u{" is escaped too, so typed text cannot pass for one.
func BoundLiteral(s string, lim PreviewLimit) (string, bool) {
	s, cut := capInput(s, lim)
	rs := []rune(strings.ToValidUTF8(s, "\uFFFD"))
	hidden := hiddenMask(rs)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range rs {
		if hidden[i] || (r == '\\' && i+2 < len(rs) && rs[i+1] == 'u' && rs[i+2] == '{') {
			fmt.Fprintf(&b, `\u{%x}`, r)
			continue
		}
		b.WriteRune(r)
	}
	out, clipped := clipGraphemes(b.String(), lim.Graphemes)
	return markCapped(out, lim, cut && !clipped), cut || clipped
}

func capInput(s string, lim PreviewLimit) (string, bool) {
	limit := max(lim.Graphemes, 1)*64 + 1024
	if len(s) <= limit {
		return s, false
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit], true
}

func markCapped(s string, lim PreviewLimit, capped bool) string {
	if !capped {
		return s
	}
	g := uniseg.NewGraphemes(s)
	pos, last := 0, 0
	for g.Next() {
		last = pos
		pos += len(g.Str())
	}
	out, _ := clipGraphemes(s[:last]+PreviewMarker, lim.Graphemes)
	return out
}

func clipLines(s string, lim PreviewLimit) (string, bool) {
	lines := strings.Split(s, "\n")
	if n := max(lim.Lines, 1); len(lines) > n {
		if strings.TrimSpace(strings.Join(lines[n:], "\n")) != "" {
			head := strings.TrimRight(strings.Join(lines[:n], "\n"), "\n")
			out, _ := clipGraphemes(head+PreviewMarker, lim.Graphemes)
			return out, true
		}
		lines = lines[:n]
	}
	return clipGraphemes(strings.Join(lines, "\n"), lim.Graphemes)
}

// clipGraphemes keeps s whole when it fits and otherwise returns its first
// limit-1 grapheme clusters plus the marker, so the result never exceeds limit.
func clipGraphemes(s string, limit int) (string, bool) {
	limit = max(limit, 1)
	g := uniseg.NewGraphemes(s)
	pos, keep, n := 0, 0, 0
	for g.Next() {
		n++
		if n == limit {
			keep = pos
		}
		if n > limit {
			return s[:keep] + PreviewMarker, true
		}
		pos += len(g.Str())
	}
	return s, false
}

const maxMarksPerBase = 6

// hiddenMask marks every rune that changes how neighbouring text renders or
// does not render at all. A joiner stays where its script or an emoji needs it,
// an emoji presentation selector stays after a pictograph, and combining marks
// beyond maxMarksPerBase on one base are marked so they cannot overdraw
// neighbouring lines.
func hiddenMask(rs []rune) []bool {
	mask := make([]bool, len(rs))
	run := 0
	for i, r := range rs {
		mask[i] = hiddenRune(rs, i)
		switch {
		case mask[i]:
		case unicode.In(r, unicode.Mn, unicode.Me):
			run++
			mask[i] = run > maxMarksPerBase
		default:
			run = 0
		}
	}
	return mask
}

func hiddenRune(rs []rune, i int) bool {
	r := rs[i]
	switch {
	case isControl(r), isFormat(r) && r != 0x200c && r != 0x200d, unicode.Is(unicode.Cs, r), unicode.Is(unicode.Co, r):
		return true
	case r == 0x200c || r == 0x200d:
		return !joinerNeeded(rs, i)
	case r == 0x2028 || r == 0x2029 || r == 0x2800:
		return true
	case r == 0xfe0e || r == 0xfe0f:
		return i == 0 || !(pictograph(rs[i-1]) || isKeycapBase(rs[i-1]))
	case r >= 0xfe00 && r <= 0xfe0d, r >= 0xe0100 && r <= 0xe01ef:
		return true
	}
	switch r {
	case 0x034f, 0x115f, 0x1160, 0x17b4, 0x17b5, 0x3164, 0xffa0:
		return true
	}
	return r >= 0x180b && r <= 0x180f
}

func isKeycapBase(r rune) bool { return r == '#' || r == '*' || (r >= '0' && r <= '9') }

func joinerNeeded(rs []rune, i int) bool {
	if i == 0 || i+1 >= len(rs) {
		return false
	}
	prev, next := rs[i-1], rs[i+1]
	if pictograph(prev) && pictograph(next) {
		return rs[i] == 0x200d
	}
	return shapesWithJoiner(prev) && shapesWithJoiner(next)
}

func pictograph(r rune) bool {
	switch {
	case r >= 0x1f000 && r <= 0x1faff, r >= 0x2190 && r <= 0x2bff:
		return true
	}
	switch r {
	case 0xa9, 0xae, 0x203c, 0x2049, 0x3030, 0x303d, 0x3297, 0x3299, 0xfe0f:
		return true
	}
	return false
}

func shapesWithJoiner(r rune) bool {
	if r < 0x80 || !(unicode.IsLetter(r) || unicode.IsMark(r)) {
		return false
	}
	return !unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Latin, unicode.Cyrillic, unicode.Greek)
}

// The limits every approval and package-detail surface shares. Identity names a
// thing, locator is where it lives or what it runs, prose is free text.
var (
	PreviewIdentity = PreviewLimit{Graphemes: 128, Lines: 1}
	PreviewLocator  = PreviewLimit{Graphemes: 1024, Lines: 1}
	PreviewProse    = PreviewLimit{Graphemes: 600, Lines: 6}
)

// Item caps for lists shown beside those limits.
const (
	MaxIdentityItems = 100
	MaxLocatorItems  = 64
	MaxProseItems    = 50
	MaxActions       = 50
)

// ShownIdentity, ShownLocator and ShownProse are the bounded forms for a
// surface with no room for a truncation flag: the ellipsis is the signal.
func ShownIdentity(s string) string { out, _ := BoundLiteral(s, PreviewIdentity); return out }
func ShownLocator(s string) string  { out, _ := BoundLiteral(s, PreviewLocator); return out }
func ShownProse(s string) string    { out, _ := BoundProse(s, PreviewProse); return out }
