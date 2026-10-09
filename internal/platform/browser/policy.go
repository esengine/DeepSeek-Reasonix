package browser

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/base/fileutil"
)

// checkURL decides whether a page may be loaded. http and https go anywhere
// the network does; about:blank is the empty page; a file must lie inside one
// of roots once symlinks resolve, and a path on disk is that file's URL. Every
// other scheme — the browser's own pages, script and data URLs — is refused.
func checkURL(raw string, roots []string) (string, error) {
	raw, kind := asAddress(raw)
	switch kind {
	case NetworkPath:
		return "", networkRefusal(raw, "a page may only be a local file inside the workspace")
	case InvalidPath:
		return "", fail(CodeURLRefused, "%q is not a valid file address", raw)
	}
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
		if !ok {
			return "", fail(CodeURLRefused, "%q is outside the workspace; a page may only be a local file inside it", raw)
		}
		switch judgeFile(path, roots) {
		case fileNetwork:
			return "", networkRefusal(raw, "a page may only be a local file inside the workspace")
		case fileOutside:
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

// Seams for the host's path rules and filesystem calls; tests replace them to
// hold the Windows reading of a path on any platform and to count lookups.
var (
	windowsPaths = runtime.GOOS == "windows"
	resolveLinks = filepath.EvalSymlinks
	statFile     = os.Stat
)

type fileVerdict uint8

const (
	fileInside fileVerdict = iota
	fileOutside
	fileNetwork
)

func networkRefusal(raw, rule string) *Failure {
	return fail(CodeNetworkPath, "%q is a network path, a file on another machine; %s. It was refused without being looked up", raw, rule)
}

func spelledAsNetwork(path string) bool { return windowsPaths && fileutil.IsNetworkPath(path) }

// networkVerdict is the file tools' rule: a network path is inside only below a
// root that is itself a network path, compared by spelling with no lookup.
func networkVerdict(path string, roots []string) fileVerdict {
	if fileutil.NetworkScopeOn(windowsPaths, path, roots) == nil {
		return fileInside
	}
	return fileNetwork
}

func fileWithin(path string, roots []string) bool { return judgeFile(path, roots) == fileInside }

// judgeFile places path against roots. A network path is told apart by its
// spelling and answered before any lookup: resolving a share is itself a
// connection to the machine that names it.
func judgeFile(path string, roots []string) fileVerdict {
	if path == "" {
		return fileOutside
	}
	if spelledAsNetwork(path) {
		return networkVerdict(path, roots)
	}
	if windowsPaths && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	path = filepath.FromSlash(path)
	// A bare drive (C:) is relative to that drive's working directory, which
	// may lie inside a root while the page the browser opens is the drive root.
	if !filepath.IsAbs(path) {
		return fileOutside
	}
	if spelledAsNetwork(path) {
		return networkVerdict(path, roots)
	}
	if resolved, err := resolveLinks(path); err == nil {
		path = resolved
	}
	for _, root := range roots {
		if resolved, err := resolveLinks(root); err == nil {
			root = resolved
		}
		if fileutil.AtOrUnder(path, root) {
			return fileInside
		}
	}
	return fileOutside
}

// OriginOf reduces a URL to what a site grant names. It answers "" for the
// empty page and anything that is not a page an agent may open.
func OriginOf(raw string) string {
	raw, kind := asAddress(raw)
	if kind == NetworkPath || kind == InvalidPath {
		return ""
	}
	u, err := url.Parse(raw)
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
	raw, kind := asAddress(raw)
	if kind == NetworkPath || kind == InvalidPath {
		return false
	}
	u, err := url.Parse(raw)
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

// asAddress is raw as the page it names: a path or file: URL on disk becomes
// its normalised file: URL, anything else is only trimmed.
func asAddress(raw string) (string, PathKind) {
	asURL, kind := LocalPathURL(raw)
	if kind == LocalPath {
		return asURL, kind
	}
	return strings.TrimSpace(raw), kind
}
