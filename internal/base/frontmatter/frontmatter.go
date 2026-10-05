// Package frontmatter parses the ---fenced YAML frontmatter blocks that prefix
// skill, command, and memory files.
package frontmatter

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

type DecodeOptions struct {
	KnownFields bool
}

// SplitLegacy is the historic flat view: Parse followed by LegacyFlat, so nested
// mappings collapse onto their leaf keys ("metadata:\n  type: user" becomes
// fm["type"]) and sequences join comma-separated. Existing vocabularies read it.
// A new one must read Parse instead — the collapse drops which key a field was
// written under, and that is what a namespace is made of.
func SplitLegacy(s string) (map[string]string, string) {
	doc, body := Parse(s)
	return doc.LegacyFlat(), body
}

// Decode separates frontmatter and decodes the YAML block into out. It is for
// callers that need typed schema validation; SplitLegacy remains the permissive
// compatibility parser for legacy metadata consumers.
func Decode(s string, out any, opts DecodeOptions) (string, error) {
	raw, body, ok := splitRaw(s)
	if !ok || strings.TrimSpace(raw) == "" {
		return body, nil
	}
	dec := yaml.NewDecoder(bytes.NewBufferString(raw))
	dec.KnownFields(opts.KnownFields)
	if err := dec.Decode(out); err != nil {
		return "", err
	}
	return body, nil
}

func splitRaw(s string) (raw, body string, ok bool) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", s, false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n"), true
		}
	}
	return "", s, false // opened but never closed: treat all as body
}

func mappingRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	return doc
}

func normalizeKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}
