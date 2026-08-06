// Package cosplay implements CoSPlay-style inference-time co-evolution for
// code verification: instead of relying on expensive human-labeled ground
// truth or self-generated tests that are themselves unreliable, the verifier
// generates high-discrimination test cases that deliberately challenge the
// candidate code, executes a code×test result matrix over multiple repair
// rounds (fixing broken code, pruning ineffective tests), and finally selects
// the best candidate by consensus clustering.
//
// Reference: CoSPlay (HKUST-GZ + JITRI) — no ground-truth data, no
// fine-tuning; only inference-time co-evolution. Reported gains on
// Qwen2.5-7B-Instruct: best score 22.1% → 33.2%, self-test accuracy
// 14.6% → 78.3%.
package cosplay

import (
	"context"
	"fmt"
	"sort"
)

// Example is one ground-truth-free input/output pair used to build
// discriminating tests. It is the highest-value signal: a candidate that
// passes every smoke test but fails an example is precisely the failure mode
// co-evolution exists to catch.
type Example struct {
	Input    string
	Expected string
}

// Task describes what the code is supposed to do.
type Task struct {
	Description string
	Language    string   // "go" | "python" | "javascript" | ...
	Function    string   // entry function name, if known
	Examples    []Example // discriminating input/output pairs
}

// Candidate is one code attempt to verify (the original generation or a
// repair-round output).
type Candidate struct {
	ID       string
	Code     string
	Language string
	Tests    []string // tests the candidate ships with (optional)
}

// TestCase is one discriminating test.
type TestCase struct {
	ID       string
	Language string
	Body     string // runnable test code (for the Runner)
	Source   string // "generated" | "candidate" | "template"
	Input    string
	Expected string
}

// Failure is one failing cell of the execution matrix, fed back to the
// Repairer.
type Failure struct {
	TestID   string
	Input    string
	Expected string
	Got      string
	Detail   string
}

// Report is the final verdict of one Verify run.
type Report struct {
	Best        Candidate
	PassRate    float64
	Rounds      int
	Tests       int
	Passed      int
	Discarded   []string // test ids pruned as ineffective
	Corrections []string // repair-round descriptions
}

// Verifier orchestrates the co-evolution loop: generate → matrix → repair →
// consensus. All components are interfaces so the model-backed and the
// fully-offline template paths share one engine.
type Verifier struct {
	// Gen produces discriminating tests. Required.
	Gen TestGenerator
	// Runner executes candidate×test cells. Required.
	Runner Runner
	// Repair fixes a failing candidate given the failure set. Optional: when
	// nil, the verifier reports failures but performs no repair rounds.
	Repair Repairer
	// MaxRounds bounds repair iterations (default 2).
	MaxRounds int
	// NumTests is how many generated tests to request per round (default 4).
	NumTests int
}

// NewVerifier builds a verifier with defaults applied.
func NewVerifier(gen TestGenerator, runner Runner, repair Repairer) *Verifier {
	return &Verifier{Gen: gen, Runner: runner, Repair: repair}
}

// Verify runs the full co-evolution cycle and returns the consensus-chosen
// best candidate.
func (v *Verifier) Verify(ctx context.Context, task Task, cand Candidate) (Report, error) {
	if v.Gen == nil {
		return Report{}, fmt.Errorf("cosplay: verifier requires a TestGenerator")
	}
	if v.Runner == nil {
		return Report{}, fmt.Errorf("cosplay: verifier requires a Runner")
	}
	rounds := v.MaxRounds
	if rounds <= 0 {
		rounds = 2
	}
	n := v.NumTests
	if n <= 0 {
		n = 4
	}

	// Step 1 — exploration & attack: generate high-discrimination tests and
	// merge the candidate's own tests (deduped by body).
	tests := v.Gen.Generate(ctx, task, cand.Code, n)
	if len(tests) == 0 {
		tests = v.Gen.Generate(ctx, task, cand.Code, n) // template fallback
	}
	tests = mergeCandidateTests(tests, cand.Tests, cand.Language)
	if len(tests) == 0 {
		return Report{}, fmt.Errorf("cosplay: no tests could be generated")
	}

	// Step 2 — execution matrix across repair rounds.
	matrix := NewMatrix()
	cands := []Candidate{cand}
	var corrections []string
	pruned := []string{}

	current := cand
	round := 0
	for {
		fails := runAll(ctx, v.Runner, current, tests, matrix)
		round++
		if len(fails) == 0 || v.Repair == nil || round > rounds {
			break
		}
		// Step 3 — repair: fix the candidate against the current failures.
		fixed := v.Repair.Repair(ctx, task, current.Code, fails, current.Language)
		if fixed == "" || fixed == current.Code {
			break
		}
		current = Candidate{
			ID:       fmt.Sprintf("%s-r%d", cand.ID, round),
			Code:     fixed,
			Language: cand.Language,
		}
		cands = append(cands, current)
		corrections = append(corrections, fmt.Sprintf("round %d: repaired against %d failures", round, len(fails)))
		// Prune ineffective tests now that at least two candidates have run:
		// tests nobody has passed across multiple attempts do not discriminate
		// and would only pollute the repair signal.
		var pr []string
		tests, pr = pruneIneffective(tests, matrix)
		pruned = append(pruned, pr...)
		if len(cands) >= rounds+1 {
			break
		}
	}

	// Step 4 — consensus clustering over all candidates.
	scored := Consensus(matrix, cands, tests)

	best := cand
	rate := 0.0
	if len(scored) > 0 {
		best = scored[0].Candidate
		rate = scored[0].PassRate
	}
	passed := 0
	for _, tc := range tests {
		if m := matrix.PassFor(best.ID, tc.ID); m {
			passed++
		}
	}
	return Report{
		Best:        best,
		PassRate:    rate,
		Rounds:      round,
		Tests:       len(tests),
		Passed:      passed,
		Discarded:   pruned,
		Corrections: corrections,
	}, nil
}

