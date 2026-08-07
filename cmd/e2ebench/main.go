// e2ebench runs the committed e2e task suite against a real provider and emits a
// markdown + JSON report (accuracy, cache-hit rate, token use, cost) for a PR.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"reasonix/internal/ablation"
	fileencoding "reasonix/internal/fileutil/encoding"
)

type task struct {
	ID         string
	Prompt     string `toml:"prompt"`
	MaxSteps   int    `toml:"max_steps"`
	TimeoutSec int    `toml:"timeout_sec"`
	dir        string
}

type runMetrics struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CacheHitTokens   int `json:"cache_hit_tokens"`
	CacheMissTokens  int `json:"cache_miss_tokens"`
	// PrefixChangeReasonCounts mirrors internal/cli.RunMetrics's field of the
	// same name: per-run tallies of why the cache prefix changed (e.g.
	// "compact_auto", "snip", "tools"), omitempty for older metrics files.
	PrefixChangeReasonCounts map[string]int `json:"prefix_change_reason_counts,omitempty"`
	Steps                    int            `json:"steps"`
	Cost                     float64        `json:"cost"`
	Currency                 string         `json:"currency"`
	Compactions              int            `json:"compactions"`

	// Optional Delivery capability counters (omitempty for baseline/old metrics).
	ReadinessChecks            int     `json:"readiness_checks,omitempty"`
	ReadinessRecoveries        int     `json:"readiness_recoveries,omitempty"`
	CapabilityRoutes           int     `json:"capability_routes,omitempty"`
	CapabilityRoutedCandidates int     `json:"capability_routed_candidates,omitempty"`
	CapabilityRoutedRequire    int     `json:"capability_routed_require,omitempty"`
	CapabilityRoutedPrefer     int     `json:"capability_routed_prefer,omitempty"`
	CapabilityRoutedSuggest    int     `json:"capability_routed_suggest,omitempty"`
	CapabilityDeclines         int     `json:"capability_declines,omitempty"`
	CapabilitySemanticRoutes   int     `json:"capability_semantic_routes,omitempty"`
	CapabilitySkillInvocations int     `json:"capability_skill_invocations,omitempty"`
	CapabilityMCPCall          int     `json:"capability_mcp_call,omitempty"`
	CapabilityReviewBlocks     int     `json:"capability_review_blocks,omitempty"`
	CapabilityRouterCost       float64 `json:"capability_router_cost,omitempty"`
	CapabilityRouterLatencyMs  int64   `json:"capability_router_latency_ms,omitempty"`

	Complete           bool           `json:"complete"`
	Outcome            string         `json:"outcome,omitempty"`
	ToolCalls          int            `json:"tool_calls,omitempty"`
	ToolFailures       int            `json:"tool_failures,omitempty"`
	SubagentToolCalls  int            `json:"subagent_tool_calls,omitempty"`
	Retries            int            `json:"retries,omitempty"`
	ToolCallsByName    map[string]int `json:"tool_calls_by_name,omitempty"`
	ToolFailuresByName map[string]int `json:"tool_failures_by_name,omitempty"`
}

type result struct {
	task
	runMetrics
	Profile string `json:"profile"`
	// Arm is the ablation arm the harness requested, not the arm the child
	// reported, so a run that died before writing metrics is still attributable.
	Arm     string `json:"arm"`
	Passed  bool
	Skipped bool
	Note    string
	// WallMs is the harness's own clock, not the agent's self-report, so the
	// number stays comparable when the same suite runs against another harness.
	WallMs int64 `json:"wall_ms"`
	// Unaccounted marks a run whose metrics file never landed — a killed agent
	// writes nothing. Its real cost is unknown, so it is kept out of the cost
	// and token aggregates instead of being averaged in as zero, which would
	// quietly understate every published per-task figure.
	Unaccounted bool `json:"unaccounted"`
	// Partial marks accounting recovered from an in-flight snapshot after the
	// agent was killed. The numbers are real but stop at the last snapshot, so
	// they are counted as lower bounds rather than dropped.
	Partial bool `json:"partial"`
	// Trajectory is the digest of the run's recorded event trajectory; nil
	// unless the harness ran with -trajectories.
	Trajectory *trajectorySummary `json:"trajectory,omitempty"`
}

