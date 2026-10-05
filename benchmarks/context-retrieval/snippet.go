// snippet.go — does a rank-1 hit actually hand over the answer?
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/retrieval"
	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
)

// TargetHit is not EvidenceSufficient: a rank-1 hit can still hand over a
// window too short to answer with, and searching again after that is correct.
// Measured through the shipped recall path, not a copy of it.

// snippetWidths are the windows to compare. 240 is what ships.
var snippetWidths = []int{120, 180, 240, 320, 480}

// historicalQueries are the queries these tasks actually drew in the pilots,
// kept because a probe query is the host's phrasing and the model's was not.
var historicalQueries = map[string][]string{
	"i03-coalescing":     {"coalesce window marker probe", "job completion coalescing window"},
	"i04-recovery-fence": {"recovery fence marker epoch", "expected epoch", "recfence quartz"},
}

type snippetReading struct {
	Task     string
	Query    string
	Width    int
	Covered  int
	Total    int
	Runes    int
	Rank     int
	Complete bool
}

// runSnippetAudit answers the question offline, over many instantiations so a
// single seed's lengths cannot decide it.
func runSnippetAudit(root string, seeds int) int {
	fmt.Printf("## Snippet coverage (%d instantiations per task)\n", seeds)
	fmt.Println("  Does the window a rank-1 hit returns contain every value the task scores on?")

	byTaskWidth := map[string]map[int][]snippetReading{}
	for _, t := range allTasks() {
		byTaskWidth[t.ID] = map[int][]snippetReading{}
		for seed := range seeds {
			inst, err := instantiateTask(t, seededRand(t.ID, "snippet", fmt.Sprint(seed)))
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			queries := append([]string{inst.ProbeQuery}, historicalQueries[t.ID]...)
			for _, q := range queries {
				for _, width := range snippetWidths {
					r := measureSnippet(inst, q, width)
					byTaskWidth[t.ID][width] = append(byTaskWidth[t.ID][width], r)
				}
			}
		}
	}

	fmt.Printf("\n  %-24s %-5s", "task", "vals")
	for _, w := range snippetWidths {
		fmt.Printf("%9d", w)
	}
	fmt.Println("   mean runes @240")
	for _, t := range allTasks() {
		vals := len(t.AnswerVars)
		fmt.Printf("  %-24s %-5d", t.ID, vals)
		for _, w := range snippetWidths {
			fmt.Printf("%8.0f%%", 100*completeRate(byTaskWidth[t.ID][w]))
		}
		fmt.Printf("   %.0f\n", meanRunes(byTaskWidth[t.ID][240]))
	}
	reportShippedPath(root)
	return 0
}

// measureSnippet renders one hit through the same MakeSnippet the recall path
// uses, then asks whether every scored value survived the window.
func measureSnippet(inst fixtureInstance, query string, width int) snippetReading {
	terms, err := retrieval.QueryTerms(query)
	if err != nil {
		return snippetReading{Task: inst.Task.ID, Query: query, Width: width, Total: len(inst.AnswerMarkers)}
	}
	// The tool result is the document the answer lives in; a call's arguments
	// are a separate document and never carry it.
	snippet := retrieval.MakeSnippet(inst.body, query, terms, width)
	covered := 0
	for _, marker := range inst.AnswerMarkers {
		if strings.Contains(strings.ToLower(snippet), strings.ToLower(marker)) {
			covered++
		}
	}
	return snippetReading{
		Task: inst.Task.ID, Query: query, Width: width,
		Covered: covered, Total: len(inst.AnswerMarkers),
		Runes: len([]rune(snippet)), Complete: covered == len(inst.AnswerMarkers),
	}
}

// reportShippedPath cross-checks the table against the real recall tool: a
// fixture, a fold, and a search through the agent that ships.
func reportShippedPath(root string) {
	fmt.Println("\n## Through the shipped recall path (width as shipped)")
	for _, t := range allTasks() {
		inst, err := instantiateTask(t, seededRand(t.ID, "snippet-live"))
		if err != nil {
			continue
		}
		f, err := buildFixture(inst, ablation.Set{}, filepath.Join(root, "snippet", t.ID, "session.jsonl"))
		if err != nil {
			fmt.Printf("  %-24s fixture: %v\n", t.ID, err)
			continue
		}
		a := agent.New(&digestProvider{}, tool.NewRegistry(), f.Session, agent.Options{
			ContextWindow: fixtureWindow, CompactRatio: 0.5, RecentKeep: 2,
			SessionPath: f.Path, KeepPolicy: agent.KeepErrors,
		}, event.Discard)
		a.LoadProjectionSidecar(f.Path)
		queries := append([]string{inst.ProbeQuery}, historicalQueries[t.ID]...)
		for _, q := range queries {
			res, err := a.RecallContext(context.Background(), tool.RecallRequest{Query: q})
			if err != nil {
				fmt.Printf("  %-24s %-34q recall: %v\n", t.ID, q, err)
				continue
			}
			covered := 0
			for _, marker := range inst.AnswerMarkers {
				if strings.Contains(strings.ToLower(res.Text), strings.ToLower(marker)) {
					covered++
				}
			}
			fmt.Printf("  %-24s %-34q %d/%d values in the result\n",
				t.ID, truncate(q, 32), covered, len(inst.AnswerMarkers))
		}
	}
}

func completeRate(rs []snippetReading) float64 {
	if len(rs) == 0 {
		return 0
	}
	n := 0
	for _, r := range rs {
		if r.Complete {
			n++
		}
	}
	return float64(n) / float64(len(rs))
}

func meanRunes(rs []snippetReading) float64 {
	if len(rs) == 0 {
		return 0
	}
	total := 0
	for _, r := range rs {
		total += r.Runes
	}
	return float64(total) / float64(len(rs))
}

// answerCoverage is what a run should have recorded all along: how much of the
// scored answer a search actually handed over. rank=1 with coverage 1/3 is not
// a stopping failure.
func answerCoverage(text string, markers []string) int {
	covered := 0
	lower := strings.ToLower(text)
	for _, marker := range markers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			covered++
		}
	}
	return covered
}
