package bootstrap

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseUname maps `uname -sm` output to Go GOOS/GOARCH. V1 supports Linux and
// macOS remotes; anything else (including Windows shells) is an error.
func ParseUname(out string) (goos, goarch string, err error) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 2 {
		return "", "", fmt.Errorf("bootstrap: cannot parse `uname -sm` output %q", out)
	}
	sys, machine := fields[0], fields[1]
	switch strings.ToLower(sys) {
	case "linux":
		goos = "linux"
	case "darwin":
		goos = "darwin"
	default:
		return "", "", fmt.Errorf("bootstrap: unsupported remote OS %q (V1 supports Linux and macOS)", sys)
	}
	switch strings.ToLower(machine) {
	case "x86_64", "amd64":
		goarch = "amd64"
	case "aarch64", "arm64":
		goarch = "arm64"
	case "armv7l", "armv6l", "arm":
		goarch = "arm"
	default:
		return "", "", fmt.Errorf("bootstrap: unsupported remote architecture %q", machine)
	}
	return goos, goarch, nil
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
// Numeric segments compare numerically; when they are equal, a version
// without a pre-release suffix outranks one with it, and pre-release
// identifiers compare per semver (numeric identifiers compare numerically
// and rank below alphanumeric ones, which compare lexically). Missing
// segments compare as 0.
func CompareVersions(a, b string) int {
	if c := compareVersionSegments(a, b); c != 0 {
		return c
	}
	ap, bp := prereleaseOf(a), prereleaseOf(b)
	switch {
	case ap == "" && bp == "":
		return 0
	case ap == "":
		return 1
	case bp == "":
		return -1
	}
	return comparePrerelease(ap, bp)
}

func compareVersionSegments(a, b string) int {
	as, bs := versionSegments(a), versionSegments(b)
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

func prereleaseOf(v string) string {
	if _, suffix, ok := strings.Cut(v, "-"); ok {
		return suffix
	}
	return ""
}

func comparePrerelease(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		if i >= len(as) {
			return -1
		}
		if i >= len(bs) {
			return 1
		}
		if c := comparePrereleaseIdent(as[i], bs[i]); c != 0 {
			return c
		}
	}
	return 0
}

func comparePrereleaseIdent(a, b string) int {
	an, bn := numericIdent(a), numericIdent(b)
	switch {
	case an && bn:
		ai, _ := strconv.Atoi(a)
		bi, _ := strconv.Atoi(b)
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		}
		return 0
	case an:
		return -1
	case bn:
		return 1
	}
	return strings.Compare(a, b)
}

func numericIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
