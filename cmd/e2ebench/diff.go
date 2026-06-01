package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type diffOpts struct {
	bin, model, repo, base, testCmd string
	maxSteps, timeoutSec            int
}

type testRef struct{ name, pkg string }

// pinResult records whether one generated test fails when the PR's source is
// reverted (so it pins the change) and, if so, whether it failed by assertion
// (strong: it checks the new behavior) or only by compile error (weak: it just
// references a symbol the PR added).
type pinResult struct {
	testRef
	pins        bool
	byAssertion bool
}

// runDiff asks the agent to write tests covering what the PR changed, on the PR
// branch, then grades with the repo's own tests: the new tests must pass on the
// PR code, and at least one must fail when the source is reverted to its pre-PR
// state. Returns a markdown report that includes the generated test diff so the
// assertions can be audited, not just trusted.
func runDiff(o diffOpts) string {
	srcFiles := changedGoFiles(o.repo, o.base, false)
	if len(srcFiles) == 0 {
		return "## 🤖 Reasonix e2e — diff test-gen\n\nNo Go source changes in this PR (excluding `_test.go`); nothing to generate tests for.\n"
	}
	pkgs := packagesOf(srcFiles)
	diffText := truncate(gitOut(o.repo, "diff", o.base+"...HEAD", "--"))

	prompt := buildDiffPrompt(srcFiles, pkgs, diffText)

	metricsPath := filepath.Join(o.repo, ".e2e-diff-metrics.json")
	_ = os.Remove(metricsPath)
	defer os.Remove(metricsPath)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(o.timeoutSec)*time.Second)
	defer cancel()

	args := []string{"run", "--metrics", metricsPath, "--max-steps", fmt.Sprint(o.maxSteps)}
	if o.model != "" {
		args = append(args, "--model", o.model)
	}
	args = append(args, prompt)
	cmd := exec.CommandContext(ctx, o.bin, args...)
	cmd.Dir = o.repo
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	runErr := cmd.Run()

	// The agent's new files are untracked, so `git diff HEAD` would miss them;
	// intent-to-add surfaces them as additions without committing.
	_ = exec.Command("git", "-C", o.repo, "add", "-AN").Run()

	m, _ := readMetrics(metricsPath)
	testDiff := gitOut(o.repo, "diff", "HEAD", "--", "*_test.go")
	refs := parseNewTests(testDiff)
	sourceTouched := len(changedGoFilesWorktree(o.repo, false))
	testsPass, testOut := runTests(o.repo, o.testCmd, pkgs)

	var pins []pinResult
	if len(refs) > 0 && testsPass {
		pins = differentialPerTest(o.repo, o.base, srcFiles, refs)
	}

	passed := len(refs) > 0 && testsPass && countPins(pins) > 0
	return renderDiff(diffReport{
		srcFiles: srcFiles, pkgs: pkgs, addedTestLines: countAdded(testDiff),
		newTests: refs, sourceTouched: sourceTouched, testsPass: testsPass,
		pins: pins, failing: failingTestNames(testOut), passed: passed,
		m: m, runErr: runErr, testOut: testOut, testDiff: testDiff,
	})
}

func buildDiffPrompt(srcFiles, pkgs []string, diffText string) string {
	var b strings.Builder
	b.WriteString("You are in a Go repository. This pull request changed these source files:\n")
	for _, f := range srcFiles {
		fmt.Fprintf(&b, "  - %s\n", f)
	}
	b.WriteString("\nUnified diff of the change:\n```diff\n")
	b.WriteString(diffText)
	b.WriteString("\n```\n\n")
	b.WriteString("Write focused Go unit tests that exercise the NEW or CHANGED behavior in those files. ")
	b.WriteString("Add them to the appropriate *_test.go files in the same packages (")
	b.WriteString(strings.Join(pkgs, ", "))
	b.WriteString("). Do NOT modify the non-test source files — only add or extend test files. ")
	b.WriteString("Prefer small, focused edits and run `gofmt`/`go vet` on the test files as you go to avoid syntax errors. ")
	b.WriteString("Then run the package tests and iterate until they pass. When finished, list the test functions you added.")
	return b.String()
}

type diffReport struct {
	srcFiles, pkgs []string
	addedTestLines int
	newTests       []testRef
	sourceTouched  int
	testsPass      bool
	pins           []pinResult
	failing        []string
	passed         bool
	m              runMetrics
	runErr         error
	testOut        string
	testDiff       string
}

