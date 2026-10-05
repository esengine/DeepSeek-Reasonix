package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
)

// Ratchet snapshot, in weighted units: no file may exceed its budget and no
// rule may exceed its repo ceiling. Both are relaxed only by hand, in a diff a
// reviewer sees.
type Baseline struct {
	Limits map[string]int            `json:"limits"`
	Files  map[string]map[string]int `json:"files"`
}

func loadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if b.Limits == nil {
		b.Limits = map[string]int{}
	}
	if b.Files == nil {
		b.Files = map[string]map[string]int{}
	}
	return &b, nil
}

func baselineFrom(findings []Finding) *Baseline {
	b := &Baseline{Limits: map[string]int{}, Files: map[string]map[string]int{}}
	for _, rule := range allRules {
		b.Limits[rule] = 0
	}
	for _, f := range findings {
		b.Limits[f.Rule] += f.Weight
		if b.Files[f.File] == nil {
			b.Files[f.File] = map[string]int{}
		}
		b.Files[f.File][f.Rule] += f.Weight
	}
	return b
}

func (b *Baseline) write(path string) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Overrun is one budget the tree went past: a file's rule budget, or a
// repo-wide ceiling for a rule when File is empty. Callers format it; keeping
// it structured is what lets a report be narrowed to the files a change touched.
type Overrun struct {
	File    string
	Rule    string
	Count   int
	Allowed int
}

func (o Overrun) String() string {
	if o.File == "" {
		return fmt.Sprintf("repo total for %s is %d, above the %d ceiling", o.Rule, o.Count, o.Allowed)
	}
	return fmt.Sprintf("%s: %s is %d over its %d budget", o.File, o.Rule, o.Count, o.Allowed)
}

func (b *Baseline) exceeded(findings []Finding) ([]Finding, []Overrun) {
	counts := map[string]map[string]int{}
	totals := map[string]int{}
	for _, f := range findings {
		if counts[f.File] == nil {
			counts[f.File] = map[string]int{}
		}
		counts[f.File][f.Rule] += f.Weight
		totals[f.Rule] += f.Weight
	}

	var overruns []Overrun
	over := map[string]map[string]bool{}
	for _, file := range sortedKeys(counts) {
		for _, rule := range sortedKeys(counts[file]) {
			allowed := b.Files[file][rule]
			if got := counts[file][rule]; got > allowed {
				overruns = append(overruns, Overrun{File: file, Rule: rule, Count: got, Allowed: allowed})
				if over[file] == nil {
					over[file] = map[string]bool{}
				}
				over[file][rule] = true
			}
		}
	}
	for _, rule := range sortedKeys(totals) {
		if totals[rule] > b.Limits[rule] {
			overruns = append(overruns, Overrun{Rule: rule, Count: totals[rule], Allowed: b.Limits[rule]})
		}
	}

	var shown []Finding
	for _, f := range findings {
		if over[f.File][f.Rule] {
			shown = append(shown, f)
		}
	}
	return shown, overruns
}

// widenings names every budget the new baseline would raise. Tightening is what
// a ratchet is for and needs no ceremony; raising one carries debt forward and
// is the move REASONIX.md refuses — so it has to be asked for rather than
// arriving inside a routine -update.
func widenings(old, next *Baseline) []string {
	if old == nil {
		return nil
	}
	var out []string
	for rule, want := range next.Limits {
		if had, ok := old.Limits[rule]; ok && want > had {
			out = append(out, fmt.Sprintf("ceiling %s: %d -> %d", rule, had, want))
		}
	}
	for file, rules := range next.Files {
		for rule, want := range rules {
			had := 0
			if prev, ok := old.Files[file]; ok {
				had = prev[rule]
			}
			if want > had {
				out = append(out, fmt.Sprintf("%s: %s %d -> %d", file, rule, had, want))
			}
		}
	}
	slices.Sort(out)
	return out
}

// reclaimable is how much budget the tree no longer needs: debt paid down since
// the last -update. It is not a failure, but an unreported surplus is a file
// that may quietly regress to where it used to be.
func reclaimable(old *Baseline, findings []Finding) map[string]int {
	if old == nil {
		return nil
	}
	actual := map[string]int{}
	for _, f := range findings {
		actual[f.Rule] += f.Weight
	}
	out := map[string]int{}
	for rule, had := range old.Limits {
		if slack := had - actual[rule]; slack > 0 {
			out[rule] = slack
		}
	}
	return out
}
