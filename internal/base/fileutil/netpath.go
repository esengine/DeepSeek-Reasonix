package fileutil

// IsNetworkPath reports whether path is spelled as a Windows network or device
// path: two leading separators of either kind, or the NT `\??\` prefix, unless a
// drive follows. It reads the spelling alone, so it can run before any lookup.
// Callers apply it where Windows rules hold: on POSIX `//x` is a local path.
func IsNetworkPath(path string) bool {
	if rest, ok := ntNamespace(path); ok {
		return !startsWithDrive(rest)
	}
	if len(path) < 2 || !isSeparator(path[0]) || !isSeparator(path[1]) {
		return false
	}
	rest := path[2:]
	if len(rest) >= 2 && (rest[0] == '?' || rest[0] == '.') && isSeparator(rest[1]) {
		return !startsWithDrive(rest[2:])
	}
	return true
}

func ntNamespace(path string) (string, bool) {
	if len(path) >= 4 && isSeparator(path[0]) && path[1] == '?' && path[2] == '?' && isSeparator(path[3]) {
		return path[4:], true
	}
	return "", false
}

func startsWithDrive(s string) bool {
	if len(s) < 2 || s[1] != ':' || !('a' <= s[0]|0x20 && s[0]|0x20 <= 'z') {
		return false
	}
	return len(s) == 2 || isSeparator(s[2])
}

func isSeparator(b byte) bool { return b == '\\' || b == '/' }
