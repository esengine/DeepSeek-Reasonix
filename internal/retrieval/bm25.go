package retrieval

import (
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Tokens lowercases Latin words and splits CJK text into single-rune terms. It
// is intentionally simple: a local, dependency-free approximation of FTS token
// matching for saved agent history and memory.
func Tokens(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, b.String())
		b.Reset()
	}
	for _, r := range s {
		switch {
		case isCJK(r):
			flush()
			out = append(out, string(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			b.WriteRune(unicode.ToLower(r))
		default:
			flush()
		}
	}
	flush()
	return out
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// Unique returns terms in first-seen order.
func Unique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// Counts returns a term-frequency map.
func Counts(terms []string) map[string]int {
	counts := map[string]int{}
	for _, term := range terms {
		counts[term]++
	}
	return counts
}

// BM25Score scores a document against query terms.
func BM25Score(counts map[string]int, length int, queryTerms []string, df map[string]int, totalDocs int, avgLen float64) float64 {
	const (
		k1 = 1.2
		b  = 0.75
	)
	if length <= 0 || totalDocs <= 0 {
		return 0
	}
	if avgLen <= 0 {
		avgLen = 1
	}
	var score float64
	docLen := float64(length)
	for _, term := range queryTerms {
		tf := counts[term]
		if tf == 0 {
			continue
		}
		termDF := df[term]
		if termDF == 0 {
			continue
		}
		idf := math.Log(1 + (float64(totalDocs)-float64(termDF)+0.5)/(float64(termDF)+0.5))
		freq := float64(tf)
		score += idf * (freq * (k1 + 1)) / (freq + k1*(1-b+b*docLen/avgLen))
	}
	return score
}

// DocumentFrequency counts how many documents contain each term.
func DocumentFrequency(docs []map[string]int) map[string]int {
	df := map[string]int{}
	for _, counts := range docs {
		for term := range counts {
			df[term]++
		}
	}
	return df
}

// KeepTopRelativeScore keeps the best item and drops trailing items whose score
// falls below ratio * topScore. Callers must pass items already sorted best
// first. This mirrors SQLite FTS/BM25 search UIs that over-fetch, then trim
// common-word-only noise without imposing an absolute score threshold.
func KeepTopRelativeScore[T any](items []T, ratio float64, score func(T) float64) []T {
	if len(items) == 0 || ratio <= 0 {
		return items
	}
	top := score(items[0])
	if top <= 0 {
		return items
	}
	cutoff := top * ratio
	out := items[:0]
	for i, item := range items {
		if i == 0 || score(item) >= cutoff {
			out = append(out, item)
		}
	}
	return out
}

// QueryTerms normalizes a search string and reports an error when nothing
// searchable remains.
func QueryTerms(query string) ([]string, error) {
	terms := Unique(Tokens(strings.TrimSpace(query)))
	if len(terms) == 0 {
		return nil, fmt.Errorf("query must contain at least one letter or number")
	}
	return terms, nil
}

// MakeSnippet returns a whitespace-compacted excerpt centered near the query.
func MakeSnippet(text, query string, terms []string, maxRunes int) string {
	text = CompactWhitespace(text)
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	lower := strings.ToLower(text)
	query = strings.ToLower(strings.TrimSpace(query))
	idx := -1
	if query != "" {
		idx = strings.Index(lower, query)
	}
	if idx < 0 {
		for _, term := range terms {
			runes := []rune(term)
			if len(runes) == 1 && !isCJK(runes[0]) {
				continue
			}
			if i := strings.Index(lower, term); i >= 0 {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		idx = 0
	}
	return snippetAround(text, idx, maxRunes)
}

func snippetAround(text string, byteIdx, maxRunes int) string {
	if byteIdx < 0 {
		byteIdx = 0
	}
	if byteIdx > len(text) {
		byteIdx = len(text)
	}
	for byteIdx > 0 && byteIdx < len(text) && !utf8.RuneStart(text[byteIdx]) {
		byteIdx--
	}
	runes := []rune(text)
	pos := utf8.RuneCountInString(text[:byteIdx])
	start := pos - maxRunes/2
	if start < 0 {
		start = 0
	}
	end := start + maxRunes
	if end > len(runes) {
		end = len(runes)
		start = end - maxRunes
		if start < 0 {
			start = 0
		}
	}
	prefix := ""
	suffix := ""
	if start > 0 {
		prefix = "..."
	}
	if end < len(runes) {
		suffix = "..."
	}
	return prefix + string(runes[start:end]) + suffix
}

// CompactWhitespace collapses runs of whitespace into one ASCII space.
func CompactWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
