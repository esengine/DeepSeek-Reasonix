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
	phase := flag.String("phase", "", "internal: construct|resume, spawned by the orchestrator")
	root := flag.String("root", "", "internal: this arm's root directory")
	armName := flag.String("arm", "", "internal: this arm's name")
	work := flag.String("work", "", "directory to hold arm roots; default is a temp dir")
	only := flag.String("only", "", "run a single arm by name")
	jsonOut := flag.String("json", "", "also write the machine-readable matrix here")
	keep := flag.Bool("keep", false, "keep arm roots on disk after the run")
	flag.Parse()

	var err error
	switch *phase {
	case "construct":
		err = runConstruct(*root, *armName)
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
