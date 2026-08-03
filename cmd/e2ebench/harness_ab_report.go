package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type abProjection struct {
	SchemaVersion int            `json:"schema_version"`
	Manifest      abManifest     `json:"manifest"`
	Cells         []abRecord     `json:"cells"`
	Arms          []abArmSummary `json:"arms"`
	Paired        abPairSummary  `json:"paired"`
}

type abArmSummary struct {
	ArmID                  string   `json:"arm_id"`
	Scored                 int      `json:"scored"`
	Passed                 int      `json:"passed"`
	TerminalInfraFailures  int      `json:"terminal_infra_failures"`
	InfraAttempts          int      `json:"infra_attempts"`
	MissingMetricsAttempts int      `json:"missing_metrics_attempts"`
	Timeouts               int      `json:"timeouts"`
	BudgetExhausted        int      `json:"suite_budget_exhausted"`
	PromptTokens           int      `json:"prompt_tokens"`
	CompletionTokens       int      `json:"completion_tokens"`
	CacheHitTokens         int      `json:"cache_hit_tokens"`
	CacheMissTokens        int      `json:"cache_miss_tokens"`
	ToolResultsProjected   int      `json:"tool_results_projected"`
	ProjectionSavedChars   int      `json:"projection_saved_chars"`
	Cost                   float64  `json:"cost"`
	Currency               string   `json:"currency"`
	CostPerPass            *float64 `json:"cost_per_pass"`
}

type abPairSummary struct {
	EligiblePairs int      `json:"eligible_pairs"`
	TotalPairs    int      `json:"total_pairs"`
	BothPass      int      `json:"both_pass"`
	BaselineOnly  int      `json:"baseline_only"`
	CandidateOnly int      `json:"candidate_only"`
	BothFail      int      `json:"both_fail"`
	DeltaPP       *float64 `json:"candidate_minus_baseline_percentage_points"`
	McNemarP      *float64 `json:"mcnemar_exact_p"`
}

func writeABProjections(runDir string, manifest abManifest, records []abRecord) error {
	projection := buildABProjection(manifest, records)
	if err := writeJSONAtomic(filepath.Join(runDir, "results.json"), projection); err != nil {
		return err
	}
	csvBody, err := renderABCSV(manifest, latestABRecords(records))
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(runDir, "results.csv"), csvBody, 0o644); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(runDir, "report.md"), []byte(renderABReport(projection)), 0o644)
}

func buildABProjection(manifest abManifest, records []abRecord) abProjection {
	latestMap := latestABRecords(records)
	cells := make([]abRecord, 0, len(latestMap))
	for _, r := range latestMap {
		cells = append(cells, r)
	}
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].TaskID != cells[j].TaskID {
			return cells[i].TaskID < cells[j].TaskID
		}
		if cells[i].Repetition != cells[j].Repetition {
			return cells[i].Repetition < cells[j].Repetition
		}
		return cells[i].ArmID < cells[j].ArmID
	})

	arms := make([]abArmSummary, 0, len(manifest.Arms))
	for _, arm := range manifest.Arms {
		summary := abArmSummary{ArmID: arm.ID}
		currencies := make(map[string]bool)
		for _, r := range cells {
			if r.ArmID != arm.ID {
				continue
			}
			if r.Scored {
				summary.Scored++
				if r.Passed {
					summary.Passed++
				}
			}
			if r.Event == abEventAttemptFinished && !r.Scored && r.Attempt >= 1+manifest.InfraRetries {
				summary.TerminalInfraFailures++
			}
			if r.Outcome == abOutcomeTimeout {
				summary.Timeouts++
			}
			if r.Outcome == abOutcomeSuiteBudgetExhausted {
				summary.BudgetExhausted++
			}
		}
		for _, r := range records {
			if r.ArmID != arm.ID || r.Event != abEventAttemptFinished {
				continue
			}
			summary.PromptTokens += r.Metrics.PromptTokens
			summary.CompletionTokens += r.Metrics.CompletionTokens
			summary.CacheHitTokens += r.Metrics.CacheHitTokens
			summary.CacheMissTokens += r.Metrics.CacheMissTokens
			summary.ToolResultsProjected += r.Metrics.ToolResultsProjected
			summary.ProjectionSavedChars += r.Metrics.ProjectionSavedChars
			summary.Cost += r.Metrics.Cost
			if r.Metrics.Currency != "" {
				currencies[r.Metrics.Currency] = true
			}
			if r.Outcome == abOutcomeInfraFailed {
				summary.InfraAttempts++
			}
			if !r.MetricsAvailable {
				summary.MissingMetricsAttempts++
			}
		}
		for currency := range currencies {
			summary.Currency = currency
		}
		if len(currencies) > 1 {
			summary.Currency = "mixed"
		}
		if summary.Passed > 0 && summary.Currency != "mixed" {
			costPerPass := summary.Cost / float64(summary.Passed)
			summary.CostPerPass = &costPerPass
		}
		arms = append(arms, summary)
	}

	return abProjection{
		SchemaVersion: abSchemaVersion,
		Manifest:      manifest,
		Cells:         cells,
		Arms:          arms,
		Paired:        pairedABSummary(manifest, latestMap),
	}
}

