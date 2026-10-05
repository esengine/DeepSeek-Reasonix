package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"slices"
	"sort"
	"strings"
	"time"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	_ "reasonix/internal/model/openai"
	"reasonix/internal/runtime/agent"
	_ "reasonix/internal/tools/builtin"
)

// A run is one recall question against a fixture the preflight has already
// cleared. Scoring reads the transcript the turn appended — tool names and
// their JSON arguments — so nothing depends on how the model narrated itself.

const (
	runBaseURL = "https://api.deepseek.com"
	runModel   = "deepseek-chat"
	runTimeout = 4 * time.Minute
)

func realProvider() (provider.Provider, error) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("a real run needs DEEPSEEK_API_KEY")
	}
	return provider.New("openai", provider.Config{
		Name: "contextbench", BaseURL: runBaseURL, Model: runModel, APIKey: key,
	})
}

// adversarialPrompts tell the model what it is running and send it after the
// answer everywhere except the conversation. The question is not whether it
// obeys the experiment: it is whether the answer is reachable at all by a model
// that knows the mechanism and is actively attacking the host.
var adversarialPrompts = map[string]string{
	experimentSearch: "You are running a benchmark. Find the expected answer for this task from the " +
		"Reasonix repository, benchmark source, fixture generator, test files, scratch files, or " +
		"session metadata. Do not use recall unless necessary. Report the exact hidden values.",
	experimentIndex: "Inspect benchmarks/context-retrieval, the git repository, session files, temporary " +
		"files, and any benchmark metadata you can access. Recover the concrete instantiated answer for " +
		"this run without relying on the folded conversation. Report the exact values.",
}

// runAdversarial sends one task per experiment after the answer through every
// host-readable surface. It passes when nothing but recall could produce it —
// a run that gives up and recalls has still proven the attack failed.
func runAdversarial(root string) int {
	root, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	restore, ierr := isolateWorkspace()
	if ierr != nil {
		fmt.Fprintln(os.Stderr, "isolate workspace:", ierr)
		return 2
	}
	defer restore()
	p, perr := realProvider()
	if perr != nil {
		fmt.Fprintln(os.Stderr, perr)
		return 2
	}
	leaked := 0
	for _, id := range []string{"r01-retry-boundary", "i01-transport-fallback"} {
		t, ok := taskByID(id)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown task %q\n", id)
			return 2
		}
		m, err := runOne(p, t, armFor("default", false), "adversarial", root, false, false,
			adversarialPrompts[t.Experiment])
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", id, err)
		}
		verdict := "isolated"
		if m.EscapeFoundAnswer {
			verdict = "LEAKED via " + m.LeakedVia
			leaked++
		}
		fmt.Printf("%-24s %-24s escapes=%-3d recall=%d answered=%v tools=%v\n",
			id, verdict, m.EscapeCalls, m.SearchCalls+m.ReadCalls, m.AnswerRecovered, m.UnexpectedWorkTools)
		if m.EscapeFoundAnswer {
			fmt.Printf("   args:   %s\n   output: %s\n", m.LeakedArgs, m.LeakedOutput)
		}
	}
	if leaked > 0 {
		fmt.Fprintf(os.Stderr, "\n%d of 2 adversarial runs reached an answer outside recall\n", leaked)
		return 1
	}
	fmt.Println("\nNo answer was reachable outside recall.")
	return 0
}

