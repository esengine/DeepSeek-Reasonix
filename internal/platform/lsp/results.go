package lsp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// parseLocations decodes the three shapes textDocument/definition and
// /references may return: Location, Location[], or LocationLink[].
func parseLocations(raw json.RawMessage) []Location {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil
	}
	if s[0] == '[' {
		var arr []Location
		if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 && arr[0].URI != "" {
			return arr
		}
		var links []struct {
			TargetURI   string `json:"targetUri"`
			TargetRange Range  `json:"targetRange"`
		}
		if json.Unmarshal(raw, &links) == nil {
			out := make([]Location, 0, len(links))
			for _, l := range links {
				if l.TargetURI != "" {
					out = append(out, Location{URI: l.TargetURI, Range: l.TargetRange})
				}
			}
			return out
		}
		return nil
	}
	var one Location
	if json.Unmarshal(raw, &one) == nil && one.URI != "" {
		return []Location{one}
	}
	return nil
}

// parseHover decodes a Hover.contents, which may be a MarkupContent, a single
// MarkedString, or an array of them.
func parseHover(raw json.RawMessage) string {
	var h struct {
		Contents json.RawMessage `json:"contents"`
	}
	if json.Unmarshal(raw, &h) != nil || len(h.Contents) == 0 {
		return ""
	}
	return strings.TrimSpace(markedToText(h.Contents))
}

func markedToText(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	switch s[0] {
	case '"':
		var str string
		_ = json.Unmarshal(raw, &str)
		return str
	case '{':
		var mc struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		}
		if json.Unmarshal(raw, &mc) == nil && mc.Value != "" {
			return mc.Value
		}
		var ms struct {
			Language string `json:"language"`
			Value    string `json:"value"`
		}
		_ = json.Unmarshal(raw, &ms)
		return ms.Value
	case '[':
		var parts []json.RawMessage
		_ = json.Unmarshal(raw, &parts)
		var out []string
		for _, p := range parts {
			if t := markedToText(p); t != "" {
				out = append(out, t)
			}
		}
		return strings.Join(out, "\n")
	}
	return ""
}

var severityName = map[int]string{1: "error", 2: "warning", 3: "info", 4: "hint"}

func formatDiagnostics(rel string, diags []Diagnostic) string {
	if len(diags) == 0 {
		return "no diagnostics for " + rel
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d diagnostic(s) in %s:\n", len(diags), rel)
	for _, d := range diags {
		sev := severityName[d.Severity]
		if sev == "" {
			sev = "error"
		}
		src := ""
		if d.Source != "" {
			src = " [" + d.Source + "]"
		}
		fmt.Fprintf(&b, "%d:%d %s%s %s\n",
			d.Range.Start.Line+1, d.Range.Start.Character+1, sev, src,
			strings.TrimSpace(d.Message))
	}
	return strings.TrimRight(b.String(), "\n")
}
