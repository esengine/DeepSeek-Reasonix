// instance.go — a task template becomes one run's concrete history.
package main

import (
	"crypto/rand"
	"fmt"
	"hash/fnv"
	"io"
	mathrand "math/rand"
	"strings"
)

// The invariant this file exists for: no literal sufficient to satisfy
// AnswerRecovered for a concrete run may exist in this repository before that
// run begins. Grepping the corpus reveals the question, never the answer.

// varKind is how one placeholder's value is generated. Values stay shaped like
// real engineering records — a UUID would defeat the leak but turn every task
// into "find the random string".
type varKind string

const (
	varCodename varKind = "codename" // cobalt-k7m4q9
	varInt      varKind = "int"      // 731
	varDecimal  varKind = "decimal"  // 0.0437
	varDuration varKind = "duration" // 43ms
)

// varSpec declares one placeholder. Min/Max bound the numeric kinds.
type varSpec struct {
	Name string
	Kind varKind
	Word string // codename stem, e.g. "cobalt"
	Min  int
	Max  int
}

// codenameEntropy is how much of a codename is unguessable. Five base32 digits
// is about 25 bits: far past what a model can reach by knowing the stem.
const codenameEntropy = 5

const codenameAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// fixtureVars is one run's instantiated values.
type fixtureVars map[string]string

// newVars generates values for every spec. A nil rng means deterministic.
func newVars(specs []varSpec, rng *mathrand.Rand) fixtureVars {
	out := fixtureVars{}
	for _, spec := range specs {
		switch spec.Kind {
		case varCodename:
			out[spec.Name] = spec.Word + "-" + randomToken(rng, codenameEntropy)
		case varInt:
			out[spec.Name] = fmt.Sprint(randomBetween(rng, spec.Min, spec.Max))
		case varDecimal:
			out[spec.Name] = fmt.Sprintf("0.%04d", randomBetween(rng, spec.Min, spec.Max))
		case varDuration:
			out[spec.Name] = fmt.Sprintf("%dms", randomBetween(rng, spec.Min, spec.Max))
		}
	}
	return out
}

func randomToken(rng *mathrand.Rand, n int) string {
	var b strings.Builder
	for range n {
		b.WriteByte(codenameAlphabet[randomBetween(rng, 0, len(codenameAlphabet)-1)])
	}
	return b.String()
}

func randomBetween(rng *mathrand.Rand, lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + rng.Intn(hi-lo+1)
}

// seededRand is the deterministic generator preflight and tests use, so a
// fixture's tier calibration and its golden state reproduce exactly.
func seededRand(parts ...string) *mathrand.Rand {
	h := fnv.New64a()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return mathrand.New(mathrand.NewSource(int64(h.Sum64())))
}

// liveRand is the generator a paid run uses: every run gets values that existed
// nowhere before it started.
func liveRand() *mathrand.Rand {
	rng, err := liveRandFrom(rand.Reader)
	if err != nil {
		panic("contextbench: no entropy for a live run: " + err.Error())
	}
	return rng
}

// liveRandFrom seeds a generator from an entropy source, so what a run's values
// are unpredictable from is the source and nothing else.
func liveRandFrom(entropy io.Reader) (*mathrand.Rand, error) {
	var seed [8]byte
	if _, err := io.ReadFull(entropy, seed[:]); err != nil {
		return nil, err
	}
	var n int64
	for _, b := range seed {
		n = n<<8 | int64(b)
	}
	return mathrand.New(mathrand.NewSource(n)), nil
}

// instantiate substitutes {{name}} and refuses anything left over. The grammar
// is one identifier in double braces: no conditionals, no loops, no defaults.
// This is fixture data, not a template language.
func instantiate(text string, vars fixtureVars) (string, error) {
	out := text
	for name, value := range vars {
		out = strings.ReplaceAll(out, "{{"+name+"}}", value)
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "}}") {
		return "", fmt.Errorf("unresolved placeholder in %q", firstPlaceholder(out))
	}
	return out, nil
}

func firstPlaceholder(text string) string {
	i := strings.Index(text, "{{")
	if i < 0 {
		return text
	}
	j := strings.Index(text[i:], "}}")
	if j < 0 {
		return text[i:min(i+40, len(text))]
	}
	return text[i : i+j+2]
}
