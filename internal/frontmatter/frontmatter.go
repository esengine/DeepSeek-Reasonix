// Package frontmatter provides a minimal, dependency-free parser for the
// ---fenced "key: value" blocks that prefix skill, command, and memory files.
// It mirrors the YAML-like frontmatter convention without pulling in a YAML
// library, keeping Reasonix's single-(TOML)-dependency promise.
package frontmatter

import "strings"

// Split separates an optional leading ---fenced block of "key: value" lines from
// the body. It returns the parsed keys (lowercased) and the remaining body. With
// no opening/closing fence the whole input is the body. An opened but never
// closed fence treats the entire input as body (no partial parse).
//
// Values are trimmed of surrounding whitespace and outer quotes (" or ').
// A section header like "metadata:" (value part empty after the colon) is
// skipped; indented lines under it (e.g. "  type: user") are parsed directly,
// so the one nested key we emit (metadata.type) flattens to fm["type"].
// The last write wins for duplicate keys.
func Split(s string) (map[string]string, string) {
	fm := map[string]string{}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fm, s
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "---" {
			continue
		}
		for _, l := range lines[1:i] {
			k, v, ok := strings.Cut(l, ":")
			if !ok {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(k))
			val := strings.Trim(strings.TrimSpace(v), `"'`)
			if val == "" {
				continue // section header — value lives on indented lines
			}
			fm[key] = val
		}
		return fm, strings.Join(lines[i+1:], "\n")
	}
	return fm, s // opened but never closed: treat all as body
}
