package installsource

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"reasonix/internal/config"
)

// packageNameRe matches valid npm package-name segments. Pinned by [a-z0-9._-]
// — exactly what npm allows. The leading character may be a digit (scoped
// packages like @5/test are rare but valid).
var packageNameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func isURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func looksLikeMarkdownURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Ext(u.Path), ".md")
}

func looksLikeMCPJSONURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Base(u.Path), ".mcp.json")
}

// looksLikeRemoteMCPEndpoint is a loose heuristic: any URL with "mcp" or
// "sse" in its path, or a host that clearly advertises mcp, is treated as a
// remote MCP endpoint when no manifest could be downloaded. Used as a fallback
// for the auto-resolver; users can always set kind="mcp" to bypass the guess.
func looksLikeRemoteMCPEndpoint(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if strings.HasPrefix(host, "mcp.") || strings.HasPrefix(host, "mcp-") || strings.Contains(host, ".mcp.") || strings.Contains(host, "-mcp.") {
		return true
	}
	p := strings.ToLower(u.Path)
	return strings.Contains(p, "mcp") || strings.Contains(p, "sse")
}

// rawGitHubBlobURL rewrites github.com/<owner>/<repo>/blob/<ref>/<path> (and
// the /raw/ variant) into the raw.githubusercontent.com form. Other URLs are
// returned untouched so we don't corrupt non-github sources.
func rawGitHubBlobURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return s
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 5 && parts[2] == "blob" {
		return "https://raw.githubusercontent.com/" + parts[0] + "/" + parts[1] + "/" + parts[3] + "/" + strings.Join(parts[4:], "/")
	}
	if len(parts) >= 5 && parts[2] == "raw" {
		return "https://raw.githubusercontent.com/" + parts[0] + "/" + parts[1] + "/" + parts[3] + "/" + strings.Join(parts[4:], "/")
	}
	return s
}

func looksLikePackage(s string) bool {
	if strings.ContainsAny(s, " \t\n\\") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") {
		return false
	}
	if strings.HasPrefix(s, "@") {
		parts := strings.Split(s, "/")
		return len(parts) == 2 && packageNameRe.MatchString(parts[0][1:]) && packageNameRe.MatchString(parts[1])
	}
	return packageNameRe.MatchString(s)
}

// isExecutable reports whether path is a regular executable file. POSIX uses
// execute bits; Windows uses executable file extensions because chmod bits do
// not reliably model launchability there.
func isExecutable(path string, info os.FileInfo) bool {
	if !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".exe", ".cmd", ".bat", ".ps1":
			return true
		}
	}
	return info.Mode().Perm()&0o111 != 0
}

// nameFromURL produces a stable human-readable skill name from a URL's
// filename stem. Falls back to "skill" when the URL has no name component.
func nameFromURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return "skill"
	}
	base := filepath.Base(u.Path)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return sanitizeName(base)
}

// mcpNameFromURL derives a default MCP server name from a remote URL. It
// strips common subdomains and TLDs so e.g. mcp.stripe.com -> "stripe" and
// api.example.co -> "example". localhost:port and explicit names work too.
func mcpNameFromURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return "mcp"
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		// Distinguish local servers by port; "localhost" alone is unhelpful.
		if p := u.Port(); p != "" {
			return sanitizeName("local-" + p)
		}
		return "local"
	}
	// Strip "mcp.", "api.", "www." subdomains and any "mcp-" / "mcp_" prefix.
	for _, p := range []string{"mcp.", "api.", "www."} {
		host = strings.TrimPrefix(host, p)
	}
	host = strings.TrimPrefix(host, "mcp-")
	host = strings.TrimPrefix(host, "mcp_")
	// Drop the TLD and any common second-level TLDs ("co.uk", "com.au").
	parts := strings.Split(host, ".")
	switch len(parts) {
	case 0, 1:
		return sanitizeName(host)
	case 2:
		return sanitizeName(parts[0])
	default:
		// Two-letter final segment is likely a ccTLD paired with a SLD
		// (".co.uk", ".com.au", ".co.jp"). Use the third-to-last.
		if len(parts[len(parts)-1]) == 2 {
			return sanitizeName(parts[len(parts)-3])
		}
		return sanitizeName(parts[len(parts)-2])
	}
}

// sanitizeName produces a valid skill/MCP identifier. Letters, digits, _ . -
// are kept; everything else becomes a single dash. Leading non-alphanumerics
// get an "mcp-" prefix so the result is always a valid config key.
func sanitizeName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "@")
	s = strings.ReplaceAll(s, "/", "-")
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-'
		if !ok {
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
			continue
		}
		b.WriteRune(r)
		prevDash = r == '-'
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "mcp"
	}
	if !((out[0] >= 'a' && out[0] <= 'z') || (out[0] >= '0' && out[0] <= '9')) {
		out = "mcp-" + out
	}
	if len(out) > 64 {
		out = out[:64]
		out = strings.TrimRight(out, "-._")
	}
	return out
}

// cleanMap drops empty keys. A nil/empty input returns nil so the JSON shape
// stays compact and downstream config compares equal.
func cleanMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range in {
		k = strings.TrimSpace(k)
		if k != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func normalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "skill", "mcp":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return "auto"
	}
}

func normalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "copy", "link", "register":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "auto"
	}
}

func modeForSingleSkill(mode string) string {
	if mode == "link" {
		return mode
	}
	return "copy"
}

func normalizeTransport(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "http", "streamable-http":
		return "http"
	case "sse":
		return "sse"
	case "stdio":
		return "stdio"
	default:
		return "auto"
	}
}

// normalizeTier maps a tier value into a known set. The boolean returned
// reports whether the original value was already in the set; callers use it
// to surface a warning when a typo'd tier quietly becomes "lazy".
func normalizeTier(tier string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "eager":
		return "eager", true
	case "background":
		return "background", true
	case "", "lazy":
		return "lazy", true
	default:
		return "lazy", false
	}
}

// pluginTransport reports the effective transport for a plugin entry,
// normalizing empty Type to stdio (the default the config layer expects).
func pluginTransport(e config.PluginEntry) string {
	switch normalizeTransport(e.Type) {
	case "http":
		return "http"
	case "sse":
		return "sse"
	default:
		return "stdio"
	}
}