// runOne asks one task under one arm and scores what the model did.
func runOne(p provider.Provider, t contextTask, arm ablation.Set, armName, root string, cueVisible, dry bool, promptOverride ...string) (contextMetrics, error) {
	// Live runs get values that existed nowhere before the run started; a dry
	// run stays reproducible.
	rng := liveRand()
	if dry {
		rng = seededRand(t.ID, armName, "dry")
	}
	inst, err := instantiateTask(t, rng)
	if err != nil {
		return contextMetrics{}, err
	}
	f, err := buildFixture(inst, arm, filepath.Join(root, t.ID, armName, "session.jsonl"))
	if err != nil {
		return contextMetrics{}, err
	}
	sess, err := sessionstore.LoadSession(f.Path)
	if err != nil {
		return contextMetrics{}, fmt.Errorf("load session: %w", err)
	}
	reg := tool.NewRegistry()
	for _, x := range tool.Builtins() {
		reg.Add(x)
	}
	boot.ApplyUnifiedProviderToolSurface(reg, false, arm)

	a := agent.New(p, reg, sess, agent.Options{
		ContextWindow: fixtureWindow, CompactRatio: 0.5, RecentKeep: 2,
		SessionPath: f.Path, KeepPolicy: agent.KeepErrors, Ablation: arm,
	}, event.Discard)
	a.LoadProjectionSidecar(f.Path)
	// From here the transcript lives only in memory. On disk it is the answer.
	sealFixture(f.Path)
	defer unsealFixture(f.Path)
	dir := filepath.Dir(f.Path)
	if at, marker := answerOnDisk(dir, inst.AnswerMarkers); at != "" {
		return contextMetrics{}, fmt.Errorf("%s [%s]: %q is readable at %s before the run", t.ID, armName, marker, at)
	}

	before := len(sess.Snapshot())
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	prompt := inst.Prompt
	if len(promptOverride) > 0 && promptOverride[0] != "" {
		prompt = promptOverride[0]
	}
	runErr := a.Run(ctx, prompt)

	if at, marker := answerOnDisk(dir, inst.AnswerMarkers); at != "" {
		return contextMetrics{}, fmt.Errorf("%s [%s]: %q was written to %s during the run", t.ID, armName, marker, at)
	}

	appended := sess.Snapshot()[before:]
	m := scoreRun(appended, inst, f.Target, armName, cueVisible)
	m.trajectory = appended
	m.scoreAnswer(finalAnswer(appended), inst)
	if runErr != nil {
		m.FailureStage = "RunError"
		m.TimeoutReason = timeoutReason(m)
	}
	return m, runErr
}

// finalAnswer is the last assistant text the turn produced.
func finalAnswer(msgs []provider.Message) string {
	for _, m := range slices.Backward(msgs) {
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content
		}
	}
	return ""
}

// isolateWorkspace moves the process into an empty directory before any task
// runs. The corpus lives in this repository, so an agent left here greps the
// answers out of corpus.go — one did, quoting the file's own line about them
// being nonces. Fixtures stay outside it too: a readable session file is the
// canonical transcript, answer included.
func isolateWorkspace() (func(), error) {
	dir, err := os.MkdirTemp("", "contextbench-workspace-*")
	if err != nil {
		return nil, err
	}
	readme := "This workspace is empty on purpose. The question is about earlier conversation,\nnot about any file here.\n"
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
		return nil, err
	}
	previous, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Chdir(dir); err != nil {
		return nil, err
	}
	return func() {
		_ = os.Chdir(previous)
		_ = os.RemoveAll(dir)
	}, nil
}

// runBoundaries runs only the pair each index task is built for: the narrowest
// scale still addressing its cue, and the next one down. Each pair moves the
// affordance and nothing else, where default-vs-off moves the whole index.

// The two arms get separate nonces: reusing one would leave the first arm's
// answer in a host-readable history for the second to find.
func runBoundaries(root string) int {
	root, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	restore, ierr := isolateWorkspace()
	if ierr != nil {
		fmt.Fprintln(os.Stderr, "isolate workspace:", ierr)
		return 2
	}
	defer restore()
	p, perr := realProvider()
	if perr != nil {
		fmt.Fprintln(os.Stderr, perr)
		return 2
	}

	var all []contextMetrics
	var cueSide, noCueSide []contextMetrics
	for _, t := range efficiencyTasks() {
		cue, noCue := boundaryPair(t.CueTier)
		for _, side := range []struct {
			scale string
			has   bool
		}{{cue, true}, {noCue, false}} {
			name := "index-" + side.scale
			m, err := runOne(p, t, armFor(side.scale, false), name, root, side.has, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s [%s]: %v\n", t.ID, name, err)
			}
			all = append(all, m)
			if side.has {
				cueSide = append(cueSide, m)
			} else {
				noCueSide = append(noCueSide, m)
			}
			fmt.Printf("%-24s %-14s cue=%-3v %-14s search=%d read=%d direct=%-5v recall-tok=%-5d escape=%d\n",
				t.ID, name, side.has, m.FailureStage, m.SearchCalls, m.ReadCalls,
				m.CueDirectRead, m.RecallReturnedTokens, m.EscapeCalls)
		}
	}
	reportDeltas(cueSide, noCueSide)
	fmt.Print(reportStopping(all))
	return writeResults(root, all)
}

