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

// runDiff asks the agent to write tests covering what the PR changed, on the PR
// branch itself, then grades with the repo's own tests: the generated tests must
// pass and the agent must actually have added test code (so a no-op can't pass,
// since the suite was already green). Returns a markdown report.
func runDiff(o diffOpts) string {
	srcFiles := changedGoFiles(o.repo, o.base, false)
	if len(srcFiles) == 0 {
		return "## 🤖 Reasonix e2e — diff test-gen\n\nNo Go source changes in this PR (excluding `_test.go`); nothing to generate tests for.\n"
	}
	pkgs := packagesOf(srcFiles)
	diffText := truncate(gitOut(o.repo, "diff", o.base+"...HEAD", "--"), srcFiles...)

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
	addedTestLines, newTestFuncs := testsAdded(o.repo)
	sourceTouched := len(changedGoFilesWorktree(o.repo, false))
	testsPass, testOut := runTests(o.repo, o.testCmd, pkgs)

	// Differential check: the new tests must FAIL when the PR's source is reverted
	// to its pre-change state, proving they actually capture the change rather than
	// asserting behavior that already held. Only meaningful once the tests are green.
	diffVerified := false
	if newTestFuncs > 0 && testsPass {
		diffVerified = differentialCheck(o.repo, o.base, srcFiles, o.testCmd, pkgs)
	}

	passed := newTestFuncs > 0 && testsPass && diffVerified
	return renderDiff(diffReport{
		srcFiles: srcFiles, pkgs: pkgs, addedTestLines: addedTestLines,
		newTestFuncs: newTestFuncs, sourceTouched: sourceTouched, testsPass: testsPass,
		diffVerified: diffVerified, passed: passed, m: m, runErr: runErr, testOut: testOut,
	})
}

// differentialCheck reverts the PR's changed source to base (deleting files that
// were new in the PR), runs the now-present tests, and restores the source. The
// tests should fail against the old code; !green means they pin the change.
func differentialCheck(repo, base string, srcFiles []string, testCmd string, pkgs []string) bool {
	for _, f := range srcFiles {
		if err := exec.Command("git", "-C", repo, "checkout", base, "--", f).Run(); err != nil {
			_ = os.Remove(filepath.Join(repo, filepath.FromSlash(f)))
		}
	}
	green, _ := runTests(repo, testCmd, pkgs)
	for _, f := range srcFiles {
		_ = exec.Command("git", "-C", repo, "checkout", "HEAD", "--", f).Run()
	}
	return !green
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
	b.WriteString("Then run the package tests and iterate until they pass. When finished, list the test functions you added.")
	return b.String()
}

type diffReport struct {
	srcFiles, pkgs               []string
	addedTestLines, newTestFuncs int
	sourceTouched                int
	testsPass, diffVerified      bool
	passed                       bool
	m                            runMetrics
	runErr                       error
	testOut                      string
}

func renderDiff(r diffReport) string {
	var b strings.Builder
	result := "❌ fail"
	if r.passed {
		result = "✅ pass"
	}
	fmt.Fprintf(&b, "## 🤖 Reasonix e2e — diff test-gen\n\n")
	fmt.Fprintf(&b, "**Result:** %s · **%d** changed source file(s) across **%d** package(s)\n\n", result, len(r.srcFiles), len(r.pkgs))

	fmt.Fprintf(&b, "| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| New test functions added | %d |\n", r.newTestFuncs)
	fmt.Fprintf(&b, "| Test lines added | +%d |\n", r.addedTestLines)
	fmt.Fprintf(&b, "| `%s` on affected pkgs | %s |\n", "go test", passFail(r.testsPass))
	fmt.Fprintf(&b, "| Differential (fails on pre-PR code) | %s |\n", yesNo(r.diffVerified))
	fmt.Fprintf(&b, "| Non-test source touched by agent | %d file(s) |\n", r.sourceTouched)
	fmt.Fprintf(&b, "| Cache hit | %s |\n", pct(r.m.CacheHitTokens, r.m.CacheHitTokens+r.m.CacheMissTokens))
	fmt.Fprintf(&b, "| Tokens (prompt / completion) | %s / %s |\n", comma(r.m.PromptTokens), comma(r.m.CompletionTokens))
	fmt.Fprintf(&b, "| Model calls | %d |\n", r.m.Steps)
	fmt.Fprintf(&b, "| Cost | %s%.4f |\n", currencySym(r.m.Currency), r.m.Cost)

	fmt.Fprintf(&b, "\n**Packages:** %s\n", strings.Join(r.pkgs, ", "))
	if r.sourceTouched > 0 {
		fmt.Fprintf(&b, "\n⚠️ The agent modified %d non-test source file(s); tests passing may not reflect the PR's code. Review the diff.\n", r.sourceTouched)
	}
	if !r.testsPass && strings.TrimSpace(r.testOut) != "" {
		fmt.Fprintf(&b, "\n<details><summary>go test output (tail)</summary>\n\n```\n%s\n```\n</details>\n", tail(r.testOut, 60))
	}
	if r.runErr != nil {
		fmt.Fprintf(&b, "\n<sub>agent run note: %v</sub>\n", r.runErr)
	}
	fmt.Fprintf(&b, "\n<sub>Pass = the agent added ≥1 new test function, the affected packages' tests are green, AND those tests fail when the PR's source is reverted (so they genuinely cover the change).</sub>\n")
	return b.String()
}

func passFail(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}

func yesNo(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}

// changedGoFiles lists .go files changed by base...HEAD. When includeTests is
// false, *_test.go files are excluded (we want the source under test).
func changedGoFiles(repo, base string, includeTests bool) []string {
	out := gitOut(repo, "diff", "--name-only", base+"...HEAD", "--", "*.go")
	return filterGo(out, includeTests)
}

// changedGoFilesWorktree lists .go files changed in the working tree vs HEAD
// (i.e. what the agent just did).
func changedGoFilesWorktree(repo string, includeTests bool) []string {
	out := gitOut(repo, "diff", "--name-only", "HEAD", "--", "*.go")
	files := strings.Fields(strings.ReplaceAll(out, "\n", " "))
	var keep []string
	for _, f := range files {
		isTest := strings.HasSuffix(f, "_test.go")
		if isTest && !includeTests {
			continue
		}
		if !isTest {
			keep = append(keep, f)
		} else if includeTests {
			keep = append(keep, f)
		}
	}
	return keep
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

// testsAdded counts the test lines and new Test functions the agent added,
// reading the working-tree diff of *_test.go files.
func testsAdded(repo string) (lines, funcs int) {
	diff := gitOut(repo, "diff", "HEAD", "--", "*_test.go")
	for _, ln := range strings.Split(diff, "\n") {
		if strings.HasPrefix(ln, "+") && !strings.HasPrefix(ln, "+++") {
			lines++
			body := strings.TrimSpace(strings.TrimPrefix(ln, "+"))
			if strings.HasPrefix(body, "func Test") || strings.HasPrefix(body, "func Fuzz") || strings.HasPrefix(body, "func Benchmark") {
				funcs++
			}
		}
	}
	return lines, funcs
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

func gitOut(repo string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, _ := cmd.Output()
	return string(out)
}

func truncate(s string, _ ...string) string {
	const max = 12000
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…(diff truncated)…"
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