// runAll executes every candidate×test cell and returns the failures of the
// current candidate.
func runAll(ctx context.Context, runner Runner, cand Candidate, tests []TestCase, matrix *ExecMatrix) []Failure {
	var fails []Failure
	for _, tc := range tests {
		pass, got, detail, err := runner.Run(ctx, cand, tc)
		matrix.Record(cand.ID, tc.ID, pass)
		if err != nil {
			matrix.RecordDetail(cand.ID, tc.ID, "runner error: "+err.Error())
			fails = append(fails, Failure{TestID: tc.ID, Input: tc.Input, Expected: tc.Expected, Got: got, Detail: err.Error()})
			continue
		}
		if !pass {
			fails = append(fails, Failure{TestID: tc.ID, Input: tc.Input, Expected: tc.Expected, Got: got, Detail: detail})
		}
	}
	return fails
}

// mergeCandidateTests appends the candidate's own tests, deduped by body.
func mergeCandidateTests(tests []TestCase, own []string, lang string) []TestCase {
	seen := make(map[string]bool, len(tests))
	for _, t := range tests {
		seen[t.Body] = true
	}
	for i, body := range own {
		if body == "" || seen[body] {
			continue
		}
		seen[body] = true
		tests = append(tests, TestCase{
			ID:       fmt.Sprintf("cand-%d", i),
			Language: lang,
			Body:     body,
			Source:   "candidate",
		})
	}
	return tests
}

// pruneIneffective removes tests that no candidate has passed across at
// least two executed candidates (they cannot discriminate good from bad code
// and only pollute the repair signal) and returns the pruned list plus the
// removed ids. A test that ran once and failed is kept — one candidate's
// failure is not yet evidence.
func pruneIneffective(tests []TestCase, matrix *ExecMatrix) ([]TestCase, []string) {
	kept := tests[:0]
	var removed []string
	for _, tc := range tests {
		if matrix.PassedByAny(tc.ID) {
			kept = append(kept, tc)
			continue
		}
		if matrix.ExecutedBy(tc.ID) >= 2 {
			removed = append(removed, tc.ID)
			continue
		}
		kept = append(kept, tc)
	}
	return kept, removed
}

// ScoredCandidate is a candidate plus its consensus score.
type ScoredCandidate struct {
	Candidate  Candidate
	PassRate   float64
	Discrim    float64 // average distinguishing power of tests it passes
}

// Consensus ranks candidates by pass rate (tie-broken by discrimination)
// and collapses near-identical scores into clusters, returning one
// representative per cluster in descending order. The top entry is the
// co-evolved best answer.
func Consensus(matrix *ExecMatrix, cands []Candidate, tests []TestCase) []ScoredCandidate {
	discrim := testDiscrimination(matrix, tests)
	scored := make([]ScoredCandidate, 0, len(cands))
	for _, c := range cands {
		rate := matrix.PassRate(c.ID)
		d := 0.0
		var n float64
		for _, tc := range tests {
			if ok := matrix.PassFor(c.ID, tc.ID); ok {
				d += discrim[tc.ID]
				n++
			}
		}
		if n > 0 {
			d /= n
		}
		scored = append(scored, ScoredCandidate{Candidate: c, PassRate: rate, Discrim: d})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].PassRate != scored[j].PassRate {
			return scored[i].PassRate > scored[j].PassRate
		}
		return scored[i].Discrim > scored[j].Discrim
	})
	// Cluster: collapse entries whose pass rates are within 5% of the
	// previous representative.
	var out []ScoredCandidate
	for _, s := range scored {
		if len(out) == 0 || s.PassRate < out[len(out)-1].PassRate-0.05 {
			out = append(out, s)
			continue
		}
		// same cluster: keep the higher-ranked representative (already sorted)
	}
	return out
}

// testDiscrimination scores each test by how unevenly candidates pass it. A
// test everyone passes (or everyone fails) cannot tell good code from bad and
// gets a low weight; a test roughly half the candidates pass is maximally
// discriminating. Falls back to 1.0 (neutral) for unexecuted tests.
func testDiscrimination(matrix *ExecMatrix, tests []TestCase) map[string]float64 {
	out := make(map[string]float64, len(tests))
	for _, tc := range tests {
		pass, total := 0, 0
		for _, cid := range matrix.CandidateIDs() {
			if v, ok := matrix.Lookup(cid, tc.ID); ok {
				total++
				if v {
					pass++
				}
			}
		}
		if total == 0 {
			out[tc.ID] = 1.0
			continue
		}
		frac := float64(pass) / float64(total)
		d := 2 * min2(frac, 1-frac)
		if d < 0.1 {
			d = 0.1 // never fully discount a test
		}
		out[tc.ID] = d
	}
	return out
}

func min2(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
