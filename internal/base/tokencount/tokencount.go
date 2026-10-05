// Package tokencount estimates how many tokens a string costs a model, without
// carrying a tokenizer. Anything that has to size text before a provider counts
// it — a cache-shape diagnostic, a tool call's context footprint — needs one
// answer to that question rather than a private heuristic per caller.
package tokencount

import "unicode/utf8"

// Text estimates the token count of s. The split is by script because the two
// behave nothing alike — Latin text and code trend near four bytes per token,
// CJK closer to one token per character — and either rule applied to the whole
// string is wrong by ~4x on the other half. A real tokenizer would be exact and
// would mean a vocabulary per model, re-run over whole transcripts every turn.
func Text(s string) int {
	narrow, wide := 0, 0
	for _, r := range s {
		if r < utf8.RuneSelf {
			narrow++
			continue
		}
		wide++
	}
	// Round up: a long run of short strings must not sum to nothing.
	return (narrow+3)/4 + wide
}
