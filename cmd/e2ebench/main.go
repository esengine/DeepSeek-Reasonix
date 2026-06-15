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
)

type task struct {
	ID         string
	Prompt     string `toml:"prompt"`
	MaxSteps   int    `toml:"max_steps"`
	TimeoutSec int    `toml:"timeout_sec"`
	dir        string
}

type runMetrics struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CacheHitTokens   int     `json:"cache_hit_tokens"`
	CacheMissTokens  int     `json:"cache_miss_tokens"`
	Steps            int     `json:"steps"`
	Cost             float64 `json:"cost"`
	Currency         string  `json:"currency"`
	Compactions      int     `json:"compactions"`
}

type result struct {
	task
	runMetrics
	Passed  bool
	Skipped bool
	Note    string
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "e2ebench — Reasonix end-to-end benchmark.\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", flag.CommandLine.Name())
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), "\nExamples:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  # Run the committed suite:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  %[1]s\n\n", strings.Replace(flag.CommandLine.Name(), "e2ebench", "go run ./cmd/e2ebench", 1))
		fmt.Fprintf(flag.CommandLine.Output(), "  # Grade a PR's diff with a retry budget:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  %[1]s -mode diff -base origin/main -repo . -attempts 3 -timeout 1800\n", strings.Replace(flag.CommandLine.Name(), "e2ebench", "go run ./cmd/e2ebench", 1))
	}

	mode := flag.String("mode", "suite", "suite | diff (diff = generate tests for the PR diff and grade with the repo's tests)")
	suite := flag.String("suite", "benchmarks/e2e", "suite root (contains tasks/<id>/)")
	bin := flag.String("bin", "reasonix", "path to the reasonix binary")
	model := flag.String("model", "", "provider/model name (default: config default)")
	outMD := flag.String("out", "", "write the markdown report here (default: stdout)")
	outJSON := flag.String("json", "", "write the JSON report here (optional)")
	budget := flag.Int("budget", 400_000, "abort once total tokens cross this (0 = no cap)")
	// diff-mode flags
	repo := flag.String("repo", ".", "repo root (diff mode)")
	base := flag.String("base", "", "base ref to diff the PR head against (diff mode)")
	testCmd := flag.String("test-cmd", "go test", "grader command run on the affected packages (diff mode)")
	maxSteps := flag.Int("max-steps", 80, "agent tool-call cap for the diff task")
	timeoutSec := flag.Int("timeout", 1200, "agent timeout in seconds (diff mode)")
	attempts := flag.Int("attempts", 1, "diff mode: retry up to N times until a run passes (stochastic agent)")
	flag.Parse()

	if *mode == "diff" {
		report := runDiff(diffOpts{
			bin: *bin, model: *model, repo: *repo, base: *base,
			testCmd: *testCmd, maxSteps: *maxSteps, timeoutSec: *timeoutSec, attempts: *attempts,
		})
		emit(report, *outMD, "")
		return
	}

	tasks, err := loadTasks(*suite)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load suite:", err)
		os.Exit(1)
	}
	if len(tasks) == 0 {
		dir := filepath.Join(*suite, "tasks")
		if _, statErr := os.Stat(dir); statErr != nil {
			fmt.Fprintf(os.Stderr, "no tasks found under %s: %v\n", dir, statErr)
		} else {
			fmt.Fprintf(os.Stderr, "no tasks found under %s (the directory exists but contains no task.toml files)\n", dir)
		}
		os.Exit(1)
	}

	var results []result
	total := 0
	for _, t := range tasks {
		if *budget > 0 && total >= *budget {
			results = append(results, result{task: t, Skipped: true, Note: "skipped: token budget reached"})
			continue
		}
		r := runTask(*bin, *model, t)
		total += r.PromptTokens + r.CompletionTokens
		results = append(results, r)
	}

	report := render(results)
	if *outMD != "" {
		if err := os.WriteFile(*outMD, []byte(report), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write report:", err)
			os.Exit(1)
		}
	} else {
		fmt.Print(report)
	}
	if *outJSON != "" {
		b, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "marshal json:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*outJSON, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write json:", err)
			os.Exit(1)
		}
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
		if _, err := toml.DecodeFile(filepath.Join(dir, "task.toml"), &t); err != nil {
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

// runTask copies the task's seed workdir into a temp dir, runs the agent there,
// then drops in verify.sh and runs it as the grader. The grader is added only
// after the run so the agent can't read the answer key.
func runTask(bin, model string, t task) result {
	r := result{task: t}

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

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(t.TimeoutSec)*time.Second)
	defer cancel()

	metricsPath := filepath.Join(work, ".run-metrics.json")
	args := []string{"run", "--metrics", metricsPath}
	if model != "" {
		args = append(args, "--model", model)
	}
	if t.MaxSteps > 0 {
		args = append(args, "--max-steps", fmt.Sprint(t.MaxSteps))
	}
	args = append(args, t.Prompt)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = work
	cmd.Stdout = os.Stderr // stream the run to the job log, keep stdout clean for the report
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 10 * time.Second // bound the wait for a stuck child after ctx timeout
	runErr := cmd.Run()

	if m, err := readMetrics(metricsPath); err == nil {
		r.runMetrics = m
	}
	if runErr != nil {
		r.Note = "run: " + runErr.Error()
		// still grade — a non-zero exit may just be a max-steps notice
	}

	r.Passed = grade(work, t.dir)
	return r
}