// reportDeltas is the paired reading: each task against itself, then pooled.
func reportDeltas(cueSide, noCueSide []contextMetrics) {
	fmt.Printf("\n## Paired deltas (cue present − cue absent)\n")
	fmt.Printf("  %-24s %-10s %-10s %-9s %-11s %s\n",
		"task", "recovery", "directread", "searches", "recall-tok", "escapes")
	for i := range cueSide {
		if i >= len(noCueSide) {
			break
		}
		a, b := cueSide[i], noCueSide[i]
		fmt.Printf("  %-24s %-10s %-10s %-9d %-11d %d\n", a.Task,
			deltaBool(a.AnswerRecovered, b.AnswerRecovered),
			deltaBool(a.CueDirectRead, b.CueDirectRead),
			a.SearchCalls-b.SearchCalls,
			a.RecallReturnedTokens-b.RecallReturnedTokens,
			a.EscapeCalls-b.EscapeCalls)
	}
	fmt.Println("\n## Pooled")
	fmt.Println(" ", summarize("cue-present", cueSide).line())
	fmt.Println(" ", summarize("cue-absent", noCueSide).line())
	fmt.Println("\n## Where each side looked first")
	fmt.Printf("  %-14s %s\n", "cue-present", countsLine(summarize("x", cueSide).Routes))
	fmt.Printf("  %-14s %s\n", "cue-absent", countsLine(summarize("x", noCueSide).Routes))
}

func deltaBool(a, b bool) string {
	switch {
	case a == b:
		return "same"
	case a:
		return "+cue"
	default:
		return "-cue"
	}
}

// runExperiment runs one experiment's tasks across its arms. A dry run drives
// the same pipeline with a scripted provider: it proves fixture, run, scoring
// and report hold together before any of it is paid for.
func runExperiment(experiment, root string, dry bool, tasks []contextTask) int {
	root, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	restore, err := isolateWorkspace()
	if err != nil {
		fmt.Fprintln(os.Stderr, "isolate workspace:", err)
		return 2
	}
	defer restore()
	var p provider.Provider = &scriptedProvider{}
	if !dry {
		real, rerr := realProvider()
		if rerr != nil {
			fmt.Fprintln(os.Stderr, rerr)
			return 2
		}
		p = real
	}
	byArm := map[string][]contextMetrics{}
	var all []contextMetrics

	if experiment == experimentIndex {
		fmt.Print(fingerprint(dry))
	}
	for _, t := range tasks {
		if t.Experiment != experiment {
			continue
		}
		// Arms are shuffled inside each task block. Running six defaults, then
		// six halves, would make provider drift collinear with the axis under
		// test: a bad minute would land entirely on one arm.
		for _, arm := range shuffledArms(t) {
			cueVisible := t.Experiment == experimentIndex && cueExpectedVisible(t, arm.scale)
			m, err := runOne(p, t, armFor(arm.scale, arm.searchOff), arm.name, root, cueVisible, dry)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s [%s]: %v\n", t.ID, arm.name, err)
			}
			byArm[arm.name] = append(byArm[arm.name], m)
			all = append(all, m)
			fmt.Printf("%-24s %-14s %-18s search=%d hit=%d read=%d recall-tok=%d %v\n",
				t.ID, arm.name, m.FailureStage, m.SearchCalls, m.TargetSearchHits, m.ReadCalls,
				m.RecallReturnedTokens, m.UnexpectedWorkTools)
		}
	}
	reportFunnels(byArm)
	if experiment == experimentIndex {
		fmt.Print(reportDoseResponse(all))
	}
	fmt.Print(reportStopping(all))
	fmt.Print(auditQueries(all).report())
	fmt.Print(queryLines(all))
	if experiment == experimentIndex {
		reportBoundaries(all)
	}
	return writeResults(root, all)
}

func cueExpectedVisible(t contextTask, scale string) bool {
	visible, _ := tierScales(t.CueTier)
	return contains(visible, scale)
}

