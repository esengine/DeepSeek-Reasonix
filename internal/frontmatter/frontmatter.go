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
// A key with an empty value heads either a section ("metadata:") whose indented
// "key: value" lines flatten (metadata.type → fm["type"]), or a YAML list whose
// "- item" lines are joined comma-separated (allowed-tools → "read_file, grep"),
// so list-valued keys from skills authored for other agent tools survive.
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
		content := lines[1:i]
		for j := 0; j < len(content); j++ {
			k, v, ok := strings.Cut(content[j], ":")
			if !ok {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(k))
			val := strings.Trim(strings.TrimSpace(v), `"'`)
			if val == "" {
				// Empty value: either a section header (metadata:) whose nested
				// "key: value" lines flatten below, or a YAML list whose "- item"
				// lines we join comma-separated so list-valued keys (allowed-tools,
				// from skills authored for other agent tools) survive instead of
				// being dropped.
				var items []string
				for j+1 < len(content) {
					item, ok := strings.CutPrefix(strings.TrimSpace(content[j+1]), "-")
					if !ok {
						break // not a list item — leave it for the outer loop
					}
					items = append(items, strings.Trim(strings.TrimSpace(item), `"'`))
					j++
				}
				if len(items) > 0 {
					fm[key] = strings.Join(items, ", ")
				}
				continue
			}
			fm[key] = val
		}
		return fm, strings.Join(lines[i+1:], "\n")
	}
	return fm, s // opened but never closed: treat all as body
}
