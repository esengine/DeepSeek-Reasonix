// e2ebench runs the committed e2e task suite against a real provider and emits a
// markdown + JSON report (accuracy, cache-hit rate, token use, cost) for a PR.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"reasonix/internal/ablation"
	fileencoding "reasonix/internal/fileutil/encoding"
)

type task struct {
	ID     string
	Prompt string `toml:"prompt"`
	// Class buckets tasks for marginal-utility comparisons (e.g. "bugfix",
	// "codegen", "exploration"): per-class uplift vs latency is what decides
	// whether a subsystem earns its round-trips for that kind of work.
	Class      string `toml:"class" json:"class,omitempty"`
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
	// UsageBySource mirrors cli.RunMetrics: per-origin model-call accounting,
	// the denominator split behind planner/subagent A/B comparisons.
	UsageBySource map[string]sourceUsage `json:"usage_by_source,omitempty"`
	Steps         int                    `json:"steps"`
	Cost          float64                `json:"cost"`
	Currency      string                 `json:"currency"`
	Compactions   int                    `json:"compactions"`

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

type sourceUsage struct {
	Calls            int     `json:"calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
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
	// PlanForced marks a -force-planner run: the prompt carried an injected
	// plan-first directive, so arms are only comparable with equal forcing.
	PlanForced bool `json:"plan_forced,omitempty"`
	// PhaseTrace is the per-task privacy-safe latency trace (counts and ms
	// only); nil unless the run recorded a trajectory.
	PhaseTrace *phaseTrace `json:"phase_trace,omitempty"`
	// CacheArm records whether the run was cold (fresh session) or warm
	// (prefix pre-warmed in the same workdir), so arms never get mixed.
	CacheArm string `json:"cache_arm,omitempty"`
	// Effort records the reasoning-effort override the arm ran with ("" =
	// model default), the adaptive-reasoning-budget experiment axis.
	Effort string `json:"effort,omitempty"`
	// Attempt is this entry's 1-based try for its task; suite retries stop at
	// the first passing attempt. Zero on skipped entries and old JSON.
	Attempt int `json:"attempt,omitempty"`
	// TTCSMs is the time to correct solution: wall clock summed across this
	// task's attempts up to and including the one that passed. Zero if unsolved.
	TTCSMs int64 `json:"ttcs_ms,omitempty"`
	// Checkpoint grading (-checkpoints): FirstCorrectMs = when the workspace
	// first graded correct (TTFCS); PostSolveWasteMs = the tail worked past
	// it; SolvedThenBroken = a passing state the agent later destroyed.
	Checkpoints      []checkpoint `json:"checkpoints,omitempty"`
	FirstCorrectMs   int64        `json:"first_correct_ms,omitempty"`
	PostSolveWasteMs int64        `json:"post_solve_waste_ms,omitempty"`
	SolvedThenBroken bool         `json:"solved_then_broken,omitempty"`
	// Correct-boundary decomposition: edits and rounds on each side of the
	// first-correct instant, verifications re-run after it, and whether a
	// passing state regressed (PASS→FAIL) even if later repaired.
	MutationsBeforeCorrect int  `json:"mutations_before_correct,omitempty"`
	RoundsBeforeCorrect    int  `json:"rounds_before_correct,omitempty"`
	RoundsAfterCorrect     int  `json:"rounds_after_correct,omitempty"`
	CallsBeforeCorrect     int  `json:"calls_before_correct,omitempty"`
	CallsAfterCorrect      int  `json:"calls_after_correct,omitempty"`
	VerifyAfterCorrect     int  `json:"verify_after_correct,omitempty"`
	ReviewsAfterCorrect    int  `json:"reviews_after_correct,omitempty"`
	MutationsAfterCorrect  int  `json:"mutations_after_correct,omitempty"`
	RegressedAfterCorrect  bool `json:"regressed_after_correct,omitempty"`
	// StopEval is the counterfactual-stop curve: per-round end-state grades,
	// the earliest stoppable round, and harmful continuations (PASS→FAIL).
	StopEval *stopEval `json:"stop_eval,omitempty"`
	// FirstUsefulMs approximates TTFUM: when part of the final solution first
	// appeared (earliest checkpoint carrying a solution file's final content).
	FirstUsefulMs int64 `json:"first_useful_ms,omitempty"`
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

	mode := flag.String("mode", "suite", "suite | diff | swebench | compare | traj")
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
	taskFilter := flag.String("task", "", "suite mode: run only these comma-separated task IDs (e.g. -task fix-add-bug)")
	cacheArm := flag.String("cache", "cold", "suite mode: cold (fresh session per task) | warm (prefix-warming one-step run in the same workdir before the graded run)")
	effort := flag.String("effort", "", "reasoning effort override passed to the agent (model-specific levels, e.g. disabled|low|high|max); empty = model default")
	checkpoints := flag.Bool("checkpoints", false, "suite mode: snapshot the workdir on every change and grade each snapshot offline after the run, yielding first_correct_ms (TTFCS) and post_solve_waste_ms")
	bin := flag.String("bin", "reasonix", "path to the reasonix binary")
	model := flag.String("model", "", "provider/model name (default: config default)")
	profileFlag := flag.String("profile", benchmarkProfileBaseline, "prompt profile: baseline | delivery")
	ablateFlag := flag.String("ablate", "", "ablation arm: subsystems to switch off (evidence, planner, subagent, retrieval, compaction; none|all)")
	outMD := flag.String("out", "", "write the markdown report here (default: stdout)")
	trajDir := flag.String("trajectories", "", "suite mode: write one <task-id>.trajectory.jsonl per task into this directory")
	forcePlanner := flag.Bool("force-planner", false, "suite mode: prefix each prompt with a plan-first directive so the two-model turn engages regardless of the planner gate")
	outJSON := flag.String("json", "", "write the JSON report here (optional)")
	budget := flag.Int("budget", defaultSuiteTokenBudget, "abort once total tokens cross this (0 = no cap)")
	// diff-mode flags
	repo := flag.String("repo", ".", "repo root (diff mode)")
	base := flag.String("base", "", "base ref to diff the PR head against (diff mode)")
	testCmd := flag.String("test-cmd", "go test", "grader command run on the affected packages (diff mode)")
	maxSteps := flag.Int("max-steps", 80, "agent tool-call cap for the diff task")
	timeoutSec := flag.Int("timeout", 1200, "agent timeout in seconds (diff mode)")
	attempts := flag.Int("attempts", 1, "suite/diff modes: retry a task up to N times until an attempt passes (stochastic agent); enables Pass@≤N")
	flag.Parse()
	profile, perr := normalizeBenchmarkProfile(*profileFlag)
	arm, aerr := ablation.Parse(*ablateFlag)
	cache, cerr := normalizeCacheArm(*cacheArm)
	if err := errors.Join(perr, aerr, cerr); err != nil {
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

	switch *mode {
	case "compare":
		runCompareMode(*outMD)
		return
	case "traj":
		emitTrajMode(*trajDir, *outMD)
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

	runSuiteMode(suiteConfig{
		bin: *bin, model: *model, profile: profile, arm: arm, budget: *budget,
		trajDir: *trajDir, forcePlanner: *forcePlanner, attempts: *attempts,
		cacheArm: cache, effort: *effort, checkpoints: *checkpoints,
	}, *suite, *taskFilter, *outMD, *outJSON)
}

func runSuiteMode(cfg suiteConfig, suite, taskFilter, outMD, outJSON string) {
	tasks, err := loadTasks(suite)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load suite:", err)
		os.Exit(1)
	}
	if len(tasks) == 0 {
		exitNoTasks(suite)
	}
	if tasks, err = filterTasks(tasks, taskFilter); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	results := runSuite(cfg, tasks)

	report := render(results)
	if outMD != "" {
		if err := os.WriteFile(outMD, []byte(report), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write report:", err)
			os.Exit(1)
		}
	} else {
		fmt.Print(report)
	}
	if outJSON == "" {
		return
	}
	b, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal json:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outJSON, b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write json:", err)
		os.Exit(1)
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

func exitNoTasks(suite string) {
	dir := filepath.Join(suite, "tasks")
	if _, statErr := os.Stat(dir); statErr != nil {
		fmt.Fprintf(os.Stderr, "no tasks found under %s: %v\n", dir, statErr)
	} else {
		fmt.Fprintf(os.Stderr, "no tasks found under %s (the directory exists but contains no task.toml files)\n", dir)
	}
	os.Exit(1)
}

// filterTasks narrows the suite to the -task list. Unknown IDs fail loudly
// with the available set — a typo silently running zero tasks would read as
// success.
func filterTasks(tasks []task, filter string) ([]task, error) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return tasks, nil
	}
	byID := make(map[string]task, len(tasks))
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
		ids = append(ids, t.ID)
	}
	var out []task
	for id := range strings.SplitSeq(filter, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		t, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("-task %q: no such task; available: %s", id, strings.Join(ids, ", "))
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("-task %q selected no tasks", filter)
	}
	return out, nil
}

// suiteConfig carries one suite invocation's fixed experiment axes: binary,
// model, tool-surface profile, ablation arm, cache arm, and reasoning effort.
type suiteConfig struct {
	bin, model, profile, cacheArm, effort string
	arm                                   ablation.Set
	trajDir                               string
	forcePlanner, checkpoints             bool
	attempts, budget                      int
}

// runSuite runs each task in order until the token budget is exhausted;
// remaining tasks are reported as skipped rather than silently dropped. Each
// task retries up to attempts times, stopping at the first passing attempt;
// TTCS accumulates the failed attempts' wall too — a solution found on try 3
// took three tries' worth of time to reach.
func runSuite(cfg suiteConfig, tasks []task) []result {
	var results []result
	total := 0
	for _, t := range tasks {
		if cfg.budget > 0 && total >= cfg.budget {
			results = append(results, result{task: t, Profile: cfg.profile, Skipped: true, Note: "skipped: token budget reached"})
			continue
		}
		var cumWallMs int64
		for attempt := 1; attempt <= max(cfg.attempts, 1); attempt++ {
			r := runTask(cfg, t)
			r.Attempt = attempt
			cumWallMs += r.WallMs
			if r.Passed {
				r.TTCSMs = cumWallMs
			}
			total += r.PromptTokens + r.CompletionTokens
			results = append(results, r)
			if r.Passed || (cfg.budget > 0 && total >= cfg.budget) {
				break
			}
		}
	}
	return results
}

// runTask copies the task's seed workdir into a temp dir, runs the agent there,
// then drops in verify.sh and runs it as the grader. The grader is added only
// after the run so the agent can't read the answer key.
func runTask(cfg suiteConfig, t task) result {
	r := result{task: t, Profile: cfg.profile, CacheArm: cfg.cacheArm, Effort: cfg.effort}
	r.Arm = cfg.arm.Arm()
	if cfg.forcePlanner {
		// Leading directive matched by the planner gate's
		// planAndExecuteDirectives, so the two-model turn engages even for
		// prompts the gate would route ExecutorOnly.
		t.Prompt = "Plan first, then implement the following task.\n\n" + t.Prompt
		r.PlanForced = true
	}

	trajPath := ""
	if cfg.trajDir != "" {
		if err := os.MkdirAll(cfg.trajDir, 0o755); err != nil {
			r.Note = "trajectory dir: " + err.Error()
			return r
		}
		trajPath = filepath.Join(cfg.trajDir, t.ID+".trajectory.jsonl")
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

	if cfg.cacheArm == benchmarkCacheWarm {
		warmPrefix(cfg, work)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(t.TimeoutSec)*time.Second)
	defer cancel()

	metricsPath := filepath.Join(work, ".run-metrics.json")
	args := buildRunTaskArgs(cfg, metricsPath, trajPath, t.MaxSteps, t.Prompt)

	cmd := exec.CommandContext(ctx, cfg.bin, args...)
	cmd.Dir = work
	cmd.Stdout = os.Stderr // stream the run to the job log, keep stdout clean for the report
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 10 * time.Second // bound the wait for a stuck child after ctx timeout
	startedAt := time.Now()
	var snap *snapshotter
	if cfg.checkpoints {
		snapDir, err := os.MkdirTemp("", "e2ebench-cp-"+t.ID+"-")
		if err == nil {
			defer os.RemoveAll(snapDir)
			snap = startSnapshotter(work, snapDir, startedAt)
		}
	}
	runErr := cmd.Run()
	r.WallMs = time.Since(startedAt).Milliseconds()
	var taken []checkpoint
	if snap != nil {
		taken = snap.halt()
	}

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
	if snap != nil {
		r.Checkpoints = gradeCheckpoints(taken, t.dir)
		r.FirstCorrectMs, r.SolvedThenBroken = firstCorrect(r.Checkpoints, r.Passed)
		if r.Passed && r.FirstCorrectMs > 0 {
			r.PostSolveWasteMs = r.WallMs - r.FirstCorrectMs
		}
		r.MutationsBeforeCorrect = mutationsBeforeCorrect(r.Checkpoints)
		r.RegressedAfterCorrect = regressedAfterCorrect(r.Checkpoints)
		if trajPath != "" && r.FirstCorrectMs > 0 {
			split := splitAtCorrect(trajPath, startedAt.UnixMilli()+r.FirstCorrectMs)
			r.RoundsBeforeCorrect, r.RoundsAfterCorrect = split.RoundsBefore, split.RoundsAfter
			r.CallsBeforeCorrect, r.CallsAfterCorrect = split.CallsBefore, split.CallsAfter
			r.VerifyAfterCorrect = split.VerifyAfter
			r.ReviewsAfterCorrect, r.MutationsAfterCorrect = split.ReviewsAfter, split.MutationsAfter
		}
		if trajPath != "" {
			var endsElapsed []int64
			for _, end := range roundEnds(trajPath) {
				endsElapsed = append(endsElapsed, end-startedAt.UnixMilli())
			}
			r.StopEval = computeStopEval(r.Checkpoints, endsElapsed)
		}
		r.FirstUsefulMs = firstUsefulMutation(r.Checkpoints, filepath.Join(t.dir, "workdir"), work)
	}
	r.PhaseTrace = buildPhaseTrace(r)
	return r
}

func buildRunTaskArgs(cfg suiteConfig, metricsPath, trajectoryPath string, maxSteps int, prompt string) []string {
	// Benchmarks are unattended and their fixtures require ordinary workspace
	// writes. Auto still honors explicit ask/deny rules and the sandbox boundary.
	args := []string{"run", "--auto", "--metrics", metricsPath}
	if trajectoryPath != "" {
		args = append(args, "--trajectory", trajectoryPath)
	}
	if cfg.model != "" {
		args = append(args, "--model", cfg.model)
	}
	if maxSteps > 0 {
		args = append(args, "--max-steps", fmt.Sprint(maxSteps))
	}
	args = appendBenchmarkProfileArgs(args, cfg.profile)
	if cfg.effort != "" {
		args = append(args, "--effort", cfg.effort)
	}
	// The control arm must produce a byte-identical command line to the one the
	// suite ran before ablation existed, so its numbers stay comparable.
	if !cfg.arm.Empty() {
		args = append(args, "--ablate", cfg.arm.String())
	}
	return append(args, prompt)
}

// warmPrefix primes the provider prefix cache for work's session shape with a
// minimal one-step run before the graded run starts its clock. Its cost is
// deliberately untracked: the warm arm measures a long-lived session's steady
// state, not the price of reaching it. Prefix-shaping flags (model, profile,
// ablation, cwd) must match the graded invocation exactly.
func warmPrefix(cfg suiteConfig, work string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	args := []string{"run", "--auto", "--max-steps", "1"}
	if cfg.model != "" {
		args = append(args, "--model", cfg.model)
	}
	args = appendBenchmarkProfileArgs(args, cfg.profile)
	if cfg.effort != "" {
		args = append(args, "--effort", cfg.effort)
	}
	if !cfg.arm.Empty() {
		args = append(args, "--ablate", cfg.arm.String())
	}
	args = append(args, "Reply with exactly: ok")
	cmd := exec.CommandContext(ctx, cfg.bin, args...)
	cmd.Dir = work
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "warm-cache pass:", err)
	}
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