func grade(work, taskDir string) bool {
	verify := filepath.Join(taskDir, "verify.sh")
	if !fileExists(verify) {
		return false
	}
	dst := filepath.Join(work, "verify.sh")
	if err := copyFile(verify, dst); err != nil {
		return false
	}
	cmd := exec.Command("bash", "verify.sh")
	cmd.Dir = work
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run() == nil
}

func render(results []result) string {
	var b strings.Builder
	passed, ran := 0, 0
	var pTok, cTok, hit, miss, compacts int
	var cost float64
	currency := ""
	for _, r := range results {
		if r.Skipped {
			continue
		}
		ran++
		if r.Passed {
			passed++
		}
		pTok += r.PromptTokens
		cTok += r.CompletionTokens
		hit += r.CacheHitTokens
		miss += r.CacheMissTokens
		compacts += r.Compactions
		cost += r.Cost
		if r.Currency != "" {
			currency = r.Currency
		}
	}

	fmt.Fprintf(&b, "## 🤖 Reasonix e2e benchmark\n\n")
	fmt.Fprintf(&b, "**Accuracy:** %d/%d (%s) · **Cache hit:** %s · **Tokens:** %s (prompt %s / completion %s) · **Compactions:** %d · **Cost:** %s%.4f\n\n",
		passed, ran, pct(passed, ran), pct(hit, hit+miss),
		comma(pTok+cTok), comma(pTok), comma(cTok), compacts, currencySym(currency), cost)

	fmt.Fprintf(&b, "| Task | Result | Steps | Prompt | Completion | Cache hit | Compact | Cost |\n")
	fmt.Fprintf(&b, "|------|--------|------:|-------:|-----------:|----------:|--------:|-----:|\n")
	for _, r := range results {
		switch {
		case r.Skipped:
			fmt.Fprintf(&b, "| `%s` | ⏭️ skipped | — | — | — | — | — | — |\n", r.ID)
		default:
			res := "❌ fail"
			if r.Passed {
				res = "✅ pass"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %d | %s | %s | %s | %d | %s%.4f |\n",
				r.ID, res, r.Steps, comma(r.PromptTokens), comma(r.CompletionTokens),
				pct(r.CacheHitTokens, r.CacheHitTokens+r.CacheMissTokens),
				r.Compactions, currencySym(r.Currency), r.Cost)
		}
	}
	fmt.Fprintf(&b, "\n<sub>Real provider run. Cache-hit %% is cached prompt tokens / total prompt tokens.</sub>\n")

	notes := false
	for _, r := range results {
		if r.Note != "" {
			if !notes {
				fmt.Fprintf(&b, "\n<details><summary>Notes</summary>\n\n")
				notes = true
			}
			fmt.Fprintf(&b, "- `%s`: %s\n", r.ID, r.Note)
		}
	}
	if notes {
		fmt.Fprintf(&b, "\n</details>\n")
	}
	return b.String()
}

func pct(n, d int) string {
	if d == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(d))
}

func comma(n int) string {
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func currencySym(c string) string {
	if c == "" {
		return ""
	}
	return c + " "
}

func readMetrics(path string) (runMetrics, error) {
	var m runMetrics
	b, err := os.ReadFile(path)
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
