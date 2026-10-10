package browser

import "strings"

// PathKind says what a typed address is when it names a place on disk.
type PathKind string

const (
	// NotPath is anything else: a host, a URL, text a path rule does not claim.
	NotPath PathKind = ""
	// LocalPath is a file on this machine.
	LocalPath PathKind = "local"
	// NetworkPath is a UNC share, a file on another machine.
	NetworkPath PathKind = "network"
	// InvalidPath is a file: address with control characters in it.
	InvalidPath PathKind = "invalid"
)

// LocalPathURL reads an address as a place on disk and answers its file: URL.
// Only a form that cannot be a host claims it: a drive letter and separator, \\
// (also \\?\ and \\.\), one leading slash, or file:. A share, which a file: URL
// can name by host or by a path that resolves to //host, answers "" and NetworkPath.
// localpath.js is held to the same table.
func LocalPathURL(raw string) (string, PathKind) {
	text := strings.Trim(strings.ToValidUTF8(raw, "\uFFFD"), typedSpace)
	if flat := stripLineBreaks(text); len(flat) >= 6 && strings.EqualFold(flat[:6], "file:/") {
		return fileForm(flat[5:])
	}
	if rest, ok := strings.CutPrefix(text, `\\?\`); ok {
		return deviceForm(rest)
	}
	if rest, ok := strings.CutPrefix(text, `\\.\`); ok {
		return deviceForm(rest)
	}
	if letter, rest, ok := splitDrive(text); ok {
		return "file:///" + letter + ":/" + encodeSegments(rest, isBackOrSlash), LocalPath
	}
	if rest, ok := strings.CutPrefix(text, `\\`); ok {
		return uncForm(rest)
	}
	if strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "//") {
		return "file:///" + encodeSegments(text[1:], func(r byte) bool { return r == '/' }), LocalPath
	}
	return "", NotPath
}

// typedSpace is what a typed address is trimmed of: the white space of
// JavaScript's trim and of unicode.IsSpace together, with the byte order mark.
const typedSpace = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

func uncForm(rest string) (string, PathKind) {
	host, share := rest, ""
	if i := strings.IndexAny(rest, `\/`); i >= 0 {
		host, share = rest[:i], rest[i+1:]
	}
	if host == "" {
		return "", NotPath
	}
	return "file://" + encodeSegment(host) + "/" + encodeSegments(share, isBackOrSlash), NetworkPath
}

func deviceForm(rest string) (string, PathKind) {
	if len(rest) >= 4 && strings.EqualFold(rest[:3], "UNC") && isBackOrSlash(rest[3]) {
		return uncForm(rest[4:])
	}
	if letter, tail, ok := splitDrive(rest); ok {
		return "file:///" + letter + ":/" + encodeSegments(tail, isBackOrSlash), LocalPath
	}
	return "", NetworkPath
}

// stripLineBreaks drops tab, LF and CR wherever they sit, as a URL parser does
// before it reads anything.
func stripLineBreaks(s string) string {
	return strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(s)
}

// fileForm reads what follows "file:" in a typed file URL. Backslashes are
// separators, as a browser reads them, and the question asked of the path is
// the one Windows asks: does it resolve to //host/..., a share.
func fileForm(rest string) (string, PathKind) {
	if strings.IndexFunc(rest, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", InvalidPath
	}
	rest = strings.ReplaceAll(rest, `\`, "/")
	host := ""
	if strings.HasPrefix(rest, "//") {
		cut := strings.IndexByte(rest[2:], '/')
		if cut < 0 {
			host, rest = rest[2:], "/"
		} else {
			host, rest = rest[2:2+cut], rest[2+cut:]
		}
	}
	path, tail := rest, ""
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		path, tail = rest[:i], rest[i:]
	}
	if len(host) == 2 && host[1] == ':' && isLetter(host[0]) {
		path = "/" + host + path
	} else if host != "" && !strings.EqualFold(host, "localhost") {
		return "", NetworkPath
	}
	if path == "" {
		path = "/"
	}
	if opensShare(path) {
		return "", NetworkPath
	}
	return "file://" + path + tail, LocalPath
}

var pathUnescaper = strings.NewReplacer("%5c", "/", "%5C", "/", "%2f", "/", "%2F", "/", "%2e", ".", "%2E", ".")

func opensShare(path string) bool {
	var out []string
	for _, seg := range strings.Split(pathUnescaper.Replace(path), "/")[1:] {
		switch seg {
		case ".":
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, seg)
		}
	}
	return len(out) > 1 && out[0] == ""
}

func splitDrive(text string) (letter, rest string, ok bool) {
	text = strings.TrimPrefix(text, "/")
	if len(text) < 3 || text[1] != ':' || !isBackOrSlash(text[2]) || !isLetter(text[0]) {
		return "", "", false
	}
	return text[:1], text[3:], true
}

func isLetter(b byte) bool { return 'a' <= b|0x20 && b|0x20 <= 'z' }

func isBackOrSlash(b byte) bool { return b == '\\' || b == '/' }

// encodeSegments joins the non-empty segments of s with "/" and keeps one
// trailing "/" when s ended on a separator, so a doubled separator is one.
func encodeSegments(s string, sep func(byte) bool) string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i < len(s) && !sep(s[i]) {
			continue
		}
		if i > start {
			out = append(out, encodeSegment(s[start:i]))
		}
		start = i + 1
	}
	joined := strings.Join(out, "/")
	if len(out) > 0 && len(s) > 0 && sep(s[len(s)-1]) {
		joined += "/"
	}
	return joined
}

const segmentKeep = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"

func encodeSegment(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		if strings.IndexByte(segmentKeep, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}
