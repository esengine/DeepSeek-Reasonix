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
	"unicode/utf8"
)

type diffOpts struct {
	bin, model, repo, base, testCmd string
	maxSteps, timeoutSec, attempts  int
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

// runDiff asks the agent to write tests covering what the PR changed, grades
// them against the repo's own tests, and — because the agent is stochastic —
// retries up to o.attempts times until a run passes, keeping the best result.
func runDiff(o diffOpts) string {
	srcFiles := changedGoFiles(o.repo, o.base, false)
	if len(srcFiles) == 0 {
		return "## 🤖 Reasonix e2e — diff test-gen\n\nNo Go source changes in this PR (excluding `_test.go`); nothing to generate tests for.\n"
	}
	pkgs := packagesOf(srcFiles)
	prompt := buildDiffPrompt(srcFiles, pkgs, truncate(gitOut(o.repo, "diff", o.base+"...HEAD", "--")))

	attempts := o.attempts
	if attempts < 1 {
		attempts = 1
	}
	var best diffReport
	made := 0
	for i := 1; i <= attempts; i++ {
		if i > 1 {
			resetTree(o.repo)
		}
		r := runOnce(o, srcFiles, pkgs, prompt)
		made = i
		if i == 1 || better(r, best) {
			best = r
		}
		if best.passed {
			break // stop at the first passing run; attempts is a retry budget
		}
	}
	best.attempt, best.attempts = made, attempts
	return renderDiff(best)
}

// runOnce does one agent run + grade: generate tests, check they pass on HEAD,
// differential-check each against the reverted source, measure changed-line
// coverage, and confirm the agent didn't break the build anywhere.
func runOnce(o diffOpts, srcFiles, pkgs []string, prompt string) diffReport {
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
	cmd.WaitDelay = 10 * time.Second // bound the wait for a wedged child after ctx timeout
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
	var mut mutationResult
	covered, coverTotal := 0, 0
	if len(refs) > 0 && testsPass {
		covered, coverTotal = changedLineCoverage(o.repo, o.base, pkgs, srcFiles)
		pins = differentialPerTest(o.repo, o.base, srcFiles, refs)
		mut = runMutation(o.repo, o.base, srcFiles, refs)
	}
	buildOK, buildOut := goBuildAll(o.repo)

	passed := len(refs) > 0 && testsPass && buildOK && countPins(pins) > 0
	return diffReport{
		srcFiles: srcFiles, pkgs: pkgs, addedTestLines: countAdded(testDiff),
		newTests: refs, sourceTouched: sourceTouched, testsPass: testsPass,
		pins: pins, mut: mut, covered: covered, coverTotal: coverTotal,
		buildOK: buildOK, buildOut: buildOut, failing: failingTestNames(testOut),
		passed: passed, m: m, runErr: runErr, testOut: testOut, testDiff: testDiff,
	}
}

// better reports whether candidate a is a stronger result than b: a pass beats a
// fail, then more assertion-pins, then more pins, then higher changed-line
// coverage.
func better(a, b diffReport) bool {
	if a.passed != b.passed {
		return a.passed
	}
	if x, y := countAssertionPins(a.pins), countAssertionPins(b.pins); x != y {
		return x > y
	}
	if x, y := countPins(a.pins), countPins(b.pins); x != y {
		return x > y
	}
	if a.mut.caught != b.mut.caught {
		return a.mut.caught > b.mut.caught
	}
	return ratio(a.covered, a.coverTotal) > ratio(b.covered, b.coverTotal)
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// resetTree restores the PR-head tree between attempts, dropping the previous
// attempt's generated tests but keeping the provider config the workflow wrote.
func resetTree(repo string) {
	_ = exec.Command("git", "-C", repo, "checkout", "--", ".").Run()
	_ = exec.Command("git", "-C", repo, "clean", "-fd", "-e", "reasonix.toml").Run()
}

func goBuildAll(repo string) (bool, string) {
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = repo
	cmd.WaitDelay = 2 * time.Minute // bound the wait if `go build` hangs
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
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
	srcFiles, pkgs      []string
	addedTestLines      int
	newTests            []testRef
	sourceTouched       int
	testsPass           bool
	pins                []pinResult
	mut                 mutationResult
	covered, coverTotal int
	buildOK             bool
	buildOut            string
	failing             []string
	passed              bool
	attempt, attempts   int
	m                   runMetrics
	runErr              error
	testOut             string
	testDiff            string
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
	fmt.Fprintf(&b, "| Changed-line coverage | %s |\n", coverageCell(r))
	fmt.Fprintf(&b, "| Mutation (changed funcs caught) | %s |\n", mutationCell(r))
	fmt.Fprintf(&b, "| `go build ./...` (regression) | %s |\n", passFail(r.buildOK))
	fmt.Fprintf(&b, "| Non-test source touched by agent | %d file(s) |\n", r.sourceTouched)
	fmt.Fprintf(&b, "| Cache hit | %s |\n", pct(r.m.CacheHitTokens, r.m.CacheHitTokens+r.m.CacheMissTokens))
	fmt.Fprintf(&b, "| Tokens (prompt / completion) | %s / %s |\n", comma(r.m.PromptTokens), comma(r.m.CompletionTokens))
	fmt.Fprintf(&b, "| Model calls | %d |\n", r.m.Steps)
	fmt.Fprintf(&b, "| Cost | %s%.4f |\n", currencySym(r.m.Currency), r.m.Cost)
	if len(r.failing) > 0 {
		fmt.Fprintf(&b, "| Failing tests | `%s` |\n", strings.Join(r.failing, "`, `"))
	}
	if r.attempts > 1 {
		status := "none passed"
		if r.passed {
			status = "passed"
		}
		fmt.Fprintf(&b, "| Attempts | %d of up to %d (%s) |\n", r.attempt, r.attempts, status)
	}

	fmt.Fprintf(&b, "\n**Packages:** %s\n", strings.Join(r.pkgs, ", "))
	if r.attempts <= 1 {
		fmt.Fprintf(&b, "\n<sub>Single stochastic run — a green result is one sample, not a guarantee. Comment `/e2e diff x3` to retry up to 3×.</sub>\n")
	}
	if !r.buildOK && strings.TrimSpace(r.buildOut) != "" {
		fmt.Fprintf(&b, "\n<details><summary>go build ./... output (tail)</summary>\n\n```\n%s\n```\n</details>\n", tail(r.buildOut, 40))
	}
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
	fmt.Fprintf(&b, "\n<sub>Pass = the agent added ≥1 test, the affected packages are green, AND ≥1 new test fails when the PR's source is reverted. \"By assertion\" pins are strong (they check changed behavior); \"by compile only\" pins just need a PR-added symbol — and since Go compiles per package, one compile-coupled test marks every test in its package that way. Mutation is the behavioral signal for additive PRs: each changed function's return is replaced with zero values and the new tests are re-run; \"caught\" means a test asserts that output, \"survived\" means it doesn't. Read the generated tests above to judge the rest.</sub>\n")
	return b.String()
}

func differentialCell(r diffReport) string {
	if !(len(r.newTests) > 0 && r.testsPass) {
		return "n/a (tests not green)"
	}
	return fmt.Sprintf("%d/%d new tests", countPins(r.pins), len(r.pins))
}

func coverageCell(r diffReport) string {
	if r.coverTotal == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%s (%d/%d changed lines)", pct(r.covered, r.coverTotal), r.covered, r.coverTotal)
}

func mutationCell(r diffReport) string {
	if r.mut.total == 0 {
		return "n/a"
	}
	cell := fmt.Sprintf("%d/%d (%s)", r.mut.caught, r.mut.total, pct(r.mut.caught, r.mut.total))
	if len(r.mut.survivors) > 0 {
		cell += fmt.Sprintf(" · survived: `%s`", strings.Join(r.mut.survivors, "`, `"))
	}
	return cell
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
	// Restore source even on panic; a tree left on `base` would mask the PR for later steps.
	restored := false
	defer func() {
		if restored {
			return
		}
		for _, f := range srcFiles {
			_ = exec.Command("git", "-C", repo, "checkout", "HEAD", "--", f).Run()
		}
	}()

	out := make([]pinResult, 0, len(refs))
	for _, r := range refs {
		cmd := exec.Command("go", "test", "-run", "^"+r.name+"$", r.pkg)
		cmd.Dir = repo
		cmd.WaitDelay = 2 * time.Minute // bound the wait for a hung test
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
	restored = true
	return out
}

// changedLineCoverage runs the affected packages with a coverage profile and
// reports how many of the PR's changed source statement-lines the (new+existing)
// tests actually execute. covered/total are over changed lines that fall inside
// a coverage block; lines that aren't statements are ignored.
func changedLineCoverage(repo, base string, pkgs, srcFiles []string) (covered, total int) {
	profile := filepath.Join(repo, ".e2e-cover.out")
	defer os.Remove(profile)
	args := append([]string{"test", "-covermode=set", "-coverprofile=" + profile, "-coverpkg=" + strings.Join(pkgs, ",")}, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = repo
	_ = cmd.Run() // a non-zero exit still writes the profile for the tests that ran

	blocks := parseCoverProfile(repo, profile)
	for file, lines := range changedLineSet(repo, base, srcFiles) {
		fileBlocks := blocks[file]
		for ln := range lines {
			for _, blk := range fileBlocks {
				if ln >= blk.start && ln <= blk.end {
					total++
					if blk.count > 0 {
						covered++
					}
					break
				}
			}
		}
	}
	return covered, total
}

type coverBlock struct {
	start, end, count int
}

// parseCoverProfile reads a Go coverage profile, keyed by repo-relative file path
// (the profile uses module-qualified paths; we match by repo-relative suffix).
func parseCoverProfile(repo, path string) map[string][]coverBlock {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string][]coverBlock{}
	for _, ln := range strings.Split(string(data), "\n") {
		if ln == "" || strings.HasPrefix(ln, "mode:") {
			continue
		}
		colon := strings.LastIndexByte(ln, ':')
		if colon < 0 {
			continue
		}
		modPath, rest := ln[:colon], ln[colon+1:]
		var sl, sc, el, ec, nstmt, count int
		if _, err := fmt.Sscanf(rest, "%d.%d,%d.%d %d %d", &sl, &sc, &el, &ec, &nstmt, &count); err != nil {
			continue
		}
		rel := repoRelFromModulePath(modPath)
		out[rel] = append(out[rel], coverBlock{start: sl, end: el, count: count})
	}
	return out
}

// repoRelFromModulePath turns "reasonix/internal/agent/foo.go" into
// "internal/agent/foo.go" by dropping the first path element (the module root).
func repoRelFromModulePath(p string) string {
	// Strip the full module prefix; a generic first-segment cut mis-strips a multi-segment module path.
	prefix := "reasonix/"
	if strings.HasPrefix(p, prefix) {
		return p[len(prefix):]
	}
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// changedLineSet returns, per repo-relative source file, the set of new line
// numbers the PR added or changed (from a zero-context diff).
func changedLineSet(repo, base string, srcFiles []string) map[string]map[int]bool {
	args := append([]string{"diff", "--unified=0", base + "...HEAD", "--"}, srcFiles...)
	diff := gitOut(repo, args...)
	out := map[string]map[int]bool{}
	file := ""
	newLine := 0
	for _, ln := range strings.Split(diff, "\n") {
		// '-' (deletion) lines are intentionally unhandled: they don't advance the
		// new-side line counter, so they fall through with no case.
		switch {
		case strings.HasPrefix(ln, "+++ b/"):
			file = strings.TrimPrefix(ln, "+++ b/")
			out[file] = map[int]bool{}
		case strings.HasPrefix(ln, "@@"):
			// @@ -a,b +c,d @@ — start collecting at new-side line c.
			// Digit-only cut: malformed headers (e.g. `@@ +abc @@`) fail closed.
			if plus := strings.Index(ln, "+"); plus >= 0 {
				num := ln[plus+1:]
				end := len(num)
				for i := 0; i < len(num); i++ {
					if num[i] < '0' || num[i] > '9' {
						end = i
						break
					}
				}
				_, _ = fmt.Sscanf(num[:end], "%d", &newLine)
			}
		case strings.HasPrefix(ln, "+") && !strings.HasPrefix(ln, "+++"):
			if file != "" {
				out[file][newLine] = true
			}
			newLine++
		}
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
		// Method form `(r T) Name(...)` starts with '('; parse the receiver out before the name.
		var name string
		if sig[0] == '(' {
			close := strings.IndexByte(sig, ')')
			if close < 0 {
				continue
			}
			rest := strings.TrimSpace(sig[close+1:])
			methodParen := strings.IndexByte(rest, '(')
			if methodParen <= 0 {
				continue
			}
			name = rest[:methodParen]
		} else {
			funcParen := strings.IndexByte(sig, '(')
			if funcParen <= 0 {
				continue
			}
			name = sig[:funcParen]
		}
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
	fields := splitShellFields(testCmd)
	if len(fields) == 0 {
		fields = []string{"go", "test"}
	}
	args := append(fields[1:], pkgs...)
	cmd := exec.Command(fields[0], args...)
	cmd.Dir = repo
	cmd.WaitDelay = 5 * time.Minute // bound the wait if `go test` hangs
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}

// splitShellFields splits on whitespace, honoring single/double-quoted spans and stripping the quotes.
func splitShellFields(s string) []string {
	var out []string
	var cur strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			} else {
				cur.WriteByte(c)
			}
		case inDouble:
			if c == '"' {
				inDouble = false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case c == ' ' || c == '\t' || c == '\n':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
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
	if max <= 0 || len(s) <= max {
		return s
	}
	// Back the cut up to a rune boundary so we don't split a multi-byte UTF-8 rune.
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n…(truncated)…"
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
