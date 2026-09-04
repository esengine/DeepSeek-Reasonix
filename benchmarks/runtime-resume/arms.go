package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
)

// arm is one death-and-resume run. Lever runs between the two processes and is
// what separates the three compaction questions: nothing changed, only the
// stable prefix changed, the covered conversation changed.
type arm struct {
	name  string
	asks  string
	lever func(armRoot, Observation) error
}

// appendsAfterFold names the arms whose construct phase adds one turn after
// the fold. Every arm carrying a lever needs it: without it the projection is
// already gone at the boundary for an unrelated reason, and the lever's own
// effect cannot be read out of the result.
func appendsAfterFold(name string) bool {
	return name != "exact"
}

// armAppendAfterFold separates the two ways a projection can fail validation
// after a restart: a covered prefix that no longer matches, and a projection
// that covers the whole transcript, whose only cross-process validation is a
// transcript version the reload does not carry.
const armAppendAfterFold = "append-after-fold"

func arms() []arm {
	return []arm{
		{name: "exact", asks: "nothing changed between the processes"},
		{name: armAppendAfterFold, asks: "nothing changed, and one turn was appended after the fold"},
		{name: "system-swap", asks: "only the stable prefix changed, against the surviving baseline", lever: swapSystemPrefix},
		{name: "covered-mutation", asks: "the covered conversation changed, against the surviving baseline", lever: mutateCoveredRow},
	}
}

// swapSystemPrefix saves a memory fact into this arm's home. The lever is a
// real user action rather than a poked field: the saved-fact index folds into
// the boot-time system prompt, so the next process composes a different one.
func swapSystemPrefix(root armRoot, _ Observation) error {
	store := memory.StoreFor(config.RootsForHome(root.Home).MemoryUserDir(), root.Workspace)
	_, err := store.Save(memory.Memory{
		Name:        "probe-prefix-lever",
		Title:       "Probe prefix lever",
		Description: "A fact saved between the two processes so the composed system prompt differs.",
		Type:        memory.TypeProject,
		Scope:       memory.FactScopeProject,
		// Pinned, not relevant: only a pinned body rides the stable prefix. The
		// saved-fact index is retrieval-only and would move nothing.
		Activation: memory.ActivationPinned,
		Body:       "The runtime-resume probe wrote this to move the stable prefix.",
	})
	return err
}

// mutateCoveredRow rewrites one provider-visible message inside the prefix the
// projection folded, through the same rewrite save a rewind or a recovery
// branch uses. This is the control arm: the digest's material changed, so the
// projection must not be reused.
func mutateCoveredRow(_ armRoot, before Observation) error {
	session, err := agent.LoadSession(before.SessionPath)
	if err != nil {
		return err
	}
	msgs := session.Snapshot()
	covered := min(before.Sidecar.CoveredCount, len(msgs))
	target := -1
	for i := range covered {
		if msgs[i].Role == provider.RoleUser || msgs[i].Role == provider.RoleAssistant {
			target = i
			break
		}
	}
	if target < 0 {
		return errUnexpected("a covered conversation row to mutate", covered)
	}
	msgs[target].Content += "\n[probe: covered row mutated]"
	session.Replace(msgs)
	return session.SaveRewrite(before.SessionPath)
}

func orchestrate(work, only, jsonOut string, keep bool) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if work == "" {
		work, err = os.MkdirTemp("", "runtime-resume-")
		if err != nil {
			return err
		}
		if !keep {
			defer os.RemoveAll(work)
		}
	}
	var results []armResult
	for _, a := range arms() {
		if only != "" && a.name != only {
			continue
		}
		result, err := runArm(self, work, a)
		if err != nil {
			return fmt.Errorf("arm %s: %w", a.name, err)
		}
		results = append(results, result)
	}
	if len(results) == 0 {
		return fmt.Errorf("no arm matched %q", only)
	}
	fmt.Print(renderMatrix(results, work))
	return writeJSON(jsonOut, results)
}

func runArm(self, work string, a arm) (armResult, error) {
	root := rootFor(filepath.Join(work, a.name))
	if err := root.requireFresh(); err != nil {
		return armResult{}, err
	}
	if err := root.create(); err != nil {
		return armResult{}, err
	}
	if err := spawn(self, "construct", root, a.name); err != nil {
		return armResult{}, fmt.Errorf("construct process: %w", err)
	}
	before, err := readObservation(root, "construct")
	if err != nil {
		return armResult{}, err
	}
	if a.lever != nil {
		if err := a.lever(root, before); err != nil {
			return armResult{}, fmt.Errorf("lever: %w", err)
		}
	}
	if err := spawn(self, "resume", root, a.name); err != nil {
		return armResult{}, fmt.Errorf("resume process: %w", err)
	}
	after, err := readObservation(root, "resume")
	if err != nil {
		return armResult{}, err
	}
	return classify(a, before, after), nil
}

// spawn runs one phase as a real child process and waits for it to exit. The
// OS process is the measurement boundary: a nil-ed object can still be revived
// by a package-level singleton, a live sink, or a cache, and none of those
// survive here.
func spawn(self, phase string, root armRoot, armName string) error {
	cmd := exec.Command(self, "-phase="+phase, "-root="+root.Dir, "-arm="+armName)
	cmd.Env = childEnv(root)
	cmd.Dir = root.Workspace
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writeJSON(path string, results []armResult) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	b, err := marshalResults(results)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
