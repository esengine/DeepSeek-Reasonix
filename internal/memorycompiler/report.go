package memorycompiler

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// StrategyReport summarizes one strategy's learned outcome counters, split by
// whether the compiled contract was provider-visible that turn.
type StrategyReport struct {
	ID                string
	Description       string
	Successes         int
	Failures          int
	InjectedSuccesses int
	InjectedFailures  int
	LastUsedAt        time.Time
}

func (s StrategyReport) samples() int         { return s.Successes + s.Failures }
func (s StrategyReport) injectedSamples() int { return s.InjectedSuccesses + s.InjectedFailures }

// LearningsReport is a read-only, human-readable snapshot of what Memory v5
// has learned for one project. It exposes local state only; nothing here is
// sent to a provider.
type LearningsReport struct {
	UpdatedAt       time.Time
	Strategies      []StrategyReport
	NoisePatterns   []string
	Improvements    []string
	Findings        []string
	Nodes           int
	HighSignalNodes int
}

// LearningsReport loads the project's learned state. ok is false when nothing
// has been learned yet. maxItems bounds each list; <=0 uses a default.
func (r *Runtime) LearningsReport(maxItems int) (LearningsReport, bool) {
	if r == nil {
		return LearningsReport{}, false
	}
	if maxItems <= 0 {
		maxItems = 8
	}
	st := r.loadState()
	rep := LearningsReport{UpdatedAt: st.UpdatedAt, Nodes: len(st.Nodes)}
	for _, node := range st.Nodes {
		if node.Quality == QualityHighSignal {
			rep.HighSignalNodes++
		}
	}
	for _, s := range st.Strategies {
		if s.Samples() == 0 {
			continue
		}
		rep.Strategies = append(rep.Strategies, StrategyReport{
			ID:                s.ID,
			Description:       s.Description,
			Successes:         s.Successes,
			Failures:          s.Failures,
			InjectedSuccesses: s.InjectedSuccesses,
			InjectedFailures:  s.InjectedFailures,
			LastUsedAt:        s.LastUsedAt,
		})
	}
	sort.Slice(rep.Strategies, func(i, j int) bool {
		if rep.Strategies[i].samples() == rep.Strategies[j].samples() {
			return rep.Strategies[i].ID < rep.Strategies[j].ID
		}
		return rep.Strategies[i].samples() > rep.Strategies[j].samples()
	})
	for _, ref := range sortedNoisyRefs(st.NoisyRefs) {
		rep.NoisePatterns = append(rep.NoisePatterns, fmt.Sprintf("%s (x%d)", ref.ref, ref.count))
	}
	rep.NoisePatterns = limitStrings(rep.NoisePatterns, maxItems)
	for _, l := range recentLearnings(st.Learnings, maxItems) {
		rep.Improvements = append(rep.Improvements, l.CompilerImprovements...)
		rep.Findings = append(rep.Findings, l.CausalFindings...)
	}
	rep.Improvements = limitStrings(dedupeStrings(rep.Improvements), maxItems)
	rep.Findings = limitStrings(dedupeStrings(rep.Findings), maxItems)
	ok := len(rep.Strategies)+len(rep.NoisePatterns)+len(rep.Improvements)+len(rep.Findings) > 0
	return rep, ok
}

// FormatLearningsReport renders the report as plain notice text for the CLI and
// desktop slash-command surfaces.
func FormatLearningsReport(rep LearningsReport) string {
	var b strings.Builder
	b.WriteString("memory-v5 learnings")
	if !rep.UpdatedAt.IsZero() {
		fmt.Fprintf(&b, " (updated %s)", rep.UpdatedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if rep.Nodes > 0 {
		fmt.Fprintf(&b, "\nmemory nodes: %d (%d high-signal)", rep.Nodes, rep.HighSignalNodes)
	}
	if len(rep.Strategies) > 0 {
		b.WriteString("\nstrategies:")
		for _, s := range rep.Strategies {
			fmt.Fprintf(&b, "\n  %s: %s", s.ID, formatOutcomeSplit(s.Successes, s.Failures))
			if s.injectedSamples() > 0 {
				observedOK := s.Successes - s.InjectedSuccesses
				observedFail := s.Failures - s.InjectedFailures
				fmt.Fprintf(&b, " | injected %s vs observed %s",
					formatOutcomeSplit(s.InjectedSuccesses, s.InjectedFailures),
					formatOutcomeSplit(observedOK, observedFail))
			}
		}
	}
	writeReportList(&b, "repeated noise patterns", rep.NoisePatterns)
	writeReportList(&b, "recent improvements", rep.Improvements)
	writeReportList(&b, "recent findings", rep.Findings)
	return b.String()
}

func formatOutcomeSplit(ok, fail int) string {
	total := ok + fail
	if total == 0 {
		return "0 samples"
	}
	return fmt.Sprintf("%d ok / %d fail (%d%%)", ok, fail, int(float64(ok)/float64(total)*100+0.5))
}

func writeReportList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString("\n" + title + ":")
	for _, item := range items {
		b.WriteString("\n  - " + summarizeText(item, 160))
	}
}