// class is the published failure taxonomy: solved, the guard that stopped the
// run, or wrong_patch when the agent finished cleanly and the grader still
// failed. outcome carries the agent's own classification when it wrote metrics.
func (r result) class() string {
	switch {
	case r.Skipped:
		return "skipped"
	case r.Passed:
		return "solved"
	case r.Outcome != "" && r.Outcome != "success":
		return r.Outcome
	case r.Outcome == "":
		return "no_metrics"
	default:
		return "wrong_patch"
	}
}

const defaultSuiteTokenBudget = 800_000

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "e2ebench — Reasonix end-to-end benchmark.\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", flag.CommandLine.Name())
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), "\nExamples:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  # Run the committed suite:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  %[1]s\n\n", strings.Replace(flag.CommandLine.Name(), "e2ebench", "go run ./cmd/e2ebench", 1))
		fmt.Fprintf(flag.CommandLine.Output(), "  # Grade a PR's diff with a retry budget:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  %[1]s -mode diff -base origin/main-v2 -repo . -attempts 3 -timeout 1800\n", strings.Replace(flag.CommandLine.Name(), "e2ebench", "go run ./cmd/e2ebench", 1))
		fmt.Fprintf(flag.CommandLine.Output(), "\n  # Run the same suite with the delivery contract:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  %[1]s -profile delivery\n", strings.Replace(flag.CommandLine.Name(), "e2ebench", "go run ./cmd/e2ebench", 1))
	}

	mode := flag.String("mode", "suite", "suite | diff | swebench")
	subset := flag.String("subset", "benchmarks/swebench/subset.json", "swebench mode: instance subset file")
	namespace := flag.String("namespace", "swebench", "swebench mode: registry namespace holding the evaluation images")
	runID := flag.String("run-id", "reasonix", "swebench mode: run id passed to the official harness")
	harnessPy := flag.String("harness-python", "python3", "swebench mode: interpreter with the swebench package installed")
	dataset := flag.String("dataset", "princeton-nlp/SWE-bench_Verified", "swebench mode: dataset name")
	permission := flag.String("permission", "auto", "swebench mode: agent permission posture (auto | yolo)")
	network := flag.String("network", "", "swebench mode: docker network for agent containers; must have no off-box route")
	proxyURL := flag.String("proxy", "", "swebench mode: the only egress the agent gets, expected to allowlist just the model API")
	workers := flag.Int("workers", 4, "swebench mode: parallel grader workers")
	keepImages := flag.Bool("keep-images", false, "swebench mode: keep instance images instead of removing them after each run")
	suite := flag.String("suite", "benchmarks/e2e", "suite root (contains tasks/<id>/)")
	bin := flag.String("bin", "reasonix", "path to the reasonix binary")
	model := flag.String("model", "", "provider/model name (default: config default)")
	profileFlag := flag.String("profile", benchmarkProfileBaseline, "prompt profile: baseline | delivery")
	ablateFlag := flag.String("ablate", "", "ablation arm: subsystems to switch off (evidence, planner, subagent, retrieval, compaction; none|all)")
	outMD := flag.String("out", "", "write the markdown report here (default: stdout)")
	trajDir := flag.String("trajectories", "", "suite mode: write one <task-id>.trajectory.jsonl per task into this directory")
	outJSON := flag.String("json", "", "write the JSON report here (optional)")
	budget := flag.Int("budget", defaultSuiteTokenBudget, "abort once total tokens cross this (0 = no cap)")
	// diff-mode flags
	repo := flag.String("repo", ".", "repo root (diff mode)")
	base := flag.String("base", "", "base ref to diff the PR head against (diff mode)")
	testCmd := flag.String("test-cmd", "go test", "grader command run on the affected packages (diff mode)")
	maxSteps := flag.Int("max-steps", 80, "agent tool-call cap for the diff task")
	timeoutSec := flag.Int("timeout", 1200, "agent timeout in seconds (diff mode)")
	attempts := flag.Int("attempts", 1, "diff mode: retry up to N times until a run passes (stochastic agent)")
	flag.Parse()
	profile, err := normalizeBenchmarkProfile(*profileFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	arm, err := ablation.Parse(*ablateFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if *mode == "swebench" {
		if _, err := permissionFlag(*permission); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		cwd, _ := os.Getwd()
		report := runSwebench(swebenchOpts{
			bin: *bin, subset: *subset, namespace: *namespace, model: *model,
			profile: profile, permission: *permission, arm: arm, runID: *runID, workDir: cwd,
			harness: *harnessPy, dataset: *dataset, maxSteps: *maxSteps,
			timeoutSec: *timeoutSec, workers: *workers, keepImages: *keepImages,
			network: *network, proxyURL: *proxyURL,
		})
		emit(report, *outMD, "")
		if *outJSON != "" {
			fmt.Fprintln(os.Stderr, "note: -json is not written in swebench mode; the harness report is authoritative")
		}
		return
	}

	if *mode == "diff" {
		report := runDiff(diffOpts{
			bin: *bin, model: *model, repo: *repo, base: *base,
			testCmd: *testCmd, profile: profile, ablate: arm, maxSteps: *maxSteps, timeoutSec: *timeoutSec, attempts: *attempts,
		})
		emit(report, *outMD, "")
		return
	}

	tasks, err := loadTasks(*suite)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load suite:", err)
		os.Exit(1)
	}
	if len(tasks) == 0 {
		dir := filepath.Join(*suite, "tasks")
		if _, statErr := os.Stat(dir); statErr != nil {
			fmt.Fprintf(os.Stderr, "no tasks found under %s: %v\n", dir, statErr)
		} else {
			fmt.Fprintf(os.Stderr, "no tasks found under %s (the directory exists but contains no task.toml files)\n", dir)
		}
		os.Exit(1)
	}

	results := runSuite(*bin, *model, profile, arm, tasks, *budget, *trajDir)

	report := render(results)
	if *outMD != "" {
		if err := os.WriteFile(*outMD, []byte(report), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write report:", err)
			os.Exit(1)
		}
	} else {
		fmt.Print(report)
	}
	if *outJSON != "" {
		b, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "marshal json:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*outJSON, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write json:", err)
			os.Exit(1)
		}
	}
}

