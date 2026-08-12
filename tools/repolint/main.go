// repolint enforces the repo standards that gofmt/vet/golangci cannot express.
package main

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

type Finding struct {
	File string
	Line int
	Rule string
	Msg  string
	// Excess over the rule's limit, so trading a short violation for a long
	// one still trips the ratchet. One for rules that are pass/fail.
	Weight int
}

const (
	ruleEssay       = "essay"
	ruleBanner      = "banner"
	ruleMarker      = "marker"
	ruleDeadCode    = "commented-code"
	ruleNarrative   = "narrative"
	ruleFileSize    = "file-size"
	ruleTestSize    = "test-file-size"
	ruleLayering    = "layering"
	ruleFuncSize    = "function-size"
	ruleComplexity  = "complexity"
	ruleStructState = "struct-state"
)

var allRules = []string{
	ruleEssay, ruleBanner, ruleMarker, ruleDeadCode,
	ruleNarrative, ruleFileSize, ruleTestSize, ruleLayering,
	ruleFuncSize, ruleComplexity, ruleStructState,
}

func main() {
	root := flag.String("root", ".", "repository root to scan")
	baselinePath := flag.String("baseline", "", "baseline file (default <root>/tools/repolint/baseline.json)")
	update := flag.Bool("update", false, "rewrite the baseline from the current tree")
	strict := flag.Bool("strict", false, "report every finding, ignoring the baseline")
	flag.Parse()

	if *baselinePath == "" {
		*baselinePath = filepath.Join(*root, "tools", "repolint", "baseline.json")
	}

	findings, err := run(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "repolint:", err)
		os.Exit(2)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	if *update {
		if err := baselineFrom(findings).write(*baselinePath); err != nil {
			fmt.Fprintln(os.Stderr, "repolint:", err)
			os.Exit(2)
		}
		fmt.Printf("wrote %s (%d findings across %d files)\n", *baselinePath, len(findings), countFiles(findings))
		return
	}

	if *strict {
		report(findings)
		fmt.Printf("\n%d findings across %d files\n", len(findings), countFiles(findings))
		if len(findings) > 0 {
			os.Exit(1)
		}
		return
	}

	baseline, err := loadBaseline(*baselinePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "repolint:", err)
		os.Exit(2)
	}
	over, msgs := baseline.exceeded(findings)
	if len(msgs) == 0 {
		fmt.Printf("repolint: clean (%d baselined findings)\n", len(findings))
		return
	}
	report(over)
	fmt.Fprintln(os.Stderr)
	for _, m := range msgs {
		fmt.Fprintln(os.Stderr, "repolint:", m)
	}
	fmt.Fprintf(os.Stderr, "\nNew standards violations. Fix them, or if this is a deliberate\n"+
		"carry-forward (file rename, extraction), run:\n\n    go run ./tools/repolint -update\n\n"+
		"and justify the baseline diff in the pull request.\n")
	os.Exit(1)
}

func run(root string) ([]Finding, error) {
	paths, err := collect(root)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	imports := map[string][]importRef{}
	for _, rel := range paths {
		src, err := parseSource(root, rel)
		if err != nil {
			return nil, err
		}
		if src == nil {
			continue
		}
		findings = append(findings, checkSize(src)...)
		if src.file == nil {
			continue
		}
		findings = append(findings, checkComments(src)...)
		findings = append(findings, checkComplexity(src)...)
		findings = append(findings, checkStructState(src)...)
		imports[rel] = src.importRefs()
	}
	return append(findings, checkLayering(imports)...), nil
}

func report(findings []Finding) {
	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "%s:%d: [%s] %s\n", f.File, f.Line, f.Rule, f.Msg)
	}
}

func countFiles(findings []Finding) int {
	seen := map[string]bool{}
	for _, f := range findings {
		seen[f.File] = true
	}
	return len(seen)
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
