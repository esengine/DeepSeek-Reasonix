package agent

import (
	"errors"
	"os"
	"strings"
	"unicode/utf8"

	"reasonix/internal/provider"
)

// The client repetition guard aborts a stream whose output degenerates into
// re-emitting the same sentence instead of acting — a DeepSeek-class failure
// the provider's own repetition_truncation often misses. Aborted attempts ride
// the sampling-recovery discard path, so degenerate text never reaches the
// session and cannot self-reinforce on the retry.
const (
	defaultRepetitionTripLimit = 12
	repetitionWindowSegments   = 192
	repetitionSegmentMinRunes  = 12
	// Two tiers: exact segments trip at the limit; lowercased 24-rune stem
	// buckets trip at twice the limit, because degenerate loops paraphrase
	// ("Let me push to the fork." / "Let me push to the fork remote first.")
	// while legitimate enumerations also share stems, just never that many.
	repetitionStemRunes       = 24
	repetitionStemTripFactor  = 2
	repetitionSegmentMaxBytes = 512
	repetitionCarryMaxBytes   = 4096

	repetitionDiscardReason = "repetition_loop"
	// maxRepetitionRetries is deliberately smaller than the stream-recovery
	// budget: every degenerate attempt burns real completion tokens before the
	// guard trips, and a frozen-request replay may reproduce the loop.
	maxRepetitionRetries = 2
)

var errRepetitionLoop = errors.New("output repetition loop detected; stream stopped by client guard")

// Escape hatch while the guard gathers field experience; graduation to config
// waits on that verdict (governor precedent).
var repetitionGuardDisabledByEnv = os.Getenv("REASONIX_REPETITION_GUARD") == "0"

// resolveRepetitionTripLimit applies the Options contract: zero means the
// default guard, negative (or the env escape hatch) disables it.
func resolveRepetitionTripLimit(v int) int {
	if v == 0 {
		v = defaultRepetitionTripLimit
	}
	if v < 0 || repetitionGuardDisabledByEnv {
		return 0
	}
	return v
}

// filter passes one received chunk through, replacing it with a terminal
// ChunkError once text or reasoning output degenerates. The consumer's error
// path rides the sampling-recovery discard, so the loop never commits. A nil
// detector forwards everything untouched.
func (d *repetitionDetector) filter(c provider.Chunk) provider.Chunk {
	if d == nil {
		return c
	}
	if (c.Type == provider.ChunkText || c.Type == provider.ChunkReasoning) && d.observe(c.Text) {
		return provider.Chunk{Type: provider.ChunkError, Err: errRepetitionLoop}
	}
	return c
}

// samplingRetryPlan decides whether a failed sampling attempt is retried and
// under which discard reason and budget. Repetition trips get a deliberately
// smaller budget than transport interruptions (see maxRepetitionRetries).
func samplingRetryPlan(err error, attempt int) (reason string, retryMax int, ok bool) {
	if provider.IsStreamInterrupted(err) && attempt < maxSamplingAttempts {
		return provider.StreamInterruptReason(err), maxStreamRecoveries, true
	}
	if errors.Is(err, errRepetitionLoop) && attempt <= maxRepetitionRetries && attempt < maxSamplingAttempts {
		return repetitionDiscardReason, maxRepetitionRetries + 1, true
	}
	return "", 0, false
}

// repetitionDetector counts normalized line/sentence segments across a sliding
// window of one stream. A nil detector observes nothing and never trips.
type repetitionDetector struct {
	tripLimit  int
	carry      []byte
	ring       []repetitionKeys
	counts     map[string]int
	stemCounts map[string]int
	repeated   string
}

type repetitionKeys struct {
	exact string
	stem  string
}

func newRepetitionDetector(tripLimit int) *repetitionDetector {
	if tripLimit <= 0 {
		return nil
	}
	return &repetitionDetector{
		tripLimit:  tripLimit,
		counts:     make(map[string]int),
		stemCounts: make(map[string]int),
	}
}

// observe folds one streamed chunk and reports whether the guard has tripped.
// Once tripped it stays tripped.
func (d *repetitionDetector) observe(chunk string) bool {
	if d == nil {
		return false
	}
	if d.repeated != "" {
		return true
	}
	d.carry = append(d.carry, chunk...)
	for {
		seg, rest, ok := cutRepetitionSegment(d.carry)
		if !ok {
			break
		}
		d.carry = rest
		d.push(seg)
		if d.repeated != "" {
			return true
		}
	}
	// Separator-less streams flush in blocks so carry stays bounded; identical
	// blocks still count when the stream repeats in phase.
	if len(d.carry) > repetitionCarryMaxBytes {
		block := string(d.carry)
		d.carry = d.carry[:0]
		d.push(block)
	}
	return d.repeated != ""
}

// cutRepetitionSegment splits off the earliest complete segment: a newline
// always ends one; ASCII sentence enders need trailing whitespace so decimals
// and abbreviations survive; fullwidth CJK enders split unconditionally.
func cutRepetitionSegment(b []byte) (seg string, rest []byte, ok bool) {
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		switch r {
		case '\n':
			return string(b[:i]), b[i+size:], true
		case '.', '!', '?':
			next := i + size
			if next < len(b) && (b[next] == ' ' || b[next] == '\t' || b[next] == '\r' || b[next] == '\n') {
				return string(b[:next]), b[next:], true
			}
		case '。', '！', '？':
			return string(b[:i+size]), b[i+size:], true
		}
		i += size
	}
	return "", b, false
}

func (d *repetitionDetector) push(raw string) {
	seg := strings.ToLower(strings.TrimSpace(raw))
	if utf8.RuneCountInString(seg) < repetitionSegmentMinRunes {
		return
	}
	if len(seg) > repetitionSegmentMaxBytes {
		cut := repetitionSegmentMaxBytes
		for cut > 0 && !utf8.RuneStart(seg[cut]) {
			cut--
		}
		seg = seg[:cut]
	}
	keys := repetitionKeys{exact: seg, stem: repetitionStem(seg)}
	if len(d.ring) == repetitionWindowSegments {
		oldest := d.ring[0]
		d.ring = d.ring[1:]
		decrement(d.counts, oldest.exact)
		decrement(d.stemCounts, oldest.stem)
	}
	d.ring = append(d.ring, keys)
	d.counts[keys.exact]++
	d.stemCounts[keys.stem]++
	if d.counts[keys.exact] >= d.tripLimit || d.stemCounts[keys.stem] >= repetitionStemTripFactor*d.tripLimit {
		d.repeated = keys.stem
	}
}

func decrement(m map[string]int, key string) {
	if n := m[key] - 1; n > 0 {
		m[key] = n
	} else {
		delete(m, key)
	}
}

func repetitionStem(seg string) string {
	runes := 0
	for i := range seg {
		if runes == repetitionStemRunes {
			return seg[:i]
		}
		runes++
	}
	return seg
}
