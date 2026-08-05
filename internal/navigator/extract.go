package navigator

import (
	"regexp"
)

// Pre-compiled regexes for implicit-fact extraction. These mirror the
// StateTracker's patterns (internal/agent/state_tracker.go) so the navigator
// kernel and the legacy StateTracker agree on what counts as a recoverable
// path or ID. Keep the two in sync.

var (
	// unixPathRe matches absolute Unix paths (/usr/local/bin, /etc/hosts).
	unixPathRe = regexp.MustCompile(`(?:^|[\s'"(:=])(/(?:[A-Za-z0-9._@-]+/)*[A-Za-z0-9._@-]+)`)
	// windowsPathRe matches Windows paths (C:\Users\..., D:\dev\...).
	windowsPathRe = regexp.MustCompile(`(?:^|[\s'"(:=])((?:[A-Za-z]:\\|[\\/][\\/])[^\s'"<>|]+)`)
	// idAssignRe matches "id = 42", "userId: 123", "ID=abc-123".
	idAssignRe = regexp.MustCompile(`(?i)\b([a-z_][a-z0-9_]*\s*(?:id|key|uuid)\s*[:=]\s*([A-Za-z0-9_-]{2,64}))`)
	// jsonIDRe matches "id": "value" inside JSON-ish result text.
	jsonIDRe = regexp.MustCompile(`"(?:id|user_?id|session_?id|request_?id|uuid)"\s*:\s*"([A-Za-z0-9_-]{2,64})"`)
)

// extractPaths returns unique file paths found in s. Unix and Windows paths are
// both matched; dedup preserves first-seen order.
func extractPaths(s string) []string {
	if s == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	collect := func(matches [][]string) {
		for _, m := range matches {
			if len(m) > 1 {
				p := m[1]
				if !seen[p] && len(p) > 2 { // skip trivial "/" matches
					seen[p] = true
					out = append(out, p)
				}
			}
		}
	}
	collect(unixPathRe.FindAllStringSubmatch(s, -1))
	collect(windowsPathRe.FindAllStringSubmatch(s, -1))
	return out
}

// extractIDs returns unique identifier values found in s.
func extractIDs(s string) []string {
	if s == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, m := range idAssignRe.FindAllStringSubmatch(s, -1) {
		if len(m) > 2 {
			v := m[2]
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	for _, m := range jsonIDRe.FindAllStringSubmatch(s, -1) {
		if len(m) > 1 {
			v := m[1]
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}
