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
	return name != "exact" && name != armRefoldIntoBody && !graphArm(name) && !uiArm(name) &&
		!schedulerWaitArm(name) && !terminalArm(name) && !deriveArm(name) &&
		!loneTaskArm(name) && !cancelRoutingArm(name)
}

// armAppendAfterFold separates the two ways a projection can fail validation
// after a restart: a covered prefix that no longer matches, and a projection
// that covers the whole transcript, whose only cross-process validation is a
// transcript version the reload does not carry.
const armAppendAfterFold = "append-after-fold"

// armRefoldIntoBody folds twice. The second fold reads the provider-visible
// view — stored body plus live tail — and must record a boundary in canonical
// terms, which the body has no counterpart for.
const armRefoldIntoBody = "refold-into-body"

// The tail arms separate two ways the live tail can move. tail-edit and
// tail-truncate change the transcript past the fold, where the covered hash
// cannot notice; tail-rewind takes the path a person actually drives, which
// throws the projection away without asking whether coverage still holds.
const (
	armTailEdit     = "tail-edit"
	armTailTruncate = "tail-truncate"
	armTailRewind   = "tail-rewind"
	// armCoveredRewind is the negative control the rewind path will need before
	// its invalidation can become conditional: rewinding below the fold removes
	// history the digest folded, and no coverage check may excuse that.
	armCoveredRewind = "covered-rewind"
	// armTodoIdentity moves host step identity while a fold is between the
	// model and it: identity A, fold, live tail, identity B, fold again.
	armTodoIdentity = "todo-identity"
	// armOpenDecision dies while a question is open. It asks what the host
	// believes afterwards, not whether one particular card comes back.
	armOpenDecision = "open-decision"
	// The three successors to an interrupted barrier. Each starts where
	// open-decision ends and differs only in what the next process does, which
	// is the whole question: when does a recorded interruption stop being the
	// context every later request carries?
	armIdleRestart   = "interrupted-idle"
	armUnrelatedTurn = "interrupted-unrelated"
	armDirectAnswer  = "interrupted-answered"
)

// successorTurn is what the middle process says, empty when it only restarts.
func successorTurn(arm string) (string, bool) {
	switch arm {
	case armIdleRestart:
		return "", true
	case armUnrelatedTurn:
		return marker(120) + " List the files you would look at first.", true
	case armDirectAnswer:
		return marker(121) + " Yes, go ahead with that — take the side below the fold.", true
	}
	return "", false
}

