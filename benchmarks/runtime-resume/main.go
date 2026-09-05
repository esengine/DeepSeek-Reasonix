// Process-boundary resurrection probe. It answers one question and modifies
// nothing: after the process that built a session exits, what can a new process
// still prove about that session's runtime semantics without asking a model?
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	phase := flag.String("phase", "", "internal: construct|successor|resume, spawned by the orchestrator")
	root := flag.String("root", "", "internal: this arm's root directory")
	armName := flag.String("arm", "", "internal: this arm's name")
	work := flag.String("work", "", "directory to hold arm roots; default is a temp dir")
	only := flag.String("only", "", "run a single arm by name")
	jsonOut := flag.String("json", "", "also write the machine-readable matrix here")
	keep := flag.Bool("keep", false, "keep arm roots on disk after the run")
	sabotage := flag.String("sabotage", "", "run under a deliberate defect the arm must fail on (ui-graph-mixed: publish|trajectory)")
	flag.Parse()
	if *sabotage != "" {
		// Carried to the children through the environment they already inherit,
		// so no phase has to learn about a flag it does not read.
		if err := os.Setenv(envProbeSabotage, *sabotage); err != nil {
			fmt.Fprintln(os.Stderr, "runtime-resume:", err)
			os.Exit(1)
		}
	}

	var err error
	switch *phase {
	case "construct":
		err = runConstruct(*root, *armName)
	case "successor":
		err = runSuccessor(*root, *armName)
	case "resume":
		err = runResume(*root, *armName)
	case "":
		err = orchestrate(*work, *only, *jsonOut, *keep)
	default:
		err = fmt.Errorf("unknown phase %q", *phase)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "runtime-resume:", err)
		os.Exit(1)
	}
}
