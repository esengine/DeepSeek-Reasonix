package bootstrap

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnsupportedRemote is a machine nothing can be installed onto: an OS or an
// architecture with no release. A caller has to tell it apart
// because there is nothing to retry — the answer is a different machine.
var ErrUnsupportedRemote = errors.New("bootstrap: unsupported remote")

// ParseUname maps `uname -sm` output to Go GOOS/GOARCH for a Linux or macOS
// remote. Windows is identified separately, so a POSIX layer running on it
// (MSYS_NT, MINGW64_NT) is refused here rather than mistaken for either.
func ParseUname(out string) (goos, goarch string, err error) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 2 {
		// A machine with no uname answers with its own shell's complaint, which
		// is exactly as unsupported as one that answered with the wrong OS.
		return "", "", fmt.Errorf("%w: `uname -sm` answered %q", ErrUnsupportedRemote, out)
	}
	sys, machine := fields[0], fields[1]
	switch strings.ToLower(sys) {
	case "linux":
		goos = "linux"
	case "darwin":
		goos = "darwin"
	default:
		return "", "", fmt.Errorf("%w: OS %q", ErrUnsupportedRemote, sys)
	}
	switch strings.ToLower(machine) {
	case "x86_64", "amd64":
		goarch = "amd64"
	case "aarch64", "arm64":
		goarch = "arm64"
	case "armv7l", "armv6l", "arm":
		goarch = "arm"
	default:
		return "", "", fmt.Errorf("%w: architecture %q", ErrUnsupportedRemote, machine)
	}
	return goos, goarch, nil
}

// KernelTooOldError is a machine that has a reasonix, of a line too old to be
// what was asked of it. A caller has to tell it apart from a machine that has
// none: one is answered by upgrading over there, the other by installing, and a
// sentence saying "install failed" sends the reader to the wrong one of the two.
type KernelTooOldError struct {
	Found string // what that machine's reasonix reports as its version
	Need  string // the floor it did not clear
	Err   error  // what the install that could have replaced it ran into
}

func (e *KernelTooOldError) Error() string {
	msg := fmt.Sprintf("bootstrap: remote reasonix %s is older than %s", e.Found, e.Need)
	if e.Err != nil {
		return msg + ": " + e.Err.Error()
	}
	return msg
}

func (e *KernelTooOldError) Unwrap() error { return e.Err }

// meetsMinVersion reports whether found clears the floor. A version nothing
// could read clears it: a source build calls itself "dev", and refusing those
// would take the bootstrap away from everyone developing against it. What such
// a build cannot do is still caught where it is asked to do it.
func meetsMinVersion(found, min string) bool {
	if min == "" || found == "" {
		return true
	}
	return CompareVersions(found, min) >= 0
}

// ParseVersion extracts a semver-ish string from `reasonix --version` output
// like "reasonix v1.9.0" or "1.9.0".
func ParseVersion(out string) (string, error) {
	for field := range strings.FieldsSeq(strings.TrimSpace(out)) {
		v := strings.TrimPrefix(field, "v")
		if looksLikeSemver(v) {
			return v, nil
		}
	}
	return "", fmt.Errorf("bootstrap: no version found in %q", out)
}

func looksLikeSemver(v string) bool {
	parts := strings.SplitN(v, "-", 2)[0]
	seg := strings.Split(parts, ".")
	if len(seg) < 2 {
		return false
	}
	for _, s := range seg {
		if s == "" {
			return false
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// CompareVersions returns -1, 0, or 1 comparing dotted numeric versions.
// Pre-release suffixes (after '-') are ignored for ordering. Non-numeric or
// missing segments compare as 0.
func CompareVersions(a, b string) int {
	as := versionSegments(a)
	bs := versionSegments(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func versionSegments(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v = strings.SplitN(v, "-", 2)[0]
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				n = 0
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}