func pairedABSummary(manifest abManifest, latest map[abCellKey]abRecord) abPairSummary {
	summary := abPairSummary{TotalPairs: len(manifest.Tasks) * manifest.Repetitions}
	for _, t := range manifest.Tasks {
		for repetition := 1; repetition <= manifest.Repetitions; repetition++ {
			baseline, baselineOK := latest[abCellKey{armID: "baseline", taskID: t.ID, repetition: repetition}]
			candidate, candidateOK := latest[abCellKey{armID: "candidate", taskID: t.ID, repetition: repetition}]
			if !baselineOK || !candidateOK || !baseline.Scored || !candidate.Scored {
				continue
			}
			summary.EligiblePairs++
			switch {
			case baseline.Passed && candidate.Passed:
				summary.BothPass++
			case baseline.Passed:
				summary.BaselineOnly++
			case candidate.Passed:
				summary.CandidateOnly++
			default:
				summary.BothFail++
			}
		}
	}
	if summary.EligiblePairs > 0 {
		delta := 100 * float64(summary.CandidateOnly-summary.BaselineOnly) / float64(summary.EligiblePairs)
		summary.DeltaPP = &delta
		p := exactMcNemarP(summary.BaselineOnly, summary.CandidateOnly)
		summary.McNemarP = &p
	}
	return summary
}

func exactMcNemarP(baselineOnly, candidateOnly int) float64 {
	n := baselineOnly + candidateOnly
	if n == 0 {
		return 1
	}
	kMax := min(baselineOnly, candidateOnly)
	logTwo := math.Log(2)
	sum := 0.0
	for k := 0; k <= kMax; k++ {
		logChoose, _ := math.Lgamma(float64(n + 1))
		logK, _ := math.Lgamma(float64(k + 1))
		logNK, _ := math.Lgamma(float64(n - k + 1))
		sum += math.Exp(logChoose - logK - logNK - float64(n)*logTwo)
	}
	return math.Min(1, 2*sum)
}

