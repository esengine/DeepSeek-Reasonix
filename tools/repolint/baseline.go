package main

import (
	"encoding/json"
	"fmt"
	"os"
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

func (b *Baseline) exceeded(findings []Finding) ([]Finding, []string) {
	counts := map[string]map[string]int{}
	totals := map[string]int{}
	for _, f := range findings {
		if counts[f.File] == nil {
			counts[f.File] = map[string]int{}
		}
		counts[f.File][f.Rule] += f.Weight
		totals[f.Rule] += f.Weight
	}

	var msgs []string
	over := map[string]map[string]bool{}
	for _, file := range sortedKeys(counts) {
		for _, rule := range sortedKeys(counts[file]) {
			allowed := b.Files[file][rule]
			if got := counts[file][rule]; got > allowed {
				msgs = append(msgs, fmt.Sprintf("%s: %s is %d over its %d budget", file, rule, got, allowed))
				if over[file] == nil {
					over[file] = map[string]bool{}
				}
				over[file][rule] = true
			}
		}
	}
	for _, rule := range sortedKeys(totals) {
		if totals[rule] > b.Limits[rule] {
			msgs = append(msgs, fmt.Sprintf("repo total for %s is %d, above the %d ceiling", rule, totals[rule], b.Limits[rule]))
		}
	}

	var shown []Finding
	for _, f := range findings {
		if over[f.File][f.Rule] {
			shown = append(shown, f)
		}
	}
	return shown, msgs
}
