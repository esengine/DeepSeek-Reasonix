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
	"strings"
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
	ruleOrphan         = "orphan"
	ruleEssay          = "essay"
	ruleBanner         = "banner"
	ruleMarker         = "marker"
	ruleDeadCode       = "commented-code"
	ruleNarrative      = "narrative"
	ruleFileSize       = "file-size"
	ruleLayering       = "layering"
	ruleFuncSize       = "function-size"
	ruleComplexity     = "complexity"
	ruleStructState    = "struct-state"
	ruleRefusalPath    = "refusal-path"
	ruleErrorText      = "error-text"
	ruleClaudeDialect  = "claude-dialect"
	ruleSpecParity     = "spec-parity"
	ruleWireParity     = "wire-parity"
	ruleFrontendParity = "frontend-parity"
	ruleFlatView       = "flat-view"
	ruleBuildArtifact  = "build-artifact"
)

var allRules = []string{
	ruleEssay, ruleBanner, ruleMarker, ruleDeadCode,
	ruleNarrative, ruleFileSize, ruleLayering,
	ruleFuncSize, ruleComplexity, ruleStructState, ruleRefusalPath, ruleErrorText,
	ruleClaudeDialect, ruleSpecParity, ruleWireParity, ruleOrphan,
	ruleFrontendParity, ruleFlatView, ruleBuildArtifact,
}

func main() {
	root := flag.String("root", ".", "repository root to scan")
	baselinePath := flag.String("baseline", "", "baseline file (default <root>/tools/repolint/baseline.json)")
	update := flag.Bool("update", false, "rewrite the baseline from the current tree")
	strict := flag.Bool("strict", false, "report every finding, ignoring the baseline")
	allowWiden := flag.Bool("allow-widen", false, "let -update raise a budget. Lowering one needs nothing; raising one carries debt forward, which is the diff a reviewer has to be shown on purpose")
	only := flag.String("only", "", "comma-separated paths: report file budgets for these only, so a change can be checked without reading the whole tree's recorded debt (repo-wide ceilings still report). Pair with `git diff --name-only`.")
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
		next := baselineFrom(findings)
		prev, _ := loadBaseline(*baselinePath)
		if widened := widenings(prev, next); len(widened) > 0 && !*allowWiden {
			fmt.Fprintln(os.Stderr, "repolint: this -update would raise budgets, which carries debt forward:")
			for _, w := range widened {
				fmt.Fprintln(os.Stderr, "  "+w)
			}
			fmt.Fprintln(os.Stderr, "\nFix the code, or pass -allow-widen and justify the diff in the pull request.")
			os.Exit(1)
		}
		if err := next.write(*baselinePath); err != nil {
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
	over, overruns := baseline.exceeded(findings)
	if paths := splitPaths(*only); len(paths) > 0 {
		over, overruns = limitToPaths(over, overruns, paths)
	}
	if len(overruns) == 0 {
		fmt.Printf("repolint: clean (%d baselined findings)\n", len(findings))
		// Budget the tree no longer needs is budget a file can quietly grow back
		// into, and it only comes down when someone runs -update.
		if slack := reclaimable(baseline, findings); len(slack) > 0 {
			rules := slices.Sorted(maps.Keys(slack))
			parts := make([]string, 0, len(rules))
			for _, rule := range rules {
				parts = append(parts, fmt.Sprintf("%s %d", rule, slack[rule]))
			}
			fmt.Printf("repolint: %s of budget is no longer used; -update reclaims it\n", strings.Join(parts, ", "))
		}
		return
	}
	report(over)
	fmt.Fprintln(os.Stderr)
	for _, o := range overruns {
		fmt.Fprintln(os.Stderr, "repolint:", o)
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
	var dialects []providerDialect
	modelVars := map[string][]string{}
	orphans := newOrphanScan()
	wires := newWireScan()
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
		findings = append(findings, checkRefusalPath(src)...)
		findings = append(findings, checkErrorText(src)...)
		findings = append(findings, checkFlatView(src)...)
		orphans.observe(src)
		wires.observe(src)
		entries, vars := dialectRefs(src)
		dialects = append(dialects, entries...)
		maps.Copy(modelVars, vars)
		imports[rel] = src.importRefs()
	}
	findings = append(findings, orphans.findings()...)
	findings = append(findings, checkLayering(imports)...)
	findings = append(findings, checkSpecParity(root)...)
	findings = append(findings, checkBuildArtifacts(root)...)
	findings = append(findings, wires.findings(root)...)
	parity, err := checkFrontendParity(root)
	if err != nil {
		return nil, err
	}
	findings = append(findings, parity...)
	return append(findings, checkClaudeDialect(dialects, modelVars)...), nil
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
