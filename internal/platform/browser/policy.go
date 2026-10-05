package browser

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/base/fileutil"
)

// checkURL decides whether a page may be loaded. http and https go anywhere
// the network does; about:blank is the empty page; a file must lie inside one
// of roots once symlinks resolve. Every other scheme — the browser's own
// pages, script and data URLs — is refused.
func checkURL(raw string, roots []string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "", fail(CodeURLRefused, "%q is not an absolute URL; include the scheme, e.g. https://", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return "", fail(CodeURLRefused, "%q names no host", raw)
		}
		return u.String(), nil
	case "about":
		if u.Opaque == "blank" {
			return "about:blank", nil
		}
	case "file":
		path, ok := localFilePath(u)
		if !ok || !fileWithin(path, roots) {
			return "", fail(CodeURLRefused, "%q is outside the workspace; a page may only be a local file inside it", raw)
		}
		return u.String(), nil
	}
	return "", fail(CodeURLRefused, "the %s: scheme is not a page the agent may open", u.Scheme)
}

// localFilePath answers the path a file URL names on this machine. A host other
// than localhost is another machine — Windows opens file://host/share as a UNC
// path — so it names no local file; file://C:/x is a drive, as browsers read it.
func localFilePath(u *url.URL) (string, bool) {
	path, err := url.PathUnescape(u.Path)
	if err != nil {
		return "", false
	}
	switch host := u.Host; {
	case host == "" || strings.EqualFold(host, "localhost"):
		return path, true
	case runtime.GOOS == "windows" && isDriveLetter(host):
		return host + path, true
	}
	return "", false
}

func isDriveLetter(s string) bool {
	return len(s) == 2 && s[1] == ':' && ('a' <= s[0]|0x20 && s[0]|0x20 <= 'z')
}

func fileWithin(path string, roots []string) bool {
	if path == "" {
		return false
	}
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	path = filepath.FromSlash(path)
	// A bare drive (C:) is relative to that drive's working directory, which
	// may lie inside a root while the page the browser opens is the drive root.
	if !filepath.IsAbs(path) {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	for _, root := range roots {
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		if fileutil.AtOrUnder(path, root) {
			return true
		}
	}
	return false
}

// OriginOf reduces a URL to what a site grant names. It answers "" for the
// empty page and anything that is not a page an agent may open.
func OriginOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return ""
		}
		return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
	case "file":
		return "file://"
	}
	return ""
}

// ServesWorkspace reports whether a page at raw is this workspace's own: a file
// inside it, or something this machine serves on loopback, which is where a dev
// server for the code being edited runs. A page anywhere else exercises code
// nobody here wrote.
func (s *Session) ServesWorkspace(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "file":
		path, ok := localFilePath(u)
		return ok && fileWithin(path, s.cfg.Roots)
	case "http", "https":
		host := u.Hostname()
		return host == "127.0.0.1" || host == "::1" || strings.EqualFold(host, "localhost")
	}
	return false
}
