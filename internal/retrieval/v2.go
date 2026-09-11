// Retrieval V2, shadow-only: BM25F field weighting, code-symbol splitting,
// and mixed CJK uni+bigrams. Nothing here serves the model — V2 rankings ride
// the recall audit next to production's, and MemoryBench decides if V2 ever
// takes over. Weights are candidates under measurement, not tuned truths.
package retrieval

import (
	"math"
	"strings"
	"unicode"
)

// TokensV2 extends Tokens with code-symbol structure and CJK unigrams:
// CamelCase and snake_case words also emit their segments (FooBar → foobar,
// foo, bar), and CJK runs emit unigrams alongside bigrams so one-character
// queries still match. The V1 form of every token is preserved, so V2 recall
// is a superset of V1's vocabulary.
func TokensV2(s string) []string {
	var out []string
	var word []rune
	var prev rune
	cjkRun := 0
	flushWord := func() {
		if len(word) == 0 {
			return
		}
		lower := strings.ToLower(string(word))
		out = append(out, lower)
		for _, seg := range splitCodeSymbol(word) {
			if seg != lower {
				out = append(out, seg)
			}
		}
		word = word[:0]
	}
	endCJK := func() { cjkRun = 0 }
	for _, r := range s {
		switch {
		case isCJK(r):
			flushWord()
			out = append(out, string(r))
			if cjkRun > 0 {
				out = append(out, string([]rune{prev, r}))
			}
			prev = r
			cjkRun++
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			endCJK()
			word = append(word, r)
		default:
			flushWord()
			endCJK()
		}
	}
	flushWord()
	return out
}

// splitCodeSymbol yields the lowercase segments of a code identifier:
// CamelCase humps, snake_case parts, and letter/digit boundaries.
func splitCodeSymbol(word []rune) []string {
	var segments []string
	var seg []rune
	flush := func() {
		if len(seg) > 1 { // single letters are noise, not signal
			segments = append(segments, strings.ToLower(string(seg)))
		}
		seg = seg[:0]
	}
	for i, r := range word {
		boundary := r == '_' ||
			(i > 0 && unicode.IsUpper(r) && !unicode.IsUpper(word[i-1])) ||
			(i > 0 && unicode.IsDigit(r) != unicode.IsDigit(word[i-1]))
		if boundary {
			flush()
		}
		if r != '_' {
			seg = append(seg, r)
		}
	}
	flush()
	if len(segments) < 2 {
		return nil // no structure worth indexing beyond the whole word
	}
	return segments
}

// FieldedDoc is one document split into weighted fields for BM25F.
type FieldedDoc struct {
	ID     string
	Fields map[string]string
}

// v2FieldWeights follow the BM25F intuition: identity fields say what a fact
// IS, the body says everything it mentions. Candidates under measurement.
var v2FieldWeights = map[string]float64{
	"name": 2.5, "title": 2.5, "keywords": 2.0, "subject": 2.0,
	"description": 1.5, "body": 1.0,
}

// V2Hit is one shadow-ranked document.
type V2Hit struct {
	ID    string
	Score float64
}

// RankV2 scores docs against a query with BM25F over TokensV2: term
// frequencies accumulate per field scaled by field weight, then the standard
// saturation applies once per term (the F in BM25F), with length
// normalization over the weighted document length.
func RankV2(query string, docs []FieldedDoc) []V2Hit {
	queryTerms := Unique(TokensV2(query))
	if len(queryTerms) == 0 || len(docs) == 0 {
		return nil
	}
	type indexed struct {
		id     string
		tf     map[string]float64
		length float64
	}
	corpus := make([]indexed, 0, len(docs))
	df := map[string]int{}
	var totalLen float64
	for _, doc := range docs {
		ix := indexed{id: doc.ID, tf: map[string]float64{}}
		seen := map[string]bool{}
		for field, text := range doc.Fields {
			weight, ok := v2FieldWeights[field]
			if !ok {
				weight = 1.0
			}
			for _, term := range TokensV2(text) {
				ix.tf[term] += weight
				ix.length += weight
				seen[term] = true
			}
		}
		for term := range seen {
			df[term]++
		}
		totalLen += ix.length
		corpus = append(corpus, ix)
	}
	avgLen := totalLen / float64(len(corpus))
	if avgLen <= 0 {
		avgLen = 1
	}
	const k1, b = 1.2, 0.75
	var hits []V2Hit
	for _, doc := range corpus {
		var score float64
		for _, term := range queryTerms {
			tf := doc.tf[term]
			if tf == 0 || df[term] == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(corpus))-float64(df[term])+0.5)/(float64(df[term])+0.5))
			score += idf * (tf * (k1 + 1)) / (tf + k1*(1-b+b*doc.length/avgLen))
		}
		if score > 0 {
			hits = append(hits, V2Hit{ID: doc.id, Score: score})
		}
	}
	SortHitsDesc(hits)
	return hits
}

// SortHitsDesc orders shadow hits best-first with a stable ID tiebreak.
func SortHitsDesc(hits []V2Hit) {
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && (hits[j].Score > hits[j-1].Score ||
			(hits[j].Score == hits[j-1].Score && hits[j].ID < hits[j-1].ID)); j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
}
