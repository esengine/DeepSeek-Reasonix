package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

// ForkBundle freezes the full turn state at a policy's first eligibility so a
// control and a treatment continuation can start from the identical point.
// Versioned from day one: this format is shared infrastructure for every
// policy experiment (EBM, reasoning governor, delegation admission, rollback).
type ForkBundle struct {
	Version       int                `json:"version"`
	Policy        string             `json:"policy"`
	Input         string             `json:"input"`
	EligibleRound int                `json:"eligible_round"`
	BlindAtFork   int                `json:"blind_at_fork"`
	DebtAtFork    int                `json:"debt_at_fork"`
	MutatedBases  []string           `json:"mutated_bases,omitempty"`
	LocalExecSeen bool               `json:"local_exec_seen,omitempty"`
	Messages      []provider.Message `json:"messages"`
}

const forkBundleVersion = 1

// armForkCapture marks first EBM eligibility for capture. The bundle is
// written by the provider wrapper right before the NEXT request — only then
// does the session hold the eligible round's tool results. Arming refuses
// under live enforcement: a treated state must never become a bundle.
func (a *Agent) armForkCapture(sample evidence.OutcomeSample) {
	if forkCapturePolicy() != "ebm" || ebmEnabled || a.task.ebm.captured || a.task.ebm.captureArmed {
		return
	}
	a.task.ebm.captureArmed = true
	a.task.ebm.captureRound = sample.Round
}

// govReasoningThreshold marks a round's thinking as expensive enough that a
// governor experiment wants the state frozen before the next purchase.
const govReasoningThreshold = 1500

// armGovernorCapture freezes the exploration-phase state where the reasoning
// governor would intervene — the same governorTrigger the live policy reads,
// so experiments fork exactly the states enforcement would treat. Refuses
// under live enforcement: a treated state must never become a bundle.
func (a *Agent) armGovernorCapture(sample evidence.OutcomeSample) {
	if forkCapturePolicy() != "governor" || governorEnabled || a.task.ebm.captured || a.task.ebm.captureArmed {
		return
	}
	if !governorTrigger(sample, a.turn.lastReasoning) {
		return
	}
	a.task.ebm.captureArmed = true
	a.task.ebm.captureRound = sample.Round
}

// forkCapturePolicy selects which policy's trigger owns bundle capture;
// unset defaults to the EBM trigger for compatibility with existing runs.
func forkCapturePolicy() string {
	if os.Getenv("REASONIX_EXPERIMENT_FORK_CAPTURE_DIR") == "" {
		return ""
	}
	if p := os.Getenv("REASONIX_EXPERIMENT_FORK_POLICY"); p != "" {
		return p
	}
	return "ebm"
}

// forkCaptureProvider snapshots the session at the next Stream call after
// eligibility was armed — the exact state the uninterrupted run sends.
type forkCaptureProvider struct {
	inner provider.Provider
	a     *Agent
}

func (p *forkCaptureProvider) Name() string { return p.inner.Name() }

func (p *forkCaptureProvider) OutputBudget() int { return outputBudgetOf(p.inner) }

func (p *forkCaptureProvider) SharesContextWindow() bool { return sharesContextWindow(p.inner) }

func (p *forkCaptureProvider) SharedWindowInputPolicy() provider.SharedWindowInputPolicy {
	return sharedWindowInputPolicyOf(p.inner)
}

func (p *forkCaptureProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	a := p.a
	if a.task.ebm.captureArmed && !a.task.ebm.captured {
		a.task.ebm.captured = true
		messages := a.sess.conversation.Snapshot()
		seed := a.task.outcome.ForkSeed()
		b := ForkBundle{
			Version: forkBundleVersion, Policy: forkCapturePolicy(),
			Input:         forkTurnInput(messages),
			EligibleRound: a.task.ebm.captureRound, BlindAtFork: seed.BlindMutations,
			DebtAtFork: seed.DebtAge, MutatedBases: seed.MutatedBases,
			LocalExecSeen: seed.LocalExecSeen, Messages: messages,
		}
		if err := writeForkBundle(os.Getenv("REASONIX_EXPERIMENT_FORK_CAPTURE_DIR"), b); err != nil {
			fmt.Fprintln(os.Stderr, "fork capture:", err)
		}
	}
	return p.inner.Stream(ctx, req)
}