func emit(report, outMD, _ string) {
	if outMD != "" {
		if err := os.WriteFile(outMD, []byte(report), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write report:", err)
			os.Exit(1)
		}
		return
	}
	fmt.Print(report)
}

func loadTasks(suite string) ([]task, error) {
	tasksDir := filepath.Join(suite, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil, err
	}
	var tasks []task
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(tasksDir, e.Name())
		var t task
		data, err := fileencoding.ReadFileUTF8(filepath.Join(dir, "task.toml"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if _, err := toml.Decode(string(data), &t); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		t.ID = e.Name()
		t.dir = dir
		if t.TimeoutSec == 0 {
			t.TimeoutSec = 240
		}
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

// runSuite runs each task in order until the token budget is exhausted;
// remaining tasks are reported as skipped rather than silently dropped.
func runSuite(bin, model, profile string, arm ablation.Set, tasks []task, budget int, trajDir string) []result {
	var results []result
	total := 0
	for _, t := range tasks {
		if budget > 0 && total >= budget {
			results = append(results, result{task: t, Profile: profile, Skipped: true, Note: "skipped: token budget reached"})
			continue
		}
		r := runTask(bin, model, profile, arm, t, trajDir)
		total += r.PromptTokens + r.CompletionTokens
		results = append(results, r)
	}
	return results
}

// runTask copies the task's seed workdir into a temp dir, runs the agent there,
// then drops in verify.sh and runs it as the grader. The grader is added only
// after the run so the agent can't read the answer key.
func runTask(bin, model, profile string, arm ablation.Set, t task, trajDir string) result {
	r := result{task: t, Profile: profile}
	r.Arm = arm.Arm()

	trajPath := ""
	if trajDir != "" {
		if err := os.MkdirAll(trajDir, 0o755); err != nil {
			r.Note = "trajectory dir: " + err.Error()
			return r
		}
		trajPath = filepath.Join(trajDir, t.ID+".trajectory.jsonl")
	}

	work, err := os.MkdirTemp("", "e2ebench-"+t.ID+"-")
	if err != nil {
		r.Note = "mktemp: " + err.Error()
		return r
	}
	defer os.RemoveAll(work)

	if seed := filepath.Join(t.dir, "workdir"); dirExists(seed) {
		if err := copyDir(seed, work); err != nil {
			r.Note = "copy seed: " + err.Error()
			return r
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(t.TimeoutSec)*time.Second)
	defer cancel()

	metricsPath := filepath.Join(work, ".run-metrics.json")
	args := buildRunTaskArgs(metricsPath, trajPath, model, profile, arm, t.MaxSteps, t.Prompt)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = work
	cmd.Stdout = os.Stderr // stream the run to the job log, keep stdout clean for the report
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 10 * time.Second // bound the wait for a stuck child after ctx timeout
	startedAt := time.Now()
	runErr := cmd.Run()
	r.WallMs = time.Since(startedAt).Milliseconds()

	if m, err := readMetrics(metricsPath); err == nil {
		r.runMetrics = m
	}
	if trajPath != "" {
		if summary, err := summarizeTrajectory(trajPath); err == nil {
			r.Trajectory = summary
		}
	}
	// A killed child never writes metrics, so the deadline is the only place
	// this failure mode is still observable.
	if ctx.Err() == context.DeadlineExceeded {
		r.Outcome = "timeout"
	}
	if runErr != nil {
		r.Note = "run: " + runErr.Error()
		// still grade — a non-zero exit may just be a max-steps notice
	}

	r.Passed = grade(work, t.dir)
	return r
}

func buildRunTaskArgs(metricsPath, trajectoryPath, model, profile string, arm ablation.Set, maxSteps int, prompt string) []string {
	// Benchmarks are unattended and their fixtures require ordinary workspace
	// writes. Auto still honors explicit ask/deny rules and the sandbox boundary.
	args := []string{"run", "--auto", "--metrics", metricsPath}
	if trajectoryPath != "" {
		args = append(args, "--trajectory", trajectoryPath)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if maxSteps > 0 {
		args = append(args, "--max-steps", fmt.Sprint(maxSteps))
	}
	args = appendBenchmarkProfileArgs(args, profile)
	// The control arm must produce a byte-identical command line to the one the
	// suite ran before ablation existed, so its numbers stay comparable.
	if !arm.Empty() {
		args = append(args, "--ablate", arm.String())
	}
	return append(args, prompt)
}

func grade(work, taskDir string) bool {
	verify := filepath.Join(taskDir, "verify.sh")
	if !fileExists(verify) {
		return false
	}
	dst := filepath.Join(work, "verify.sh")
	if err := copyFile(verify, dst); err != nil {
		return false
	}
	cmd := exec.Command("bash", "verify.sh")
	cmd.Dir = work
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run() == nil
}

func render(results []result) string {
	profile := benchmarkProfileBaseline
	arm := "full"
	if len(results) > 0 {
		if results[0].Profile != "" {
			profile = results[0].Profile
		}
		if results[0].Arm != "" {
			arm = results[0].Arm
		}
	}
	return fmt.Sprintf("## 🤖 Reasonix e2e benchmark (%s · arm `%s`)\n\n", profile, arm) + renderBody(results)
}

// renderBody is the report without a heading, so a caller that supplies its own
// (SWE-bench mode) does not stack two titles.
func renderBody(results []result) string {
	var b strings.Builder
	passed, ran := 0, 0
	accounted, accountedSolved, unaccounted, unaccountedSolved, partial := 0, 0, 0, 0, 0
	var pTok, cTok, hit, miss, compacts, tools, toolFails int
	var cost float64
	var walls []int64
	currency := ""
	classes := map[string]int{}
	prefixChangeReasons := map[string]int{}
	for _, r := range results {
		if r.Skipped {
			continue
		}
		ran++
		if r.Passed {
			passed++
		}
		classes[r.class()]++
		walls = append(walls, r.WallMs)
		if r.Unaccounted {
			unaccounted++
			if r.Passed {
				unaccountedSolved++
			}
			continue
		}
		accounted++
		if r.Passed {
			accountedSolved++
		}
		if r.Partial {
			partial++
		}
		pTok += r.PromptTokens
		cTok += r.CompletionTokens
		hit += r.CacheHitTokens
		miss += r.CacheMissTokens
		compacts += r.Compactions
		tools += r.ToolCalls
		toolFails += r.ToolFailures
		cost += r.Cost
		if r.Currency != "" {
			currency = r.Currency
		}
		for reason, n := range r.PrefixChangeReasonCounts {
			prefixChangeReasons[reason] += n
		}
	}

	// Cost and tokens are divided by the solved instances we actually have
	// accounting for. Dividing by every solve would treat a lost metrics file as
	// a free solve and understate the published figure.
	fmt.Fprintf(&b, "**Solved:** %d/%d (%s) · **Cost per solved:** %s · **Tokens per solved:** %s · **Median wall time:** %s\n\n",
		passed, ran, pct(passed, ran),
		costPerSolved(cost, accountedSolved, currency), tokensPerSolved(pTok+cTok, accountedSolved), dur(median(walls)))
	fmt.Fprintf(&b, "**Cache hit:** %s · **Tokens:** %s (prompt %s / completion %s) · **Tool calls:** %s (%s failed) · **Compactions:** %d · **Cost:** %s%.4f\n\n",
		pct(hit, hit+miss), comma(pTok+cTok), comma(pTok), comma(cTok),
		comma(tools), comma(toolFails), compacts, currencySym(currency), cost)
	b.WriteString(renderTimeAttribution(results))
	if unaccounted > 0 {
		fmt.Fprintf(&b, "> **Accounting incomplete for %d of %d instances** (%d of them solved): the agent was killed before it wrote any metrics, so their cost and tokens are unknown. Totals above cover the %d accounted instances only, and per-solved figures divide by the %d accounted solves — the true totals are higher.\n\n",
			unaccounted, ran, unaccountedSolved, accounted, accountedSolved)
	}
	if partial > 0 {
		fmt.Fprintf(&b, "> **%d of %d instances contributed partial accounting**: the agent was killed mid-run and its numbers were recovered from the last in-flight snapshot. What is counted is real but stops at that snapshot, so every total above is a lower bound.\n\n",
			partial, ran)
	}

	fmt.Fprintf(&b, "| Task | Result | Class | Steps | Tools | Time | Prompt | Completion | Cache hit | Cost |\n")
	fmt.Fprintf(&b, "|------|--------|-------|------:|------:|-----:|-------:|-----------:|----------:|-----:|\n")
	for _, r := range results {
		switch {
		case r.Skipped:
			fmt.Fprintf(&b, "| `%s` | ⏭️ skipped | — | — | — | — | — | — | — | — |\n", r.ID)
		default:
			res := "❌ fail"
			if r.Passed {
				res = "✅ pass"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %d | %d | %s | %s | %s | %s | %s%.4f |\n",
				r.ID, res, r.class(), r.Steps, r.ToolCalls, dur(r.WallMs),
				comma(r.PromptTokens), comma(r.CompletionTokens),
				pct(r.CacheHitTokens, r.CacheHitTokens+r.CacheMissTokens),
				currencySym(r.Currency), r.Cost)
		}
	}
	fmt.Fprintf(&b, "\n<sub>Real provider run. Cache-hit %% is cached prompt tokens / total prompt tokens. Wall time is measured by the harness and includes process startup.</sub>\n")

	if breakdown := failureBreakdown(classes); breakdown != "" {
		fmt.Fprintf(&b, "\n**Failures by class:** %s\n", breakdown)
	}
	if breakdown := reasonBreakdown(prefixChangeReasons); breakdown != "" {
		fmt.Fprintf(&b, "\n**Cache resets by cause:** %s\n", breakdown)
	}

	notes := false
	for _, r := range results {
		if r.Note != "" {
			if !notes {
				fmt.Fprintf(&b, "\n<details><summary>Notes</summary>\n\n")
				notes = true
			}
			fmt.Fprintf(&b, "- `%s`: %s\n", r.ID, r.Note)
		}
	}
	if notes {
		fmt.Fprintf(&b, "\n</details>\n")
	}
	return b.String()
}

func pct(n, d int) string {
	if d == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(d))
}

func costPerSolved(cost float64, solved int, currency string) string {
	if solved == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%s%.4f", currencySym(currency), cost/float64(solved))
}

func tokensPerSolved(tokens, solved int) string {
	if solved == 0 {
		return "n/a"
	}
	return comma(tokens / solved)
}

func median(ms []int64) int64 {
	if len(ms) == 0 {
		return 0
	}
	sorted := append([]int64(nil), ms...)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}

func dur(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func failureBreakdown(classes map[string]int) string {
	names := make([]string, 0, len(classes))
	for name := range classes {
		if name != "solved" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s ×%d", name, classes[name]))
	}
	return strings.Join(parts, " · ")
}

// reasonBreakdown renders cache-prefix-change reason counts (compact_auto,
// snip, prune, tools, ...) the same way failureBreakdown renders failure
// classes, so a hit-rate regression in a PR shows which operation caused it.
func reasonBreakdown(reasons map[string]int) string {
	names := make([]string, 0, len(reasons))
	for name := range reasons {
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s ×%d", name, reasons[name]))
	}
	return strings.Join(parts, " · ")
}

func comma(n int) string {
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func currencySym(c string) string {
	if c == "" {
		return ""
	}
	return c + " "
}

func readMetrics(path string) (runMetrics, error) {
	var m runMetrics
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip symlinks so a seed link can't leak a file from outside the seed tree.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	// Mirror the source mode so a seed's read-only / exec bit survives the copy.
	return os.Chmod(dst, info.Mode().Perm())
}
