package evidence

import "strings"

// CommandMatches reports whether a cited verification command is proven by a
// command that actually ran. Models paraphrase commands when citing them
// (dropping a `cd` prefix, changing quote style, omitting flags), so byte
// equality rejects real verifications; instead both sides are split into
// shell segments and each cited segment must be covered by some ran segment.
func CommandMatches(cited, ran string) bool {
	citedSegs := commandSegments(cited)
	if len(citedSegs) == 0 {
		return false
	}
	ranSegs := commandSegments(ran)
	for _, c := range citedSegs {
		if !segmentCovered(c, ranSegs) {
			return false
		}
	}
	return true
}

func segmentCovered(cited string, ranSegs []string) bool {
	for _, r := range ranSegs {
		if segmentMatches(cited, r) {
			return true
		}
	}
	return false
}

// segmentMatches accepts normalized equality, or a token subset with the same
// head token (e.g. cited "ls x 2>&1" against ran "ls -la x 2>&1"). One-token
// citations only match exactly, so a bare "ls" can't claim an unrelated run.
func segmentMatches(cited, ran string) bool {
	ct, rt := segmentTokens(cited), segmentTokens(ran)
	if len(ct) == 0 || len(rt) == 0 {
		return false
	}
	if strings.Join(ct, " ") == strings.Join(rt, " ") {
		return true
	}
	if len(ct) < 2 || ct[0] != rt[0] {
		return false
	}
	have := make(map[string]bool, len(rt))
	for _, t := range rt {
		have[t] = true
	}
	for _, t := range ct {
		if !have[t] {
			return false
		}
	}
	return true
}

var segmentSeparators = []string{"&&", "||", ";", "|", "\n"}

func commandSegments(s string) []string {
	parts := []string{s}
	for _, sep := range segmentSeparators {
		var next []string
		for _, p := range parts {
			next = append(next, strings.Split(p, sep)...)
		}
		parts = next
	}
	var segs []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		segs = append(segs, p)
	}
	return segs
}

func segmentTokens(s string) []string {
	fields := strings.Fields(s)
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, "")
		f = strings.ReplaceAll(f, "'", "")
		if f != "" {
			tokens = append(tokens, f)
		}
	}
	return tokens
}
