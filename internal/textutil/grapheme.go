package textutil

import (
	"strings"

	"github.com/rivo/uniseg"
)

// FitGraphemeBytes returns the longest prefix that fits maxBytes without
// splitting a grapheme cluster. If a single cluster is larger than maxBytes, it
// returns that whole cluster so callers never emit malformed user-visible text.
func FitGraphemeBytes(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	end := 0
	used := 0
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		size := len(graphemes.Str())
		if used > 0 && used+size > maxBytes {
			break
		}
		end += size
		used += size
		if used >= maxBytes {
			break
		}
	}
	if end > 0 {
		return text[:end]
	}
	graphemes = uniseg.NewGraphemes(text)
	if !graphemes.Next() {
		return ""
	}
	return graphemes.Str()
}

// ClipGraphemes truncates s to at most max grapheme clusters, counting suffix
// inside the budget when suffix is used.
func ClipGraphemes(s string, max int, suffix string) string {
	if max < 1 {
		max = 1
	}
	clusters := collectGraphemes(s, max+1)
	if len(clusters) <= max && len(clusters) == countGraphemes(s) {
		return s
	}
	suffixClusters := countGraphemes(suffix)
	keep := max - suffixClusters
	if keep < 1 {
		keep = 1
		suffix = ""
	}
	if keep > len(clusters) {
		keep = len(clusters)
	}
	return strings.Join(clusters[:keep], "") + suffix
}

// TruncateGraphemes truncates s to at most max grapheme clusters, then appends
// suffix outside that budget. This preserves legacy preview behavior where the
// suffix is an extra truncation marker rather than part of the display width.
func TruncateGraphemes(s string, max int, suffix string) string {
	if max < 0 {
		max = 0
	}
	clusters := collectGraphemes(s, max+1)
	if len(clusters) <= max && len(clusters) == countGraphemes(s) {
		return s
	}
	if max > len(clusters) {
		max = len(clusters)
	}
	return strings.Join(clusters[:max], "") + suffix
}

func collectGraphemes(s string, limit int) []string {
	if limit < 1 {
		return nil
	}
	clusters := make([]string, 0, limit)
	graphemes := uniseg.NewGraphemes(s)
	for graphemes.Next() {
		clusters = append(clusters, graphemes.Str())
		if len(clusters) >= limit {
			break
		}
	}
	return clusters
}

func countGraphemes(s string) int {
	count := 0
	graphemes := uniseg.NewGraphemes(s)
	for graphemes.Next() {
		count++
	}
	return count
}
