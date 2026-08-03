package config

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// encodeTOMLString encodes value as a TOML 1.0 basic string, including the
// surrounding double quotes. It is the single encoding entry point for every
// string rendered into a TOML config file:
//
//   - backslash and double quote are escaped (`\\`, `\"`);
//   - tab, newline, form feed, carriage return and backspace use TOML escapes;
//   - remaining control characters (U+0000..U+001F) and DEL (U+007F) use
//     `\uXXXX`;
//   - valid UTF-8 (Chinese, emoji, ...) is preserved verbatim;
//   - invalid UTF-8 returns an error instead of being silently replaced.
//
// Unlike strconv.Quote, the output never contains Go-only escapes (`\a`,
// `\v`, `\xNN`) that the TOML parser rejects.
func encodeTOMLString(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("encode TOML string: invalid UTF-8")
	}
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}

// encodeTOMLKeyPart encodes a TOML key: bare keys are returned verbatim and
// every other key is encoded as a quoted basic string. Value encoding rules
// must not be reused for key names (a value may contain arbitrary UTF-8 while
// a key additionally needs to stay readable as an identifier).
func encodeTOMLKeyPart(key string) (string, error) {
	if isBareTOMLKey(key) {
		return key, nil
	}
	return encodeTOMLString(key)
}

// tomlRenderer records the first TOML encoding failure while a config is being
// rendered. Render helpers use it instead of strconv.Quote/%q so the write
// pipeline can refuse to persist output that would not parse back (for
// example a value containing invalid UTF-8).
type tomlRenderer struct {
	err error
}

func (r *tomlRenderer) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// q renders a string as a TOML basic string value. On an encoding error the
// renderer is poisoned and the caller must discard the output.
func (r *tomlRenderer) q(s string) string {
	encoded, err := encodeTOMLString(s)
	if err != nil {
		r.fail(err)
		return `""`
	}
	return encoded
}

// key renders a TOML key part (bare when possible, quoted otherwise).
func (r *tomlRenderer) key(s string) string {
	encoded, err := encodeTOMLKeyPart(s)
	if err != nil {
		r.fail(err)
		return `""`
	}
	return encoded
}

// stringArray renders a []string as a TOML inline array of basic strings.
func (r *tomlRenderer) stringArray(ss []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range ss {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(r.q(s))
	}
	b.WriteByte(']')
	return b.String()
}

// stringMap renders a map[string]string as a TOML inline table with keys in
// sorted order so output is deterministic (round-trips cleanly).
func (r *tomlRenderer) stringMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{ ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s = %s", r.key(k), r.q(m[k]))
	}
	b.WriteString(" }")
	return b.String()
}
