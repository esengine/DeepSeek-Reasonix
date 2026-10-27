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
	TokenShadow TokenKind = "shadow"
)

// Tokens is the pack-facing vocabulary; a frontend maps it onto its own CSS
// variables. Absent on purpose: ok/warn/err/net/deleg carry meaning rather than
// taste, a pill radius is a shape, and a stretched transition would make the app
// feel broken. link, brand, halo and labelAgent are decoration — a hyperlink, the
// product accent, an interaction halo, an agent's name — so they are taste.
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
	"accentInk":   TokenColour,
	"link":        TokenColour,
	"brand":       TokenColour,
	"halo":        TokenColour,
	"labelAgent":  TokenColour,
	"float":       TokenColour,
	"floatHi":     TokenColour,
	"codeBg":      TokenColour,
	"sunkBg":      TokenColour,
	"synKeyword":  TokenColour,
	"synString":   TokenColour,
	"synNumber":   TokenColour,
	"synFunction": TokenColour,
	// The five optional code colours. A pack that sets none keeps what each
	// surface draws today; see docs/THEME_AUTHOR_GUIDE.md.
	"synComment":     TokenColour,
	"synType":        TokenColour,
	"synOperator":    TokenColour,
	"synPunctuation": TokenColour,
	"synVariable":    TokenColour,
	"shadowFloat":    TokenShadow,
	"radiusXs":       TokenLength,
	"radiusSm":       TokenLength,
	"radiusMd":       TokenLength,
	"fontUi":         TokenFont,
	"fontMono":       TokenFont,
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
	case TokenShadow:
		return isShadow(value)
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

// maxShadowLayers and maxShadowPx bound a shadow the way maxRadiusPx bounds a
// corner: a sanity limit against an accidental wall of paint, not a taste one.
const (
	maxShadowLayers = 4
	maxShadowPx     = 100
)

// isShadow keeps a box-shadow list: "none", or up to four comma-separated
// layers of an optional inset, two to four px offsets and a hex colour. The
// blur and the spread are the third and fourth lengths; a negative blur is not
// CSS and a spread may be negative. No functions, no named colours.
func isShadow(v string) bool {
	v = strings.TrimSpace(v)
	if v == "none" {
		return true
	}
	if v == "" || len(v) > 240 {
		return false
	}
	// Fields would split on any Unicode space and the stored value is the
	// original string, which CSS does not read as a separator: ASCII only.
	for _, r := range v {
		if r < ' ' || r > '~' {
			return false
		}
	}
	layers := strings.Split(v, ",")
	if len(layers) > maxShadowLayers {
		return false
	}
	for _, layer := range layers {
		if !isShadowLayer(strings.FieldsFunc(layer, func(r rune) bool { return r == ' ' })) {
			return false
		}
	}
	return true
}

func isShadowLayer(parts []string) bool {
	if len(parts) > 0 && parts[0] == "inset" {
		parts = parts[1:]
	}
	if len(parts) < 3 || len(parts) > 5 || !isColour(parts[len(parts)-1]) {
		return false
	}
	lengths := parts[:len(parts)-1]
	if len(lengths) < 2 || len(lengths) > 4 {
		return false
	}
	for i, l := range lengths {
		if !isSignedPx(l, i != 2) {
			return false
		}
	}
	return true
}

// isSignedPx is a plain px length, or the unitless zero, within maxShadowPx.
func isSignedPx(v string, mayBeNegative bool) bool {
	if v == "0" {
		return true
	}
	digits, negative := strings.CutPrefix(v, "-")
	if negative && !mayBeNegative {
		return false
	}
	digits, ok := strings.CutSuffix(digits, "px")
	if !ok || !plainDecimal(digits) {
		return false
	}
	n, err := strconv.ParseFloat(digits, 64)
	return err == nil && n <= maxShadowPx
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
