package builtin

import (
	"encoding/json"
	"strings"
)

// unmarshalArgs is a fault-tolerant replacement for json.Unmarshal(args, &v).
// It first attempts a standard unmarshal; if that fails, it applies a series of
// repairs that target the most common malformed JSON produced by third-party
// LLM providers (triple-quoted strings, trailing commas, outer-quote wrapping,
// single-quoted keys, unescaped control characters). Each repair is retried
// independently so a valid-but-unusual input (e.g. single-quoted keys from a
// Chinese model) still passes through.
func unmarshalArgs(args json.RawMessage, v any) error {
	if err := json.Unmarshal(args, v); err == nil {
		return nil
	}
	// Attempt progressive repairs, retrying unmarshal after each.
	s := string(args)
	for _, repair := range []func(string) string{
		stripMarkdownFence,
		stripOuterQuotes,
		fixTripleQuotes,
		stripTrailingCommas,
		fixSingleQuotes,
		fixPythonLiterals,
		fixUnescapedControls,
		stripComments,
	} {
		s = repair(s)
		if err := json.Unmarshal([]byte(s), v); err == nil {
			return nil
		}
	}
	// All repairs exhausted — return the original error so the model gets
	// the standard diagnostic and can self-correct.
	return json.Unmarshal(args, v)
}

// stripOuterQuotes handles the case where a model wraps the entire JSON object
// in an extra layer of quotes: `"{ ... }"` → `{ ... }`. This is common when
// smaller models confuse the tool-call argument string with its content.
func stripOuterQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		// Only strip if the inner content looks like a JSON object/array.
		trimmed := strings.TrimSpace(inner)
		if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
			// Unescape the inner content (the outer quotes caused \" escaping).
			var unescaped string
			if err := json.Unmarshal([]byte(s), &unescaped); err == nil {
				return unescaped
			}
		}
	}
	return s
}

// fixTripleQuotes replaces `"""` with `"` — a pattern emitted by some models
// that confuse Python-style triple quotes with JSON string delimiters.
// Example: `{"content": """hello\n"""}` → `{"content": "hello\n"}`
func fixTripleQuotes(s string) string {
	return strings.ReplaceAll(s, `"""`, `"`)
}

// stripTrailingCommas removes commas immediately before `}` or `]`, which
// are invalid in JSON but commonly emitted by models trained on Python/JS
// codebases where trailing commas are allowed.
// Example: `{"a": 1, "b": 2,}` → `{"a": 1, "b": 2}`
func stripTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			b.WriteByte(c)
			escape = false
			continue
		}
		if c == '\\' && inString {
			b.WriteByte(c)
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
			b.WriteByte(c)
			continue
		}
		if !inString && c == ',' {
			// Look ahead: skip whitespace, then check for } or ].
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // skip the trailing comma
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// fixSingleQuotes replaces single-quoted JSON strings with double-quoted ones.
// Some models (especially Chinese ones trained on Python-heavy corpora) emit
// {'key': 'value'} instead of {"key": "value"}.
func fixSingleQuotes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inDouble := false
	escape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			b.WriteByte(c)
			escape = false
			continue
		}
		if c == '\\' && inDouble {
			b.WriteByte(c)
			escape = true
			continue
		}
		if c == '"' {
			inDouble = !inDouble
			b.WriteByte(c)
			continue
		}
		if !inDouble && c == '\'' {
			// Check if this looks like a JSON key or value delimiter.
			// Replace single quote with double quote.
			b.WriteByte('"')
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// fixUnescapedControls replaces raw control characters (newlines, tabs) inside
// JSON string values with their proper escape sequences. Some models emit
// unescaped newlines in string content, which violates the JSON spec.
func fixUnescapedControls(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			b.WriteByte(c)
			escape = false
			continue
		}
		if c == '\\' && inString {
			b.WriteByte(c)
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
			b.WriteByte(c)
			continue
		}
		if inString && c < 0x20 {
			switch c {
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				// Other control chars — use unicode escape.
				b.WriteString(`\u`)
				b.WriteString(toHex4(rune(c)))
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func toHex4(r rune) string {
	const hex = "0123456789abcdef"
	return string([]byte{
		hex[(r>>12)&0xf],
		hex[(r>>8)&0xf],
		hex[(r>>4)&0xf],
		hex[r&0xf],
	})
}

// stripMarkdownFence removes ```json ... ``` or ``` ... ``` wrappers that some
// models wrap around tool-call arguments.
// Example: "```json\n{\"todos\":[]}\n```" → "{\"todos\":[]}"
func stripMarkdownFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Find the end of the opening fence line (```json or ```)
	firstNL := strings.IndexByte(s, '\n')
	if firstNL < 0 {
		return s
	}
	inner := s[firstNL+1:]
	// Strip the closing fence
	if idx := strings.LastIndex(inner, "```"); idx >= 0 {
		inner = inner[:idx]
	}
	return strings.TrimSpace(inner)
}

// fixPythonLiterals replaces Python-style True/False/None with JSON
// true/false/null. Some models trained on Python-heavy corpora emit these.
// Example: {"done": True, "data": None} → {"done": true, "data": null}
func fixPythonLiterals(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			b.WriteByte(c)
			escape = false
			continue
		}
		if c == '\\' && inString {
			b.WriteByte(c)
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
			b.WriteByte(c)
			continue
		}
		if !inString {
			rest := s[i:]
			if strings.HasPrefix(rest, "True") {
				b.WriteString("true")
				i += 3
				continue
			}
			if strings.HasPrefix(rest, "False") {
				b.WriteString("false")
				i += 4
				continue
			}
			if strings.HasPrefix(rest, "None") {
				b.WriteString("null")
				i += 3
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// stripComments removes // line comments and /* block comments */ from JSON.
// Some models add comments to their output. Only strips comments outside strings.
func stripComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			b.WriteByte(c)
			escape = false
			continue
		}
		if c == '\\' && inString {
			b.WriteByte(c)
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
			b.WriteByte(c)
			continue
		}
		if !inString && c == '/' && i+1 < len(s) {
			if s[i+1] == '/' {
				// Line comment — skip to end of line
				for i < len(s) && s[i] != '\n' {
					i++
				}
				if i < len(s) {
					b.WriteByte('\n')
				}
				continue
			}
			if s[i+1] == '*' {
				// Block comment — skip to */
				i += 2
				for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
					i++
				}
				if i+1 < len(s) {
					i++ // skip past the /
				}
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}