func renderABCSV(manifest abManifest, latest map[abCellKey]abRecord) ([]byte, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	header := []string{
		"run_id", "task_id", "repetition",
		"baseline_outcome", "baseline_scored", "baseline_passed", "baseline_attempt", "baseline_metrics_available", "baseline_prompt_tokens", "baseline_completion_tokens", "baseline_cache_hit_tokens", "baseline_cache_miss_tokens", "baseline_tool_results_projected", "baseline_projection_saved_chars", "baseline_cost",
		"candidate_outcome", "candidate_scored", "candidate_passed", "candidate_attempt", "candidate_metrics_available", "candidate_prompt_tokens", "candidate_completion_tokens", "candidate_cache_hit_tokens", "candidate_cache_miss_tokens", "candidate_tool_results_projected", "candidate_projection_saved_chars", "candidate_cost",
	}
	if err := w.Write(header); err != nil {
		return nil, err
	}
	for _, t := range manifest.Tasks {
		for repetition := 1; repetition <= manifest.Repetitions; repetition++ {
			baseline := latest[abCellKey{armID: "baseline", taskID: t.ID, repetition: repetition}]
			candidate := latest[abCellKey{armID: "candidate", taskID: t.ID, repetition: repetition}]
			row := []string{manifest.RunID, t.ID, strconv.Itoa(repetition)}
			row = append(row, abCSVCell(baseline)...)
			row = append(row, abCSVCell(candidate)...)
			if err := w.Write(row); err != nil {
				return nil, err
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

func abCSVCell(r abRecord) []string {
	outcome := r.Outcome
	if r.Event == abEventAdmissionStarted {
		outcome = abEventAdmissionStarted
	}
	if outcome == "" {
		outcome = "pending"
	}
	return []string{
		outcome, strconv.FormatBool(r.Scored), strconv.FormatBool(r.Passed), strconv.Itoa(r.Attempt), strconv.FormatBool(r.MetricsAvailable),
		strconv.Itoa(r.Metrics.PromptTokens), strconv.Itoa(r.Metrics.CompletionTokens),
		strconv.Itoa(r.Metrics.CacheHitTokens), strconv.Itoa(r.Metrics.CacheMissTokens),
		strconv.Itoa(r.Metrics.ToolResultsProjected), strconv.Itoa(r.Metrics.ProjectionSavedChars),
		strconv.FormatFloat(r.Metrics.Cost, 'f', 8, 64),
	}
}

func renderABReport(p abProjection) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Reasonix Harness A/B — `%s`\n\n", p.Manifest.RunID)
	status := "partial"
	terminal := 0
	for _, cell := range p.Cells {
		if cell.Event == abEventAttemptFinished && (cell.Scored || cell.Attempt >= 1+p.Manifest.InfraRetries) {
			terminal++
		}
	}
	if terminal == p.Paired.TotalPairs*2 {
		status = "complete"
	}
	fmt.Fprintf(&b, "**Status:** %s · **Model:** `%s` · **Environment:** `%s` · **Paired cells:** %d/%d · **Repetitions:** %d\n\n",
		status, emptyAsDefault(p.Manifest.Model), emptyAsDefault(p.Manifest.EnvironmentID), p.Paired.EligiblePairs, p.Paired.TotalPairs, p.Manifest.Repetitions)

	fmt.Fprintf(&b, "## Arm summary\n\n")
	fmt.Fprintf(&b, "| Arm | Binary | Profile | Accuracy | Cache hit | Projections / chars saved | Tokens | Cost | Cost/pass | Timeout | Infra attempts | Metrics gaps |\n")
	fmt.Fprintf(&b, "|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for i, arm := range p.Manifest.Arms {
		s := p.Arms[i]
		costPerPass := "n/a"
		if s.CostPerPass != nil {
			costPerPass = fmt.Sprintf("%s%.4f", currencySym(s.Currency), *s.CostPerPass)
		}
		cost := fmt.Sprintf("%s%.4f", currencySym(s.Currency), s.Cost)
		if s.Currency == "mixed" {
			cost = "mixed currencies"
		}
		fmt.Fprintf(&b, "| `%s` | `%s` (`%s`) | `%s` | %d/%d (%s) | %s | %d / %s | %s | %s | %s | %d | %d | %d |\n",
			arm.ID, arm.Binary, shortHash(arm.BinarySHA256), arm.Profile,
			s.Passed, s.Scored, pct(s.Passed, s.Scored), pct(s.CacheHitTokens, s.CacheHitTokens+s.CacheMissTokens),
			s.ToolResultsProjected, comma(s.ProjectionSavedChars), comma(s.PromptTokens+s.CompletionTokens), cost, costPerPass, s.Timeouts, s.InfraAttempts, s.MissingMetricsAttempts)
	}

	fmt.Fprintf(&b, "\n## Paired result\n\n")
	fmt.Fprintf(&b, "| Both pass | Baseline only | Candidate only | Both fail | Candidate Δ | Exact McNemar p |\n")
	fmt.Fprintf(&b, "|---:|---:|---:|---:|---:|---:|\n")
	delta, pValue := "n/a", "n/a"
	if p.Paired.DeltaPP != nil {
		delta = fmt.Sprintf("%+.2f pp", *p.Paired.DeltaPP)
	}
	if p.Paired.McNemarP != nil {
		pValue = fmt.Sprintf("%.6f", *p.Paired.McNemarP)
	}
	fmt.Fprintf(&b, "| %d | %d | %d | %d | %s | %s |\n",
		p.Paired.BothPass, p.Paired.BaselineOnly, p.Paired.CandidateOnly, p.Paired.BothFail, delta, pValue)

	fmt.Fprintf(&b, "\n## Per-task results\n\n")
	fmt.Fprintf(&b, "| Task | Rep | Baseline | Candidate |\n")
	fmt.Fprintf(&b, "|---|---:|---|---|\n")
	latest := make(map[abCellKey]abRecord, len(p.Cells))
	for _, cell := range p.Cells {
		latest[abCellKey{armID: cell.ArmID, taskID: cell.TaskID, repetition: cell.Repetition}] = cell
	}
	for _, task := range p.Manifest.Tasks {
		for repetition := 1; repetition <= p.Manifest.Repetitions; repetition++ {
			baseline := latest[abCellKey{armID: "baseline", taskID: task.ID, repetition: repetition}]
			candidate := latest[abCellKey{armID: "candidate", taskID: task.ID, repetition: repetition}]
			fmt.Fprintf(&b, "| `%s` | %d | %s | %s |\n", task.ID, repetition, renderABCell(baseline), renderABCell(candidate))
		}
	}

	fmt.Fprintf(&b, "\n<sub>Accuracy uses terminal scored cells. Infrastructure failures are excluded from paired statistics; attempts with available metrics still count toward token and cost totals. Metrics gaps make those totals lower bounds. The exact two-sided McNemar test uses discordant pairs only. Manifest SHA-256: `%s`.</sub>\n",
		shortHash(p.Manifest.Suite.SHA256))
	return b.String()
}

func renderABCell(r abRecord) string {
	if r.Event == abEventAdmissionStarted {
		return fmt.Sprintf("⏳ %s (attempt %d)", abEventAdmissionStarted, r.Attempt)
	}
	if r.Outcome == "" {
		return "⏳ pending"
	}
	if !r.Scored {
		return fmt.Sprintf("⚠️ %s (attempt %d)", r.Outcome, r.Attempt)
	}
	icon := "❌"
	if r.Passed {
		icon = "✅"
	}
	return fmt.Sprintf("%s %s", icon, r.Outcome)
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func emptyAsDefault(value string) string {
	if value == "" {
		return "config default"
	}
	return value
}
