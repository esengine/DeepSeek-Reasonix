package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
)

// forkRow is one continuation run from a frozen eligibility state: the pair
// analysis unit. Post-fork metrics need no alignment — the whole run IS the
// post-trigger tail, so its totals are Remaining-Cost-From-Eligibility.
type forkRow struct {
	Task           string `json:"task"`
	Class          string `json:"class"`
	Arm            string `json:"arm"`
	Rep            int    `json:"rep"`
	Passed         bool   `json:"passed"`
	WallMs         int64  `json:"wall_ms"`
	Rounds         int    `json:"rounds"`
	ReasoningTok   int64  `json:"reasoning_tok"`
	ExtraBlind     int    `json:"extra_blind"`
	FirstCheckRnds int    `json:"first_check_rounds"` // 0 = never
	BlindAtFork    int    `json:"blind_at_fork"`
	DebtAtFork     int    `json:"debt_at_fork"`
	EligibleRound  int    `json:"eligible_round"`
}

// runForkMode replays every captured bundle under the requested arms. Arms
// alternate order per rep so provider drift over the session cannot
// systematically favor one arm.
func runForkMode(bundlesDir, suite, arms string, reps int, cfg suiteConfig, trajDir, outMD, outJSON string) error {
	entries, err := os.ReadDir(bundlesDir)
	if err != nil {
		return err
	}
	tasks, err := loadTasks(suite)
	if err != nil {
		return err
	}
	byID := map[string]task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}
	armList := strings.Split(arms, ",")
	var rows []forkRow
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		bdir := filepath.Join(bundlesDir, e.Name())
		bundle, err := agent.LoadForkBundle(filepath.Join(bdir, "bundle.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", e.Name(), err)
			continue
		}
		t, ok := byID[e.Name()]
		if !ok {
			fmt.Fprintf(os.Stderr, "skip %s: no such task in suite\n", e.Name())
			continue
		}
		for rep := 1; rep <= reps; rep++ {
			order := append([]string(nil), armList...)
			if rep%2 == 0 {
				sort.Sort(sort.Reverse(sort.StringSlice(order)))
			}
			for _, arm := range order {
				row, err := runForkContinuation(cfg, t, bundle, bdir, arm, rep, trajDir)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s/%s rep %d: %v\n", e.Name(), arm, rep, err)
					continue
				}
				rows = append(rows, row)
				fmt.Fprintf(os.Stderr, "fork %s %s rep%d: pass=%v wall=%s rounds=%d check@%d\n",
					row.Task, row.Arm, rep, row.Passed, dur(row.WallMs), row.Rounds, row.FirstCheckRnds)
			}
		}
	}
	report := renderForkReport(rows)
	emit(report, outMD, "")
	if outJSON != "" {
		data, _ := json.MarshalIndent(rows, "", " ")
		if err := os.WriteFile(outJSON, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func runForkContinuation(cfg suiteConfig, t task, b *agent.ForkBundle, bdir, arm string, rep int, trajDir string) (forkRow, error) {
	row := forkRow{Task: t.ID, Class: t.Class, Arm: arm, Rep: rep,
		BlindAtFork: b.BlindAtFork, DebtAtFork: b.DebtAtFork, EligibleRound: b.EligibleRound}
	work, err := os.MkdirTemp("", "fork-"+t.ID+"-")
	if err != nil {
		return row, err
	}
	defer os.RemoveAll(work)
	if err := copyDir(filepath.Join(bdir, "workspace"), work); err != nil {
		return row, err
	}
	trajPath := ""
	if trajDir != "" {
		trajPath = filepath.Join(trajDir, fmt.Sprintf("%s.%s.r%d.trajectory.jsonl", t.ID, arm, rep))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(t.TimeoutSec)*time.Second)
	defer cancel()
	// Governor arms: low-effort cuts the whole continuation's thinking via
	// the provider knob; act-first injects the shaping line instead. Both use
	// the same frozen state as control.
	runCfg, forkEnvArm := cfg, arm
	switch arm {
	case "low-effort":
		runCfg.effort = "low"
		forkEnvArm = "control"
	case "act-first":
		forkEnvArm = "actfirst"
	}
	args := buildRunTaskArgs(runCfg, filepath.Join(work, ".run-metrics.json"), trajPath, t.MaxSteps, b.Input)
	cmd := exec.CommandContext(ctx, cfg.bin, args...)
	cmd.Dir = work
	cmd.Env = append(os.Environ(),
		"REASONIX_EXPERIMENT_FORK_BUNDLE="+filepath.Join(bdir, "bundle.json"),
		"REASONIX_EXPERIMENT_FORK_ARM="+forkEnvArm)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 10 * time.Second
	started := time.Now()
	_ = cmd.Run()
	row.WallMs = time.Since(started).Milliseconds()
	row.Passed = grade(work, t.dir)
	if trajPath != "" {
		if scan, err := scanTrajectoryFile(trajPath); err == nil {
			s := scan.finish()
			row.Rounds = len(scan.outcomePoints)
			row.ReasoningTok = s.ReasoningTokensTotal
			for i, p := range scan.outcomePoints {
				if p.discriminating > 0 {
					row.FirstCheckRnds = i + 1
					break
				}
				row.ExtraBlind += p.churn
			}
		}
	}
	return row, nil
}

// renderForkReport is deliberately per-pair first, per-class second, and never
// one blended score: the arms answer different questions per class.
func renderForkReport(rows []forkRow) string {
	var b strings.Builder
	b.WriteString("## Fork continuation report\n\n")
	b.WriteString("| task | class | arm | rep | pass | wall | rounds | reasoning | extra blind | check@ |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		check := "never"
		if r.FirstCheckRnds > 0 {
			check = fmt.Sprint(r.FirstCheckRnds)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %v | %s | %d | %s | %d | %s |\n",
			r.Task, r.Class, r.Arm, r.Rep, r.Passed, dur(r.WallMs), r.Rounds,
			comma(int(r.ReasoningTok)), r.ExtraBlind, check)
	}
	type agg struct {
		n, pass, checks, extraBlind int
		wall, reasoning             int64
	}
	byClassArm := map[string]*agg{}
	for _, r := range rows {
		key := r.Class + "/" + r.Arm
		a := byClassArm[key]
		if a == nil {
			a = &agg{}
			byClassArm[key] = a
		}
		a.n++
		if r.Passed {
			a.pass++
		}
		if r.FirstCheckRnds > 0 {
			a.checks++
		}
		a.extraBlind += r.ExtraBlind
		a.wall += r.WallMs
		a.reasoning += r.ReasoningTok
	}
	keys := make([]string, 0, len(byClassArm))
	for k := range byClassArm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("\n| class/arm | n | pass | checked | avg wall | avg reasoning | avg extra blind |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, k := range keys {
		a := byClassArm[k]
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s | %.1f |\n",
			k, a.n, pct(a.pass, a.n), pct(a.checks, a.n),
			dur(a.wall/int64(a.n)), comma(int(a.reasoning/int64(a.n))), float64(a.extraBlind)/float64(a.n))
	}
	return b.String()
}
