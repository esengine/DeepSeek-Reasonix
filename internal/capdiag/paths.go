package capdiag

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// displayPath rewrites absolute paths for safe reports:
//   - under workspace → <workspace>/...
//   - under home → ~/...
//   - elsewhere → <external>/basename (no full external path, no username)
func displayPath(p, workspace, home string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	// Builtin markers stay as-is.
	if strings.HasPrefix(p, "(builtin") {
		return p
	}
	clean := filepath.Clean(p)
	if abs, err := filepath.Abs(clean); err == nil {
		clean = abs
	}
	ws := workspace
	if ws != "" {
		if abs, err := filepath.Abs(ws); err == nil {
			ws = abs
		}
		if clean == ws {
			return "<workspace>"
		}
		if rel, err := filepath.Rel(ws, clean); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "<workspace>/" + filepath.ToSlash(rel)
		}
	}
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	if home != "" {
		home = filepath.Clean(home)
		if clean == home {
			return "~"
		}
		if rel, err := filepath.Rel(home, clean); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	// External: only show a generic marker + base name so reports stay shareable.
	base := filepath.Base(clean)
	if base == "" || base == "." || base == string(os.PathSeparator) {
		return "<external>"
	}
	return "<external>/" + base
}

func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		if strings.TrimSpace(k) != "" {
			keys = append(keys, k)
		}
	}
	// Insertion sort for stability without importing sort in hot path of small maps.
	for i := 1; i < len(keys); i++ {
		j := i
		for j > 0 && keys[j-1] > keys[j] {
			keys[j-1], keys[j] = keys[j], keys[j-1]
			j--
		}
	}
	return keys
}

// urlHostOnly returns the host (and port) from a URL without path/query/userinfo.
func urlHostOnly(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<url>"
	}
	return u.Host
}

func transportOf(typ string) string {
	t := strings.ToLower(strings.TrimSpace(typ))
	switch t {
	case "", "stdio":
		return "stdio"
	case "http", "streamable-http", "streamable_http":
		return "http"
	case "sse":
		return "sse"
	default:
		return t
	}
}

func isValidTransport(t string) bool {
	switch transportOf(t) {
	case "stdio", "http", "sse":
		return true
	default:
		return false
	}
}
