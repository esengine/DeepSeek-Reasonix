// ContextRetrievalBench builds the fixtures the long-horizon retrieval
// experiments run against, and proves they are worth paying a provider for.
//
//	-mode=calibrate  find each index task's lead-call count for its cue tier
//	-mode=preflight  build every task under every arm and check the contract
//
// Nothing here calls a provider. The experiments themselves do; this is what
// says they would measure what they claim to.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

func main() {
	mode := flag.String("mode", "preflight", "calibrate | preflight | snippet | adversarial | run-search | run-boundary | run-index")
	work := flag.String("work", "", "directory for built fixtures (default: a temp dir)")
	only := flag.String("task", "", "limit to one task id")
	dry := flag.Bool("dry", false, "run modes: drive the pipeline with a scripted provider instead of paying for one")
	flag.Parse()

	if err := validateCorpus(); err != nil {
		fmt.Fprintln(os.Stderr, "corpus:", err)
		os.Exit(2)
	}
	root := *work
	if root == "" {
		dir, err := os.MkdirTemp("", "context-retrieval-*")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		defer os.RemoveAll(dir)
		root = dir
	}

	tasks := allTasks()
	if *only != "" {
		t, ok := taskByID(*only)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown task %q\n", *only)
			os.Exit(2)
		}
		tasks = []contextTask{t}
	}

	switch *mode {
	case "calibrate":
		os.Exit(runCalibrate(tasks, root))
	case "preflight":
		os.Exit(runPreflight(tasks, root))
	case "run-search":
		os.Exit(runExperiment(experimentSearch, root, *dry, tasks))
	case "run-boundary":
		os.Exit(runBoundaries(root))
	case "snippet":
		os.Exit(runSnippetAudit(root, 100))
	case "adversarial":
		os.Exit(runAdversarial(root))
	case "run-index":
		batch := tasks
		if *only == "" {
			// A batch measures the efficiency substrate only. Naming one task
			// with -task runs that task, role included, so the stopping corpus
			// stays measurable on its own.
			batch = efficiencyTasks()
		}
		os.Exit(runExperiment(experimentIndex, root, *dry, batch))
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", *mode)
		os.Exit(2)
	}
}

func runPreflight(tasks []contextTask, root string) int {
	var all []finding
	for _, t := range tasks {
		found, err := checkTask(t, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", t.ID, err)
			return 2
		}
		all = append(all, found...)
		status := "ok"
		if len(found) > 0 {
			status = fmt.Sprintf("%d problem(s)", len(found))
		}
		fmt.Printf("%-24s %-6s gen=%-2d %s\n", t.ID, t.Experiment, t.PlantAfterGen, status)
	}
	if len(all) == 0 {
		fmt.Printf("\n%d fixtures ready.\n", len(tasks))
		return 0
	}
	fmt.Fprintf(os.Stderr, "\n%d preflight problem(s):\n", len(all))
	sort.Slice(all, func(i, j int) bool { return all[i].Task < all[j].Task })
	for _, f := range all {
		fmt.Fprintln(os.Stderr, " ", f)
	}
	return 1
}

// runCalibrate reports, for each index task, the lead-call counts at which its
// cue is still addressed by each budget. The corpus freezes one number from
// this; it is never computed at run time, or the implementation under test
// would be moving its own goalposts.
func runCalibrate(tasks []contextTask, root string) int {
	for _, t := range tasks {
		if t.Experiment != experimentIndex {
			continue
		}
		var rows []string
		for lead := range fixtureGenerations {
			probe := t
			probe.PlantAfterGen = lead
			var visibleAt []string
			for _, scale := range armScales {
				inst, ierr := instantiateTask(probe, seededRand(t.ID, scale, "calibrate"))
				if ierr != nil {
					fmt.Fprintln(os.Stderr, ierr)
					return 2
				}
				f, err := buildFixture(inst, armFor(scale, false),
					fmt.Sprintf("%s/calib/%s/%d-%s/session.jsonl", root, t.ID, lead, scale))
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s gen=%d %s: %v\n", t.ID, lead, scale, err)
					return 2
				}
				if strings.Contains(visibleText(f), inst.CueMarker) {
					visibleAt = append(visibleAt, scale)
				}
			}
			rows = append(rows, fmt.Sprintf("planted after gen %d, cue visible at: %s", lead, strings.Join(visibleAt, ",")))
		}
		fmt.Printf("\n%s (wants tier %s)\n", t.ID, t.CueTier)
		for _, r := range rows {
			fmt.Println(" ", r)
		}
	}
	return 0
}
