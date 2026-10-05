package builtin

import "time"

// toolTimeout resolves a caller-supplied timeout_seconds against a default and
// a ceiling. Zero seconds means unspecified and yields def; a zero ceiling means
// unbounded. bash passes its host cap as both, so a per-call value can only
// tighten that cap, never raise it.
func toolTimeout(sec int, def, ceiling time.Duration) time.Duration {
	to := time.Duration(sec) * time.Second
	if sec <= 0 {
		to = def
	}
	if ceiling > 0 && to > ceiling {
		return ceiling
	}
	return to
}
