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
	// NoSolution declares that no reachable solution exists: the task leaves
	// every accuracy denominator and is scored on honesty instead, and its
	// verify.sh grades the inverse contract. See benchmarks/README.md.
	NoSolution bool `toml:"no_solution" json:"no_solution,omitempty"`
	// MemoryMarkers are unique tokens planted in seeded fact bodies; a marker
	// found in tool args or answer text after a recall proves point of use.
	MemoryMarkers []string `toml:"memory_markers" json:"memory_markers,omitempty"`
	// MemoryMarkersPrefix marks tasks whose seeded facts are pinned: their
	// bodies arrive via the stable prefix, so markers count from turn one.
	MemoryMarkersPrefix bool `toml:"memory_markers_prefix" json:"memory_markers_prefix,omitempty"`
	// SeedCorrect and SeedWrong are the hypotheses the -anchor arms hand the
	// agent before it starts: the task's real cause, and a plausible one that
	// is not. Only tasks carrying both can be scored for anchor resistance.
	SeedCorrect string `toml:"seed_correct" json:"-"`
	SeedWrong   string `toml:"seed_wrong" json:"-"`
	dir         string
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

	// Delegation counters mirror internal/cli.RunMetrics. They are what makes a
	// single-agent arm comparable against a delegated one for the same model.
	SubagentRuns          int `json:"subagent_runs,omitempty"`
	SubagentNestedRuns    int `json:"subagent_nested_runs,omitempty"`
	SubagentMutations     int `json:"subagent_mutations,omitempty"`
	CompletionReports     int `json:"completion_reports,omitempty"`
	CompletionsProsedOnly int `json:"completions_prose_only,omitempty"`
	FalseCompletions      int `json:"false_completions,omitempty"`
	CriterionDowngrades   int `json:"criterion_downgrades,omitempty"`
	WriteScopeViolations  int `json:"write_scope_violations,omitempty"`
	DuplicateWorkPaths    int `json:"duplicate_work_paths,omitempty"`
	// Evidence origin: what the parent's own delegation text scoped and named,
	// and how much of what the children looked at they had to find themselves.
	ParentScopeHints     int `json:"parent_scope_hints,omitempty"`
	ParentNamedFiles     int `json:"parent_named_files,omitempty"`
	ChildEvidencePaths   int `json:"child_evidence_paths,omitempty"`
	ChildDiscoveredPaths int `json:"child_discovered_paths,omitempty"`
	// Optional capability counters (omitempty for older metrics).
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
	// Profile is retained for readers of older benchmark JSON. New runs always
	// record "standard" because execution mode is no longer an experiment axis.
	Profile string `json:"profile"`
	// Arm is the ablation arm the harness requested, not the arm the child
	// reported, so a run that died before writing metrics is still attributable.
	Arm     string `json:"arm"`
	Passed  bool
	Skipped bool
	Note    string
	// Memory shadow: recall decisions and point-of-use evidence extracted from
	// the trajectory (see memorybench.go). Zero for suites without seeds.
	MemoryRecallEvents int `json:"memory_recall_events,omitempty"`
	MemoryRecallHits   int `json:"memory_recall_hits,omitempty"`
	MemoryRecallChars  int `json:"memory_recall_chars,omitempty"`
	MemorySuppressed   int `json:"memory_suppressed,omitempty"`
	MemoryMarkersUsed  int `json:"memory_markers_used,omitempty"`
	MemoryShadowAgree  int `json:"memory_shadow_agree,omitempty"`
	// WallMs is the harness's own clock, not the agent's self-report, so the
	// number stays comparable when the same suite runs against another harness.
	WallMs int64 `json:"wall_ms"`
	// Unaccounted marks a run whose metrics file never landed — a killed agent
	// writes nothing. Its real cost is unknown, so it is kept out of the cost
	// and token aggregates instead of being averaged in as zero, which would
	// quietly understate every published per-task figure.
	Unaccounted bool `json:"unaccounted"`
	// Segments is how many resumed legs the run was split into (1 = a single
	// leg). Above 1 the trajectory digest covers only the last leg.
	Segments int `json:"segments,omitempty"`
	// Partial marks accounting recovered from an in-flight snapshot after the
	// agent was killed. The numbers are real but stop at the last snapshot, so
	// they are counted as lower bounds rather than dropped.
	Partial bool `json:"partial"`
	// Meter is what the neutral proxy observed for this run, when metering was
	// on. It is the authority for cross-harness spend; runMetrics is the
	// harness's own account, kept only to be checked against it.
	Meter *meterUsage `json:"meter,omitempty"`
	// Trajectory is the digest of the run's recorded event trajectory; nil
	// unless the harness ran with -trajectories.
	Trajectory *trajectorySummary `json:"trajectory,omitempty"`
	// PlanForced marks a -force-planner run: the prompt carried an injected
	// plan-first directive, so arms are only comparable with equal forcing.
	PlanForced bool `json:"plan_forced,omitempty"`
	// Anchor is the hypothesis arm the prompt carried (blind | correct |
	// wrong). Runs are only comparable within one arm.
	Anchor string `json:"anchor,omitempty"`
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
	}

	mode := flag.String("mode", "suite", "suite | diff | swebench | compare | traj | serve | fork")
	addr := flag.String("addr", "127.0.0.1:7480", "serve mode: live dashboard listen address")
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
	pressure := registerPressureFlags()
	policyFlag := flag.String("policy", "", "suite mode: experiment arm — empty (baseline) | ebm (evidence-before-more-mutation nudge) | governor (exploration-phase reasoning governor) | memory-off (hide the memory store: MemoryBench counterfactual arm)")
	forkCapture := flag.String("fork-capture", "", "suite mode: capture a fork bundle per task at first EBM eligibility into <dir>/<task-id>")
	bundles := flag.String("bundles", "", "fork mode: directory of captured bundles (<task-id>/bundle.json)")
	forkArms := flag.String("arm", "control,treatment", "fork mode: comma-separated continuation arms (control | treatment)")
	forkReps := flag.Int("reps", 1, "fork mode: continuation repetitions per bundle per arm")
	bin := flag.String("bin", "reasonix", "path to the reasonix binary")
	model := flag.String("model", "", "provider/model name (default: config default)")
	ablateFlag := flag.String("ablate", "", "ablation arm: subsystems to switch off (evidence, planner, subagent, retrieval, compaction; none|all)")
	outMD := flag.String("out", "", "write the markdown report here (default: stdout)")
	trajDir := flag.String("trajectories", "", "suite mode: write one <task-id>.trajectory.jsonl per task into this directory")
	forcePlanner := flag.Bool("force-planner", false, "suite mode: prefix each prompt with a plan-first directive so the two-model turn engages regardless of the planner gate")
	anchorFlag := flag.String("anchor", anchorBlind, "suite mode: hypothesis arm — blind (no hypothesis) | correct | wrong; correct/wrong prefix each prompt with the task's authored seed and skip tasks that have none")
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
	axes, err := resolveExperimentAxes(*ablateFlag, *cacheArm, *anchorFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	arm, cache, anchor := axes.arm, axes.cache, axes.anchor

	if *mode == "swebench" {
		if _, err := permissionFlag(*permission); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		cwd, _ := os.Getwd()
		report := runSwebench(swebenchOpts{
			bin: *bin, subset: *subset, namespace: *namespace, model: *model,
			permission: *permission, arm: arm, runID: *runID, workDir: cwd,
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
	case "serve":
		if err := runServeMode(*trajDir, *suite, *addr); err != nil {
			fmt.Fprintln(os.Stderr, "serve mode:", err)
			os.Exit(1)
		}
		return
	case "fork":
		cfg := suiteConfig{bin: *bin, model: *model, arm: arm,
			cacheArm: cache, effort: *effort, policy: *policyFlag}
		if err := runForkMode(*bundles, *suite, *forkArms, *forkReps, cfg, *trajDir, *outMD, *outJSON); err != nil {
			fmt.Fprintln(os.Stderr, "fork mode:", err)
			os.Exit(1)
		}
		return
	}

	if *mode == "diff" {
		report := runDiff(diffOpts{
			bin: *bin, model: *model, repo: *repo, base: *base,
			testCmd: *testCmd, ablate: arm, maxSteps: *maxSteps, timeoutSec: *timeoutSec, attempts: *attempts,
		})
		emit(report, *outMD, "")
		return
	}

	meterSource, faults, segments, steers := pressure.settings()
	runSuiteMode(suiteConfig{
		bin: *bin, model: *model, arm: arm, budget: *budget,
		trajDir: *trajDir, forcePlanner: *forcePlanner, attempts: *attempts, anchor: anchor,
		cacheArm: cache, effort: *effort, checkpoints: *checkpoints, policy: *policyFlag,
		forkCapture: *forkCapture, meterConfig: meterSource, meterFaults: faults, segments: segments, steers: steers,
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
// model, ablation arm, cache arm, and reasoning effort.
type suiteConfig struct {
	bin, model, cacheArm, effort string
	arm                          ablation.Set
	anchor                       string
	policy, forkCapture          string
	trajDir                      string
	forcePlanner, checkpoints    bool
	attempts, budget             int
	// meterConfig is the real config.toml whose provider endpoint each run is
	// redirected through the neutral meter; empty leaves runs unmetered.
	meterConfig string
	meterFaults faultScript
	// segments splits each task into that many resumed legs; steers delivers a
	// user turn at a leg boundary. Both are LongRun pressure, not defaults.
	segments int
	steers   map[int]string
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
			results = append(results, result{task: t, Profile: benchmarkProfileStandard, Skipped: true, Note: "skipped: token budget reached"})
			continue
		}
		if skipped, ok := anchorSkip(cfg, t); ok {
			results = append(results, skipped)
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
	r := result{task: t, Profile: benchmarkProfileStandard, CacheArm: cfg.cacheArm, Effort: cfg.effort}
	r.Arm = cfg.arm.Arm()
	r.Anchor = cfg.anchor
	t.Prompt = anchorPrompt(cfg.anchor, t)
	if cfg.forcePlanner {
		// Leading directive matched by the planner gate's
		// planAndExecuteDirectives, so the two-model turn engages even for
		// prompts the gate would route ExecutorOnly.
		t.Prompt = "Plan first, then implement the following task.\n\n" + t.Prompt
		r.PlanForced = true
	}

	// The per-leg file names are decided in runSegments; only the directory has
	// to exist before the first child starts, and the digest below reads the
	// last leg's file.
	trajPath := ""
	if cfg.trajDir != "" {
		if err := os.MkdirAll(cfg.trajDir, 0o755); err != nil {
			r.Note = "trajectory dir: " + err.Error()
			return r
		}
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

	extraEnv, seedNote := taskExperimentEnv(cfg, t, work)
	if seedNote != "" {
		r.Note = seedNote
	}
	mtr := attachMeter(cfg, &r)
	defer mtr.close()
	extraEnv = append(extraEnv, mtr.env...)
	startedAt := time.Now()
	snap, dropSnapshots := attachSnapshotter(cfg, t, work, startedAt)
	defer dropSnapshots()
	runErr := runSegments(ctx, cfg, t, work, cfg.trajDir, extraEnv, &r)
	r.WallMs = time.Since(startedAt).Milliseconds()
	var taken []checkpoint
	if snap != nil {
		taken = snap.halt()
	}

	mtr.record(&r)
	if trajPath = lastSegmentTrajectory(cfg.trajDir, t.ID, r.Segments); trajPath != "" {
		if summary, err := summarizeTrajectory(trajPath); err == nil {
			r.Trajectory = summary
		}
		applyMemoryStats(&r, trajPath, t)
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

	var graderSaid string
	r.Passed, graderSaid = gradeVerbose(work, t.dir)
	if !r.Passed && graderSaid != "" {
		r.Note = appendNote(r.Note, "grader: "+utf8Prefix(graderSaid, graderNoteLimit))
	}
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

// buildSegmentArgs is buildRunTaskArgs for one leg: a resumed leg adds
// --continue, which is unambiguous because each task runs in its own home and
// therefore its own session directory.
func buildSegmentArgs(cfg suiteConfig, seg segment, metricsPath, trajectoryPath string) []string {
	args := buildRunTaskArgs(cfg, metricsPath, trajectoryPath, seg.maxSteps, seg.prompt)
	if !seg.resume {
		return args
	}
	// The prompt is the last argument; --continue must precede it.
	return append(args[:len(args)-1:len(args)-1], "--continue", args[len(args)-1])
}

// warmPrefix primes the provider prefix cache for work's session shape with a
// minimal one-step run before the graded run starts its clock. Its cost is
// deliberately untracked: the warm arm measures a long-lived session's steady
// state, not the price of reaching it. Prefix-shaping flags (model, effort,
// ablation, cwd) must match the graded invocation exactly.
func warmPrefix(cfg suiteConfig, work string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	args := []string{"run", "--auto", "--max-steps", "1"}
	if cfg.model != "" {
		args = append(args, "--model", cfg.model)
	}
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