// forkTurnInput recovers the turn's raw input from the frozen conversation:
// the first user message's authored form. Single-turn scope (the bench runs
// one turn per task); multi-turn capture would need the active turn's index.
func forkTurnInput(messages []provider.Message) string {
	for _, m := range messages {
		if m.Role == provider.RoleUser {
			if m.RawContent != "" {
				return m.RawContent
			}
			return m.Content
		}
	}
	return ""
}

func writeForkBundle(dir string, b ForkBundle) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "bundle.json"), data, 0o644); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return copyWorkspace(cwd, filepath.Join(dir, "workspace"))
}

// copyWorkspace mirrors the task workdir minus harness artifacts, so a
// continuation starts from byte-identical files.
func copyWorkspace(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil || rel == "." {
			return rerr
		}
		name := d.Name()
		if d.IsDir() {
			if name == "__pycache__" || name == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if name == ".run-metrics.json" {
			return nil
		}
		in, oerr := os.Open(path)
		if oerr != nil {
			return oerr
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dst, rel)), 0o755); err != nil {
			return err
		}
		out, cerr := os.Create(filepath.Join(dst, rel))
		if cerr != nil {
			return cerr
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

// LoadForkBundle reads and version-checks a bundle.
func LoadForkBundle(path string) (*ForkBundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b ForkBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	if b.Version != forkBundleVersion {
		return nil, fmt.Errorf("fork bundle version %d, this build replays %d", b.Version, forkBundleVersion)
	}
	return &b, nil
}

// actFirstNudge is the reasoning-governor's soft treatment: shaping, not
// capping — spend cheap external evidence before expensive speculation.
const actFirstNudge = "[guidance] Prefer cheap repository evidence or a targeted check over extended " +
	"speculation when either can reduce uncertainty."

// armForkContinuation makes the next Run continue from the bundle: turn-local
// classification still runs on the same input, then the appended user message
// is replaced wholesale by the frozen conversation. A non-empty nudge is the
// arm's single treatment, placed in the live policy's slot; the dose disarms
// every runtime policy for the continuation.
func (a *Agent) armForkContinuation(b *ForkBundle, nudge string) {
	a.pending.forkRestore = func(_ *turnRuntime) {
		messages := append([]provider.Message(nil), b.Messages...)
		if nudge != "" {
			applyForkTreatment(messages, nudge)
		}
		a.sess.conversation.Replace(messages)
		a.task.outcome = evidence.RestoreOutcomeTracker(evidence.OutcomeSeed{
			MutatedBases: b.MutatedBases, DebtAge: b.DebtAtFork,
			BlindMutations: b.BlindAtFork, LocalExecSeen: b.LocalExecSeen,
		})
		a.task.ebm = ebmState{fired: true, captured: true, captureRound: b.EligibleRound}
	}
}

// applyForkTreatment appends the nudge to the eligible batch's first tool
// result — the same slot the live policy writes to.
func applyForkTreatment(messages []provider.Message, nudge string) {
	lastAssistant := -1
	for i, m := range messages {
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			lastAssistant = i
		}
	}
	for i := lastAssistant + 1; lastAssistant >= 0 && i < len(messages); i++ {
		if messages[i].Role == provider.RoleTool {
			messages[i].Content += "\n\n" + nudge
			return
		}
	}
}

// maybeWrapForkCaptureProvider interposes the capture wrapper when the
// experiment env asks for bundles; inert otherwise.
func (a *Agent) maybeWrapForkCaptureProvider() {
	if os.Getenv("REASONIX_EXPERIMENT_FORK_CAPTURE_DIR") != "" && a.prov != nil {
		a.prov = &forkCaptureProvider{inner: a.prov, a: a}
	}
}

// maybeArmForkFromEnv wires the experiment from the environment so the bench
// can fork without new public plumbing. Control is the default arm.
func (a *Agent) maybeArmForkFromEnv() {
	path := os.Getenv("REASONIX_EXPERIMENT_FORK_BUNDLE")
	if path == "" {
		return
	}
	b, err := LoadForkBundle(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fork continuation:", err)
		return
	}
	nudge := ""
	switch os.Getenv("REASONIX_EXPERIMENT_FORK_ARM") {
	case "treatment":
		nudge = ebmNudge
	case "actfirst":
		nudge = actFirstNudge
	}
	a.armForkContinuation(b, nudge)
}
