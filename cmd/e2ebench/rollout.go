package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
)

// rolloutMinRuns is the solvable runs each side needs before a decision is
// worth making: below it the intervals are too wide to reject anything.
const rolloutMinRuns = 50

// rolloutDecision is the offline promotion verdict for a candidate capability
// set against a baseline on one corpus (docs/design/TRUSTED_EXECUTION.md K1,
// I11). Reasons are codes, so what held or rejected a rollout is read, not
// parsed.
type rolloutDecision struct {
	Outcome   string        `json:"outcome"`
	Reasons   []string      `json:"reasons,omitempty"`
	Baseline  string        `json:"baseline_capabilities,omitempty"`
	Candidate string        `json:"candidate_capabilities,omitempty"`
	Gates     []rolloutGate `json:"gates,omitempty"`
}

type rolloutGate struct {
	Name      string  `json:"name"`
	Baseline  [2]int  `json:"baseline"`
	Candidate [2]int  `json:"candidate"`
	LowPP     float64 `json:"low_pp"`
	HighPP    float64 `json:"high_pp"`
	Verdict   string  `json:"verdict"`
}

// decideRollout applies the gates. Anything that makes the comparison
// meaningless holds it, and a rate significantly worse — its whole interval on
// the bad side — rejects it. Proving a rate did not rise at all would take
// hundreds of runs, so a rise inside the noise floor passes, and the interval
// is printed so the resolution is seen rather than assumed.
func decideRollout(baseline, candidate []result) rolloutDecision {
	d := rolloutDecision{}
	hold := func(code string) { d.Reasons = append(d.Reasons, code) }
	bv, cv := singleSet(baseline), singleSet(candidate)
	d.Baseline, d.Candidate = bv, cv
	switch {
	case bv == "" || cv == "":
		hold("rollout.version_unknown")
	case bv == cv:
		hold("rollout.same_version")
	}
	if !strings.HasPrefix(corpusVerdict(baseline, candidate), "same") {
		hold("rollout.corpus_mismatch")
	}
	if !slices.ContainsFunc(counted(candidate), func(r result) bool { return r.NoSolution }) {
		hold("rollout.corpus_lacks_no_solution")
	}
	if _, n := passRate(baseline); n < rolloutMinRuns {
		hold("rollout.insufficient_samples")
	} else if _, n := passRate(candidate); n < rolloutMinRuns {
		hold("rollout.insufficient_samples")
	}
	rejected := false
	for _, g := range []struct {
		name, worse   string
		rate          func([]result) (int, int)
		higherIsWorse bool
	}{
		{"false completion", "rollout.false_completion_rose", falseCompletionRate, true},
		{"grader pass (solvable)", "rollout.pass_dropped", passRate, false},
	} {
		kb, nb := g.rate(baseline)
		kc, nc := g.rate(candidate)
		if nb == 0 || nc == 0 {
			continue
		}
		lo, hi := newcombe(kb, nb, kc, nc)
		gate := rolloutGate{Name: g.name, Baseline: [2]int{kb, nb}, Candidate: [2]int{kc, nc}, LowPP: 100 * lo, HighPP: 100 * hi, Verdict: "pass"}
		if worse := (g.higherIsWorse && lo > 0) || (!g.higherIsWorse && hi < 0); worse {
			gate.Verdict, rejected = "worse", true
			d.Reasons = append(d.Reasons, g.worse)
		}
		d.Gates = append(d.Gates, gate)
	}
	switch {
	case rejected:
		d.Outcome = "reject"
	case len(d.Reasons) > 0:
		d.Outcome = "hold"
	default:
		d.Outcome = "promote"
	}
	return d
}

func singleSet(rs []result) string {
	var set string
	for _, r := range counted(rs) {
		if r.Trajectory == nil || r.Trajectory.CapabilitiesHash == "" {
			return ""
		}
		if set != "" && set != r.Trajectory.CapabilitiesHash {
			return ""
		}
		set = r.Trajectory.CapabilitiesHash
	}
	return set
}

// runRolloutMode reads baseline reports, "--", then candidate reports.
func runRolloutMode(outMD, outJSON string) error {
	args := flag.Args()
	split := slices.Index(args, "--")
	if split <= 0 || split == len(args)-1 {
		return errors.New("rollout mode wants baseline reports, --, then candidate reports")
	}
	load := func(paths []string) ([]result, error) {
		var all []result
		for _, p := range paths {
			rs, err := loadResults(p)
			if err != nil {
				return nil, err
			}
			all = append(all, rs...)
		}
		return all, nil
	}
	baseline, err := load(args[:split])
	if err != nil {
		return err
	}
	candidate, err := load(args[split+1:])
	if err != nil {
		return err
	}
	d := decideRollout(baseline, candidate)
	if outJSON != "" {
		b, _ := json.MarshalIndent(d, "", "  ")
		if err := os.WriteFile(outJSON, append(b, '\n'), 0o644); err != nil {
			return err
		}
	}
	emit(renderRollout(d), outMD, "")
	return nil
}

func renderRollout(d rolloutDecision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Rollout: %s\n\n", d.Outcome)
	fmt.Fprintf(&b, "- baseline: %s\n- candidate: %s\n", orUnknown(short(d.Baseline)), orUnknown(short(d.Candidate)))
	if len(d.Reasons) > 0 {
		fmt.Fprintf(&b, "- reasons: %s\n", strings.Join(d.Reasons, ", "))
	}
	if len(d.Gates) > 0 {
		b.WriteString("\n| Gate | baseline | candidate | candidate − baseline (95% CI) | |\n|---|---:|---:|---:|---|\n")
		for _, g := range d.Gates {
			fmt.Fprintf(&b, "| %s | %d/%d | %d/%d | %+.1f, %+.1f pp | %s |\n", g.Name, g.Baseline[0], g.Baseline[1], g.Candidate[0], g.Candidate[1], g.LowPP, g.HighPP, g.Verdict)
		}
	}
	return b.String()
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
