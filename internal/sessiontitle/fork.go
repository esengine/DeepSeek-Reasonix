// Package sessiontitle contains title transforms shared by conversation hosts.
package sessiontitle

import (
	"math/big"
	"regexp"
	"strings"
)

var (
	asciiForkSuffix     = regexp.MustCompile(`^(.*) \(([0-9]+)\)$`)
	fullwidthForkSuffix = regexp.MustCompile(`^(.*)（([0-9]+)）$`)
)

// IncreaseFork returns the DeepSeek Harness-style title for an independent
// child conversation. Existing ASCII and fullwidth numeric suffixes are
// incremented without changing the source title's punctuation style.
func IncreaseFork(title string) string {
	base := strings.TrimSpace(title)
	if base == "" {
		return ""
	}
	if next, ok := increaseSuffix(base, asciiForkSuffix, " (", ")"); ok {
		return next
	}
	if next, ok := increaseSuffix(base, fullwidthForkSuffix, "（", "）"); ok {
		return next
	}
	return base + " (1)"
}

func increaseSuffix(title string, pattern *regexp.Regexp, open, close string) (string, bool) {
	match := pattern.FindStringSubmatch(title)
	if len(match) != 3 {
		return "", false
	}
	n, ok := new(big.Int).SetString(match[2], 10)
	if !ok {
		return "", false
	}
	n.Add(n, big.NewInt(1))
	return match[1] + open + n.String() + close, true
}