func arms() []arm {
	return []arm{
		{name: "exact", asks: "nothing changed between the processes"},
		{name: armAppendAfterFold, asks: "nothing changed, and one turn was appended after the fold"},
		{name: armRefoldIntoBody, asks: "a second fold reaches into the body the first one stored"},
		{name: "system-swap", asks: "only the stable prefix changed, against the surviving baseline", lever: swapSystemPrefix},
		{name: armTailEdit, asks: "a message past the fold changed", lever: editTailRow},
		{name: armTailTruncate, asks: "the messages past the fold were dropped", lever: truncateTail},
		{name: armTailRewind, asks: "the same truncation, driven through the rewind a person uses"},
		{name: armCoveredRewind, asks: "a rewind that lands below the fold boundary"},
		{name: armTodoIdentity, asks: "host step identity moved across two folds and a live tail"},
		{name: armOpenDecision, asks: "the process died while a decision was open"},
		{name: armIdleRestart, asks: "an interrupted barrier, and nobody does anything"},
		{name: armUnrelatedTurn, asks: "an interrupted barrier, then a turn about something else"},
		{name: armDirectAnswer, asks: "an interrupted barrier, then a turn that reads like an answer to it"},
		{name: armGraphCompleted, asks: "a fan-out whose items had all settled when the process died"},
		{name: armGraphRunning, asks: "a fan-out with a child still executing when the process died"},
		{name: armGraphMixed, asks: "a fan-out holding completed, failed, adopted and running at once"},
		{name: armUIGraphMixed, asks: "which door each fact reaches the frontend through, after a death"},
		{name: armTaskCompleted, asks: "a lone delegation that finished, in a turn that closed"},
		{name: armTaskRunning, asks: "a lone delegation still executing when the process died"},
		{name: armTaskFgQueued, asks: "what a lone delegation is drawn as while the scheduler holds it back"},
		{name: armTaskBgQueued, asks: "a backgrounded delegation the ceiling refused, with its job already handed over"},
		{name: armTaskBgRunning, asks: "a backgrounded delegation executing inside its job"},
		{name: armCancelTool, asks: "whether a stop reaches an ordinary tool the turn is waiting on"},
		{name: armCancelBackground, asks: "whether a stop leaves work already handed to a job alone"},
		{name: armCancelHeadlessOwner, asks: "who owns the cancellation of a turn the controller never admitted"},
		{name: armTaskFgQueuedCancel, asks: "a delegation stopped while the scheduler still held it back"},
		{name: armTaskBgQueuedCancel, asks: "the same, backgrounded: a job killed before it was ever admitted"},
		{name: armTaskFgRunningCancel, asks: "a delegation stopped while its child was executing"},
		{name: armTaskBgRunningCancel, asks: "the same, backgrounded: a job killed while its child ran"},
		{name: armWaitSlots, asks: "an item the session's total ceiling refused"},
		{name: armWaitWriters, asks: "an item the writer ceiling refused, with total capacity free"},
		{name: armWaitClaim, asks: "an item a path conflict refused, with both ceilings free"},
		{name: armWaitTransition, asks: "what a reported wait cause does when the thing it named stops holding the item"},
		{name: armTerminalAdopted, asks: "an item that never ran and still holds an answer"},
		{name: armTerminalSkippedDep, asks: "an item that never ran and holds none, because its dependency failed"},
		{name: armTerminalCancelled, asks: "an item a cancellation ended before it was admitted"},
		{name: armTerminalContext, asks: "whether a delivered answer's edge follows from what is already durable"},
		{name: armChildTerminal, asks: "whether the store keeps every terminal the graph draws"},
		{name: armDeriveSkipBoth, asks: "a skip whose two upstreams both ended without an answer"},
		{name: armDeriveSkipFlip, asks: "the same, with the two failures in the other order"},
		{name: armIdentitySemantics, asks: "what a node's model and effort report, across two producers and the store"},
		{name: armDeriveAnswered, asks: "a dependent whose upstreams answered, one by completing and one by adopting"},
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

// editTailRow rewrites one message the projection does not claim to cover. The
// covered hash cannot see it, so the projection should keep serving.
func editTailRow(_ armRoot, before Observation) error {
	return rewriteSession(before, func(msgs []provider.Message, covered int) ([]provider.Message, error) {
		for i := covered; i < len(msgs); i++ {
			if msgs[i].Role == provider.RoleUser || msgs[i].Role == provider.RoleAssistant {
				msgs[i].Content += "\n[probe: tail row edited]"
				return msgs, nil
			}
		}
		return nil, errUnexpected("a live tail row to edit", covered)
	})
}

// truncateTail drops everything past the fold boundary, leaving the covered
// prefix exactly as the projection folded it.
func truncateTail(_ armRoot, before Observation) error {
	return rewriteSession(before, func(msgs []provider.Message, covered int) ([]provider.Message, error) {
		return msgs[:covered], nil
	})
}

// rewriteSession applies a transcript rewrite through the save a rewind or a
// recovery branch uses, refusing an arm with no live tail to act on.
func rewriteSession(before Observation, edit func([]provider.Message, int) ([]provider.Message, error)) error {
	session, err := agent.LoadSession(before.SessionPath)
	if err != nil {
		return err
	}
	msgs := session.Snapshot()
	covered := before.Sidecar.CoveredCount
	if covered <= 0 || covered >= len(msgs) {
		return errUnexpected("a live tail past the fold", covered)
	}
	next, err := edit(msgs, covered)
	if err != nil {
		return err
	}
	session.Replace(next)
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
	if _, ok := successorTurn(a.name); ok {
		if err := spawn(self, "successor", root, a.name); err != nil {
			return armResult{}, fmt.Errorf("successor process: %w", err)
		}
	}
	if err := spawn(self, "resume", root, a.name); err != nil {
		return armResult{}, fmt.Errorf("resume process: %w", err)
	}
	after, err := readObservation(root, "resume")
	if err != nil {
		return armResult{}, err
	}
	var extra *Observation
	if _, ok := successorTurn(a.name); ok {
		obs, err := readObservation(root, "successor")
		if err != nil {
			return armResult{}, err
		}
		extra = &obs
	} else if phase := extraPhase(a.name); phase != "" {
		obs, err := readObservation(root, phase)
		if err != nil {
			return armResult{}, err
		}
		extra = &obs
	}
	return classify(a, extra, before, after), nil
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