func renderDiff(r diffReport) string {
	var b strings.Builder
	result := "❌ fail"
	if r.passed {
		result = "✅ pass"
	}
	fmt.Fprintf(&b, "## 🤖 Reasonix e2e — diff test-gen\n\n")
	fmt.Fprintf(&b, "**Result:** %s · **%d** changed source file(s) across **%d** package(s)\n\n", result, len(r.srcFiles), len(r.pkgs))

	pinned, byAssert := countPins(r.pins), countAssertionPins(r.pins)
	fmt.Fprintf(&b, "| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| New test functions added | %d |\n", len(r.newTests))
	fmt.Fprintf(&b, "| Test lines added | +%d |\n", r.addedTestLines)
	fmt.Fprintf(&b, "| `go test` on affected pkgs | %s |\n", passFail(r.testsPass))
	fmt.Fprintf(&b, "| Differential (fail on pre-PR code) | %s |\n", differentialCell(r))
	if pinned > 0 {
		fmt.Fprintf(&b, "| ↳ pin by assertion / by compile only | %d / %d |\n", byAssert, pinned-byAssert)
	}
	fmt.Fprintf(&b, "| Non-test source touched by agent | %d file(s) |\n", r.sourceTouched)
	fmt.Fprintf(&b, "| Cache hit | %s |\n", pct(r.m.CacheHitTokens, r.m.CacheHitTokens+r.m.CacheMissTokens))
	fmt.Fprintf(&b, "| Tokens (prompt / completion) | %s / %s |\n", comma(r.m.PromptTokens), comma(r.m.CompletionTokens))
	fmt.Fprintf(&b, "| Model calls | %d |\n", r.m.Steps)
	fmt.Fprintf(&b, "| Cost | %s%.4f |\n", currencySym(r.m.Currency), r.m.Cost)
	if len(r.failing) > 0 {
		fmt.Fprintf(&b, "| Failing tests | `%s` |\n", strings.Join(r.failing, "`, `"))
	}

	fmt.Fprintf(&b, "\n**Packages:** %s\n", strings.Join(r.pkgs, ", "))
	if r.sourceTouched > 0 {
		fmt.Fprintf(&b, "\n⚠️ The agent modified %d non-test source file(s); a green run may not reflect the PR's code. Review the diff.\n", r.sourceTouched)
	}

	if len(r.pins) > 0 {
		fmt.Fprintf(&b, "\n<details><summary>Per-test differential</summary>\n\n| Test | Package | Pins the change? |\n|---|---|---|\n")
		for _, p := range r.pins {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", p.name, p.pkg, pinCell(p))
		}
		fmt.Fprintf(&b, "\n</details>\n")
	}
	if strings.TrimSpace(r.testDiff) != "" {
		fmt.Fprintf(&b, "\n<details><summary>Generated tests (review the assertions)</summary>\n\n```diff\n%s\n```\n</details>\n", truncateFor(r.testDiff, 20000))
	}
	if !r.testsPass && strings.TrimSpace(r.testOut) != "" {
		fmt.Fprintf(&b, "\n<details><summary>go test output (tail)</summary>\n\n```\n%s\n```\n</details>\n", tail(r.testOut, 60))
	}
	if r.runErr != nil {
		fmt.Fprintf(&b, "\n<sub>agent run note: %v</sub>\n", r.runErr)
	}
	fmt.Fprintf(&b, "\n<sub>Pass = the agent added ≥1 test, the affected packages are green, AND ≥1 new test fails when the PR's source is reverted. \"By assertion\" pins are strong (they check changed behavior); \"by compile only\" pins just need a PR-added symbol — and since Go compiles per package, one compile-coupled test marks every test in its package that way, so read the diff above to judge the rest.</sub>\n")
	return b.String()
}

func differentialCell(r diffReport) string {
	if !(len(r.newTests) > 0 && r.testsPass) {
		return "n/a (tests not green)"
	}
	return fmt.Sprintf("%d/%d new tests", countPins(r.pins), len(r.pins))
}

func pinCell(p pinResult) string {
	switch {
	case p.pins && p.byAssertion:
		return "✅ by assertion"
	case p.pins:
		return "⚠️ by compile only"
	default:
		return "❌ no (passes on old code)"
	}
}

