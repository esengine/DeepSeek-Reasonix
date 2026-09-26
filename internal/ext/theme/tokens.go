// tokens.go — what a pack may set, and what a value of that kind may look like.
package theme

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// TokenKind classifies a token by the CSS grammar its value ends up in. Every
// value is interpolated into a stylesheet, so each kind has one strict pattern
// and a value that does not match is dropped rather than escaped.
type TokenKind string

const (
	TokenColour TokenKind = "colour"
	TokenLength TokenKind = "length"
	TokenFont   TokenKind = "font"
)

// Tokens is the pack-facing vocabulary; a frontend maps it onto its own CSS
// variables. Absent on purpose: ok/warn/err/net/deleg carry meaning rather than
// taste, a pill radius is a shape, and a stretched transition would make the app
// feel broken. Floating, code and sunken grounds and syntax colours are taste,
// so a pack sets them instead of leaving them on the default palette.
var Tokens = map[string]TokenKind{
	"bg":          TokenColour,
	"bgSoft":      TokenColour,
	"bgElev":      TokenColour,
	"panel":       TokenColour,
	"border":      TokenColour,
	"borderSoft":  TokenColour,
	"fg":          TokenColour,
	"fgDim":       TokenColour,
	"fgFaint":     TokenColour,
	"fgStrong":    TokenColour,
	"accent":      TokenColour,
	"accentFg":    TokenColour,
	"float":       TokenColour,
	"floatHi":     TokenColour,
	"codeBg":      TokenColour,
	"sunkBg":      TokenColour,
	"synKeyword":  TokenColour,
	"synString":   TokenColour,
	"synNumber":   TokenColour,
	"synFunction": TokenColour,
	"radiusXs":    TokenLength,
	"radiusSm":    TokenLength,
	"radiusMd":    TokenLength,
	"fontUi":      TokenFont,
	"fontMono":    TokenFont,
}

// TokenNames returns the vocabulary sorted, for callers that report it.
func TokenNames() []string {
	out := slices.Sorted(maps.Keys(Tokens))
	return out
}

// validToken reports whether name is in the vocabulary and value fits its kind.
func validToken(name, value string) bool {
	switch Tokens[name] {
	case TokenColour:
		return isColour(value)
	case TokenLength:
		return isLength(value)
	case TokenFont:
		return isFontStack(value)
	default:
		return false
	}
}

// maxRadiusPx bounds a length so a pack cannot round a card into a circle by
// accident. It is a sanity limit, not a taste one — 64px is already extreme.
const maxRadiusPx = 64

// isLength keeps a plain px or rem length. No calc(), no var(), no unitless
// value that would silently mean nothing.
func isLength(v string) bool {
	v = strings.TrimSpace(v)
	// Zero is the one length CSS writes without a unit, and an author squaring
	// off every corner will write it that way.
	if v == "0" {
		return true
	}
	var digits string
	scale := 1.0
	switch {
	case strings.HasSuffix(v, "px"):
		digits = strings.TrimSuffix(v, "px")
	case strings.HasSuffix(v, "rem"):
		digits, scale = strings.TrimSuffix(v, "rem"), 16
	default:
		return false
	}
	// plainDecimal has already refused the empty string, the signs and the
	// exponents, so what reaches ParseFloat is a non-negative decimal.
	if !plainDecimal(digits) {
		return false
	}
	n, err := strconv.ParseFloat(digits, 64)
	return err == nil && n*scale <= maxRadiusPx
}

// plainDecimal is digits with at most one dot — what CSS is written in, minus
// the signs, exponents and infinities strconv would otherwise accept.
func plainDecimal(s string) bool {
	if s == "" || s == "." || len(s) > 8 {
		return false
	}
	dot := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return true
}

// isFontStack keeps a family list: names, separators, and quotes. Everything
// that could end the declaration and start another one — ; } ( ) \ @ — or
// reach the network through url() is absent from the allowed set, so the value
// cannot leave the font-family property it is written into.
func isFontStack(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 200 {
		return false
	}
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ' ' || r == ',' || r == '-' || r == '_' || r == '.' || r == '\'' || r == '"':
		case r > 0x7F && unicode.IsLetter(r):
			// A CJK family name is spelled in its own script. Letters only, so
			// no control or formatting character rides in on this branch.
		default:
			return false
		}
	}
	return true
}

// dropReason says which half of the contract a token missed: the vocabulary or
// the grammar of its kind. Both are actionable, and they are different fixes.
func dropReason(scheme, name, value string) string {
	kind, known := Tokens[name]
	if !known {
		return fmt.Sprintf("%s: %q is not a theme token", scheme, name)
	}
	return fmt.Sprintf("%s: %q is not a valid %s value for %q", scheme, value, kind, name)
}
