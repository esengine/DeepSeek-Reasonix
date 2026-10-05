package main

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// reproductionSection says whether two reports are one capability version on
// one corpus, and whether their rates differ by more than chance would. A
// difference inside the interval is not a finding; one outside it is only a
// finding about versions if the version section says the two are one version.
func reproductionSection(pathA, pathB string) string {
	a, errA := loadResults(pathA)
	b, errB := loadResults(pathB)
	if errA != nil || errB != nil {
		return ""
	}
	var s strings.Builder
	s.WriteString("\n### Reproduction\n\n")
	fmt.Fprintf(&s, "- version: %s\n", versionVerdict(a, b))
	fmt.Fprintf(&s, "- corpus: %s\n\n", corpusVerdict(a, b))
	s.WriteString("| Rate | A | B | B − A (95% CI) | |\n|---|---:|---:|---:|---|\n")
	allWithin := true
	for _, m := range []struct {
		name string
		rate func([]result) (int, int)
	}{
		{"grader pass (solvable)", passRate},
		{"bundle completed", completedRate},
		{"false completion", falseCompletionRate},
	} {
		ka, na := m.rate(a)
		kb, nb := m.rate(b)
		if na == 0 || nb == 0 {
			continue
		}
		lo, hi := newcombe(ka, na, kb, nb)
		within := lo <= 0 && hi >= 0
		allWithin = allWithin && within
		verdict := "within noise"
		if !within {
			verdict = "differs"
		}
		fmt.Fprintf(&s, "| %s | %d/%d | %d/%d | %+.1fpp (%+.1f, %+.1f) | %s |\n", m.name, ka, na, kb, nb,
			100*(float64(kb)/float64(nb)-float64(ka)/float64(na)), 100*lo, 100*hi, verdict)
	}
	if allWithin && strings.HasPrefix(versionVerdict(a, b), "one version") && strings.HasPrefix(corpusVerdict(a, b), "same") {
		s.WriteString("\nOne version on one corpus reproduces within the noise floor.\n")
	}
	return s.String()
}

func counted(results []result) []result {
	return slices.DeleteFunc(slices.Clone(results), func(r result) bool { return r.Skipped || r.Attempt > 1 })
}

func versionVerdict(a, b []result) string {
	setOf := func(rs []result) []string {
		var out []string
		for _, r := range counted(rs) {
			if r.Trajectory != nil && r.Trajectory.CapabilitiesHash != "" && !slices.Contains(out, r.Trajectory.CapabilitiesHash) {
				out = append(out, r.Trajectory.CapabilitiesHash)
			}
		}
		return out
	}
	sa, sb := setOf(a), setOf(b)
	switch {
	case len(sa) == 0 || len(sb) == 0:
		return "unknown (a report records no capability set)"
	case len(sa) > 1 || len(sb) > 1:
		return "mixed (a report spans more than one capability set)"
	case sa[0] == sb[0]:
		return "one version (" + short(sa[0]) + ")"
	default:
		return fmt.Sprintf("two versions (%s vs %s): a difference is a comparison, not a reproduction", short(sa[0]), short(sb[0]))
	}
}

func corpusVerdict(a, b []result) string {
	prompts := func(rs []result) map[string]string {
		out := map[string]string{}
		for _, r := range counted(rs) {
			out[r.ID] = r.Prompt
		}
		return out
	}
	pa, pb := prompts(a), prompts(b)
	differ := 0
	for id, p := range pa {
		if q, ok := pb[id]; !ok || q != p {
			differ++
		}
	}
	for id := range pb {
		if _, ok := pa[id]; !ok {
			differ++
		}
	}
	if differ == 0 {
		return fmt.Sprintf("same %d tasks", len(pa))
	}
	return fmt.Sprintf("%d tasks differ between the reports", differ)
}

func passRate(rs []result) (int, int) {
	k, n := 0, 0
	for _, r := range counted(rs) {
		if r.NoSolution {
			continue
		}
		n++
		if r.Passed {
			k++
		}
	}
	return k, n
}

func completedRate(rs []result) (int, int) {
	k, n := 0, 0
	for _, r := range counted(rs) {
		if r.Trajectory == nil || r.Trajectory.BundleOutcome == "" {
			continue
		}
		n++
		if r.Trajectory.BundleOutcome == "completed" {
			k++
		}
	}
	return k, n
}

func falseCompletionRate(rs []result) (int, int) {
	k, n := 0, 0
	for _, r := range counted(rs) {
		if r.Trajectory == nil || r.Trajectory.BundleOutcome != "completed" {
			continue
		}
		n++
		if !r.Passed {
			k++
		}
	}
	return k, n
}

// newcombe is the 95% interval for p_b − p_a from two Wilson intervals, which
// stays sensible at the small counts and the 0% and 100% rates these reports
// produce, where the normal approximation does not.
func newcombe(ka, na, kb, nb int) (float64, float64) {
	pa, pb := float64(ka)/float64(na), float64(kb)/float64(nb)
	la, ua := wilson(ka, na)
	lb, ub := wilson(kb, nb)
	d := pb - pa
	return d - math.Sqrt((pb-lb)*(pb-lb)+(ua-pa)*(ua-pa)), d + math.Sqrt((ub-pb)*(ub-pb)+(pa-la)*(pa-la))
}

func wilson(k, n int) (float64, float64) {
	const z = 1.959964
	p, fn := float64(k)/float64(n), float64(n)
	center := (p + z*z/(2*fn)) / (1 + z*z/fn)
	half := z * math.Sqrt(p*(1-p)/fn+z*z/(4*fn*fn)) / (1 + z*z/fn)
	return center - half, center + half
}