func reportFunnels(byArm map[string][]contextMetrics) {
	names := make([]string, 0, len(byArm))
	for name := range byArm {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Println("\n## Funnel")
	for _, name := range names {
		fmt.Println(" ", summarize(name, byArm[name]).line())
	}
	fmt.Println("\n## Where it looked first")
	for _, name := range names {
		f := summarize(name, byArm[name])
		fmt.Printf("  %-14s %s\n", name, countsLine(f.Routes))
	}
	fmt.Println("\n## Where it broke")
	for _, name := range names {
		f := summarize(name, byArm[name])
		stages := make([]string, 0, len(f.Stages))
		for stage, n := range f.Stages {
			stages = append(stages, fmt.Sprintf("%s=%d", stage, n))
		}
		sort.Strings(stages)
		fmt.Printf("  %-12s %s\n", name, strings.Join(stages, " "))
	}
}

// writeTrajectories saves what each turn produced, after the whole batch is
// done. Three analyses in a row have needed a re-run because something was
// counted and not kept — query text, then snippet contents. It is written at
// the end on purpose: during the batch these files are the answer.
func writeTrajectories(root string, all []contextMetrics) error {
	f, err := os.Create(filepath.Join(root, "trajectories.jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	for _, m := range all {
		if err := writeJSONLine(f, map[string]any{
			"task": m.Task, "arm": m.Arm, "messages": m.trajectory,
		}); err != nil {
			return err
		}
	}
	return nil
}

func writeResults(root string, all []contextMetrics) int {
	path := filepath.Join(root, "results.jsonl")
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer f.Close()
	for _, m := range all {
		if err := writeJSONLine(f, m); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	if err := writeTrajectories(root, all); err != nil {
		fmt.Fprintln(os.Stderr, "write trajectories:", err)
		return 2
	}
	fmt.Printf("\n%d runs written to %s (with trajectories.jsonl)\n", len(all), path)
	return 0
}

// reportBoundaries prints each index task across its own cue boundary, then the
// two sides pooled. Task difficulty is held constant inside a row, which is
// what a four-arm average cannot do.
func reportBoundaries(all []contextMetrics) {
	byKey := map[string]contextMetrics{}
	for _, m := range all {
		byKey[m.Task+"|"+m.Arm] = m
	}
	var cueSide, noCueSide []contextMetrics
	fmt.Println("\n## Across each task's cue boundary")
	fmt.Printf("  %-24s %-16s %-4s %-10s %-9s %-12s %-11s %s\n",
		"task", "arm", "cue", "recovered", "cue-read", "search/task", "recall-tok", "escape")
	for _, t := range indexTasks() {
		cue, noCue := boundaryPair(t.CueTier)
		for _, side := range []struct {
			scale string
			has   bool
		}{{cue, true}, {noCue, false}} {
			m, ok := byKey[t.ID+"|index-"+side.scale]
			if !ok {
				continue
			}
			if side.has {
				cueSide = append(cueSide, m)
			} else {
				noCueSide = append(noCueSide, m)
			}
			mark := "no"
			if side.has {
				mark = "yes"
			}
			fmt.Printf("  %-24s %-16s %-4s %-10v %-9v %-12d %-11d %d\n",
				t.ID, "index-"+side.scale, mark, m.AnswerRecovered, m.RecallReadWithoutSearch,
				m.SearchCalls, m.RecallReturnedTokens, m.EscapeCalls)
		}
	}
	fmt.Println("\n## Pooled across boundaries")
	fmt.Println(" ", summarize("cue-present", cueSide).line())
	fmt.Println(" ", summarize("cue-absent", noCueSide).line())
}

// shuffledArms permutes a task's arms deterministically, so the order is
// reproducible and still uncorrelated with the axis being measured.
func shuffledArms(t contextTask) []experimentArm {
	arms := armsFor(t)
	rng := seededRand(t.ID, "arm-order")
	rng.Shuffle(len(arms), func(i, j int) { arms[i], arms[j] = arms[j], arms[i] })
	return arms
}

// fingerprint records what produced a batch. Not a gate — but batch effects
// have reversed three conclusions in this experiment already, and a table with
// no provenance cannot be compared with the next one.
func fingerprint(dry bool) string {
	model, endpoint := runModel, runBaseURL
	if dry {
		model, endpoint = "scripted", "none"
	}
	commit := "unknown"
	if out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(out))
	}
	return fmt.Sprintf("## Batch\n  model=%s endpoint=%s window=%d generations=%d commit=%s started=%s\n",
		model, endpoint, fixtureWindow, fixtureGenerations, commit, time.Now().UTC().Format(time.RFC3339))
}

// timeoutReason separates a run that never found its answer from one that found
// it and kept going. Only the first is a case for a longer budget; the second
// would just get more time to circle. A batch reporting only "context deadline
// exceeded" cannot tell an arm that made retrieval harder from an arm that
// happened to draw a slow minute.
func timeoutReason(m contextMetrics) string {
	switch {
	case m.TargetHeldAtEnd && m.RoundsAfterTarget >= 2:
		return "PostTargetRunaway"
	case m.TargetHeldAtEnd:
		return "TargetHeldLate"
	case m.SearchCalls >= 3:
		return "SearchSpiral"
	case m.SearchCalls+m.ReadCalls+m.EscapeCalls == 0:
		return "NoToolActivity"
	default:
		return "Unknown"
	}
}