// differentialPerTest reverts the PR's changed source to base (deleting files
// new in the PR), runs each generated test on its own against the old code, and
// restores the source. A test that fails on the old code pins the change.
func differentialPerTest(repo, base string, srcFiles []string, refs []testRef) []pinResult {
	for _, f := range srcFiles {
		if err := exec.Command("git", "-C", repo, "checkout", base, "--", f).Run(); err != nil {
			_ = os.Remove(filepath.Join(repo, filepath.FromSlash(f)))
		}
	}
	out := make([]pinResult, 0, len(refs))
	for _, r := range refs {
		cmd := exec.Command("go", "test", "-run", "^"+r.name+"$", r.pkg)
		cmd.Dir = repo
		raw, err := cmd.CombinedOutput()
		out = append(out, pinResult{
			testRef:     r,
			pins:        err != nil,
			byAssertion: strings.Contains(string(raw), "--- FAIL: "+r.name),
		})
	}
	for _, f := range srcFiles {
		_ = exec.Command("git", "-C", repo, "checkout", "HEAD", "--", f).Run()
	}
	return out
}

func countPins(ps []pinResult) int {
	n := 0
	for _, p := range ps {
		if p.pins {
			n++
		}
	}
	return n
}

func countAssertionPins(ps []pinResult) int {
	n := 0
	for _, p := range ps {
		if p.pins && p.byAssertion {
			n++
		}
	}
	return n
}

// parseNewTests reads the working-tree *_test.go diff and returns the Test/Fuzz/
// Benchmark functions the agent added, each tagged with its package directory.
func parseNewTests(diff string) []testRef {
	var refs []testRef
	pkg := ""
	for _, ln := range strings.Split(diff, "\n") {
		if strings.HasPrefix(ln, "+++ b/") {
			pkg = "./" + filepath.ToSlash(filepath.Dir(strings.TrimPrefix(ln, "+++ b/")))
			continue
		}
		if !strings.HasPrefix(ln, "+") || strings.HasPrefix(ln, "+++") {
			continue
		}
		body := strings.TrimSpace(ln[1:])
		if !strings.HasPrefix(body, "func ") {
			continue
		}
		sig := strings.TrimPrefix(body, "func ")
		paren := strings.IndexByte(sig, '(')
		if paren <= 0 {
			continue
		}
		name := sig[:paren]
		if strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Benchmark") {
			refs = append(refs, testRef{name: name, pkg: pkg})
		}
	}
	return refs
}

func countAdded(diff string) int {
	n := 0
	for _, ln := range strings.Split(diff, "\n") {
		if strings.HasPrefix(ln, "+") && !strings.HasPrefix(ln, "+++") {
			n++
		}
	}
	return n
}

// failingTestNames pulls the names out of `--- FAIL: TestX (…)` lines.
func failingTestNames(out string) []string {
	var names []string
	seen := map[string]bool{}
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "--- FAIL:") {
			continue
		}
		rest := strings.Fields(strings.TrimSpace(strings.TrimPrefix(ln, "--- FAIL:")))
		if len(rest) > 0 && !seen[rest[0]] {
			seen[rest[0]] = true
			names = append(names, rest[0])
		}
	}
	return names
}

func runTests(repo, testCmd string, pkgs []string) (bool, string) {
	fields := strings.Fields(testCmd)
	if len(fields) == 0 {
		fields = []string{"go", "test"}
	}
	args := append(fields[1:], pkgs...)
	cmd := exec.Command(fields[0], args...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}

// changedGoFiles lists .go files changed by base...HEAD, excluding *_test.go
// when includeTests is false (we want the source under test).
func changedGoFiles(repo, base string, includeTests bool) []string {
	return filterGo(gitOut(repo, "diff", "--name-only", base+"...HEAD", "--", "*.go"), includeTests)
}

func changedGoFilesWorktree(repo string, includeTests bool) []string {
	return filterGo(gitOut(repo, "diff", "--name-only", "HEAD", "--", "*.go"), includeTests)
}

func filterGo(out string, includeTests bool) []string {
	var keep []string
	for _, f := range strings.Fields(strings.ReplaceAll(out, "\n", " ")) {
		if strings.HasSuffix(f, "_test.go") && !includeTests {
			continue
		}
		keep = append(keep, f)
	}
	sort.Strings(keep)
	return keep
}

func packagesOf(files []string) []string {
	seen := map[string]bool{}
	var pkgs []string
	for _, f := range files {
		dir := "./" + filepath.ToSlash(filepath.Dir(f))
		if !seen[dir] {
			seen[dir] = true
			pkgs = append(pkgs, dir)
		}
	}
	sort.Strings(pkgs)
	return pkgs
}

func gitOut(repo string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, _ := cmd.Output()
	return string(out)
}

func truncate(s string) string { return truncateFor(s, 12000) }

func truncateFor(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…(truncated)…"
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func passFail(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}
