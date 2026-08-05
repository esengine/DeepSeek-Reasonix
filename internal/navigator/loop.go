package navigator

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// DeviationKind classifies how an observation diverged from its hypothesis.
// The kind drives the corrector's strategy: a hash drift on a "shouldn't
// change" field is a warning (someone else touched the env); a missing
// expected fact is a retry; a total mismatch is a rollback.
type DeviationKind int

const (
	DeviationNone DeviationKind = iota
	// DeviationInterfaceDrift — the interface hash changed when the action
	// shouldn't have touched it (e.g. a "read" caused a screen change), or
	// didn't change when it should have (e.g. a "click" had no visible effect).
	DeviationInterfaceDrift
	// DeviationEnvDrift — same, for the environment hash.
	DeviationEnvDrift
	// DeviationFactLost — an implicit fact present in the prediction is
	// missing from the observation. This is the direct signal of OSWorld 2.0's
	// implicit-state amnesia: the agent forgot something it had recovered.
	DeviationFactLost
	// DeviationTotalMismatch — both hashes and the fact set diverge. The action
	// likely affected the wrong target; rollback is the safe move.
	DeviationTotalMismatch
)

// Deviation is the comparator's output: what changed vs the hypothesis, and
// how severe it is. The ClosedLoopEngine turns this into a Correction.
type Deviation struct {
	Kind DeviationKind

	// ExpectedX is the hypothesis value; ObservedX is what really happened.
	ExpectedInterface string
	ObservedInterface string
	ExpectedEnv       string
	ObservedEnv       string

	// MissingFactKeys are implicit facts that were in the prediction but not
	// in the observation — the facts at risk of being lost.
	MissingFactKeys []string

	// NewFactKeys are facts the observation recovered that the prediction
	// didn't carry — normally a good sign, but a sudden flood can indicate the
	// agent is re-discovering state it already had (amnesia + recovery loop).
	NewFactKeys []string

	// Message is a human-readable summary for diagnostics.
	Message string
}

// Severity ranks the deviation so the corrector can pick a proportionate
// response. Low = note and continue; Medium = retry with adjusted params;
// High = rollback to last known-good; Critical = stop and ask the host.
func (d Deviation) Severity() int {
	switch d.Kind {
	case DeviationNone:
		return 0
	case DeviationInterfaceDrift, DeviationEnvDrift:
		return 1 // low — could be benign (another process, async update)
	case DeviationFactLost:
		return 2 // medium — re-inject the fact and continue
	case DeviationTotalMismatch:
		return 3 // high — rollback
	default:
		return 4
	}
}

// Compare is the comparator: given the predicted (hypothesis) snapshot and
// the real observation, return what diverged. It is pure and side-effect free
// so it can be unit-tested without a running kernel.
func Compare(predicted, observed StateSnapshot) Deviation {
	d := Deviation{
		ExpectedInterface: predicted.InterfaceHash,
		ObservedInterface: observed.InterfaceHash,
		ExpectedEnv:       predicted.EnvHash,
		ObservedEnv:       observed.EnvHash,
	}
	// Build fact key sets for diffing.
	predKeys := factKeySet(predicted.ImplicitFacts)
	obsKeys := factKeySet(observed.ImplicitFacts)
	for k := range predKeys {
		if !obsKeys[k] {
			d.MissingFactKeys = append(d.MissingFactKeys, k)
		}
	}
	for k := range obsKeys {
		if !predKeys[k] {
			d.NewFactKeys = append(d.NewFactKeys, k)
		}
	}

	ifaceChanged := predicted.InterfaceHash != observed.InterfaceHash
	envChanged := predicted.EnvHash != observed.EnvHash
	// A predicted-empty hash means "expect change"; a non-empty match means
	// "expect no change". Distinguish:
	//   - predicted non-empty, observed same → no drift (good)
	//   - predicted non-empty, observed different → drift on a stable field
	//   - predicted empty (expect change), observed empty → no change when one
	//     was expected → drift (action had no effect)
	ifaceDrift := false
	if predicted.InterfaceHash != "" && observed.InterfaceHash != predicted.InterfaceHash {
		ifaceDrift = true
	}
	if predicted.InterfaceHash == "" && observed.InterfaceHash == "" && actionChangesInterface(predicted.Action) {
		ifaceDrift = true // expected a change, got none
	}
	envDrift := false
	if predicted.EnvHash != "" && observed.EnvHash != predicted.EnvHash {
		envDrift = true
	}
	if predicted.EnvHash == "" && observed.EnvHash == "" && actionChangesEnv(predicted.Action) {
		envDrift = true
	}

	switch {
	case !ifaceChanged && !envChanged && len(d.MissingFactKeys) == 0 && len(d.NewFactKeys) == 0:
		d.Kind = DeviationNone
		d.Message = "observation matches prediction"
	case len(d.MissingFactKeys) > 0 && !ifaceDrift && !envDrift:
		d.Kind = DeviationFactLost
		d.Message = fmt.Sprintf("implicit facts lost: %s", strings.Join(d.MissingFactKeys, ", "))
	case ifaceDrift && envDrift:
		d.Kind = DeviationTotalMismatch
		d.Message = "interface + environment both diverged from prediction"
	case ifaceDrift:
		d.Kind = DeviationInterfaceDrift
		d.Message = "interface hash drifted from prediction"
	case envDrift:
		d.Kind = DeviationEnvDrift
		d.Message = "environment hash drifted from prediction"
	default:
		d.Kind = DeviationNone
		d.Message = "minor diff, within tolerance"
	}
	return d
}

func factKeySet(facts []Fact) map[string]bool {
	m := make(map[string]bool, len(facts))
	for _, f := range facts {
		m[f.Key] = true
	}
	return m
}

func actionChangesInterface(action string) bool {
	v := actionVerb(action)
	switch v {
	case "click", "type", "scroll", "exec":
		return true
	}
	return false
}

func actionChangesEnv(action string) bool {
	v := actionVerb(action)
	switch v {
	case "write", "exec":
		return true
	}
	return false
}

// CorrectionStrategy is what the corrector decided to do about a deviation.
type CorrectionStrategy int

const (
	// StrategyContinue — deviation is within tolerance, proceed.
	StrategyContinue CorrectionStrategy = iota
	// StrategyReinjectFacts — facts were lost; re-inject them into the next
	// action's context and continue. This is the direct fix for OSWorld 2.0's
	// implicit-state amnesia.
	StrategyReinjectFacts
	// StrategyRetry — the action had no effect or the wrong effect; retry it,
	// optionally with adjusted parameters.
	StrategyRetry
	// StrategyRollback — serious mismatch; rewind to the last known-good
	// snapshot and re-plan from there.
	StrategyRollback
	// StrategyAskHost — the engine cannot resolve this autonomously; hand
	// control back to the host (Reasonix/HERMES) for a human or planner
	// decision.
	StrategyAskHost
)

// Correction is the engine's verdict on a deviation.
type Correction struct {
	Strategy   CorrectionStrategy
	Deviation  Deviation
	RewindTo   int    // for StrategyRollback: step to rewind to
	Reinject   []Fact // for StrategyReinjectFacts: facts to restore
	RetryCount int    // how many times this action has been retried
	Reason     string // human-readable explanation
	At         time.Time
}

// MaxRetries bounds how many times the engine will retry the same action before
// escalating to rollback or ask-host. Beyond this the action is presumed wrong,
// not unlucky.
const MaxRetries = 3

// ClosedLoopEngine wraps every action in a verify-act-correct cycle. It holds
// retry counters per action so a flaky action doesn't loop forever, and it
// records the correction history so the host can audit what went wrong.
type ClosedLoopEngine struct {
	mu          sync.Mutex
	retryCount  map[string]int // action → consecutive retries
	corrections []Correction
}

func NewClosedLoopEngine() *ClosedLoopEngine {
	return &ClosedLoopEngine{retryCount: make(map[string]int)}
}

// Decide turns a deviation into a correction strategy. It is pure (no side
// effects beyond the retry counter) so the kernel can call it synchronously
// in the action loop.
func (e *ClosedLoopEngine) Decide(action string, d Deviation, lastGoodStep int) Correction {
	e.mu.Lock()
	defer e.mu.Unlock()

	c := Correction{Deviation: d, At: time.Now()}

	if d.Kind == DeviationNone {
		e.retryCount[action] = 0
		c.Strategy = StrategyContinue
		c.Reason = "no deviation"
		return c
	}

	// Re-inject lost facts first — cheapest fix, directly targets amnesia.
	if d.Kind == DeviationFactLost && len(d.MissingFactKeys) > 0 {
		// Build the reinject set from the deviation's expected facts.
		c.Strategy = StrategyReinjectFacts
		c.Reinject = missingFacts(d)
		c.Reason = fmt.Sprintf("re-injecting %d lost implicit facts", len(c.Reinject))
		e.retryCount[action] = 0
		return c
	}

	retries := e.retryCount[action]
	if retries < MaxRetries && d.Severity() <= 2 {
		e.retryCount[action] = retries + 1
		c.Strategy = StrategyRetry
		c.RetryCount = retries + 1
		c.Reason = fmt.Sprintf("retrying action (attempt %d/%d): %s", retries+1, MaxRetries, d.Message)
		return c
	}

	// High severity or out of retries — rollback if we have a known-good state.
	if lastGoodStep >= 0 {
		c.Strategy = StrategyRollback
		c.RewindTo = lastGoodStep
		c.Reason = fmt.Sprintf("rolling back to step %d: %s", lastGoodStep, d.Message)
		e.retryCount[action] = 0
		return c
	}

	// No known-good state to rewind to — ask the host.
	c.Strategy = StrategyAskHost
	c.Reason = fmt.Sprintf("cannot resolve autonomously, asking host: %s", d.Message)
	e.retryCount[action] = 0
	return c
}

// missingFacts extracts the Fact records for the deviation's MissingFactKeys
// from the prediction's implicit facts. (The deviation carries keys; the
// engine needs the full Fact to re-inject.)
func missingFacts(d Deviation) []Fact {
	// The deviation's Expected* carry the prediction's snapshot indirectly via
	// MissingFactKeys; the full facts are passed through the kernel. Here we
	// return placeholder facts keyed by the missing keys — the kernel fills in
	// the real values from the state manager before re-injection.
	var out []Fact
	for _, k := range d.MissingFactKeys {
		out = append(out, Fact{Key: k, Kind: StateKindImplicit})
	}
	return out
}

// RecordCorrection appends to the correction history for auditing.
func (e *ClosedLoopEngine) RecordCorrection(c Correction) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.corrections = append(e.corrections, c)
}

// Corrections returns a copy of the correction history.
func (e *ClosedLoopEngine) Corrections() []Correction {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Correction, len(e.corrections))
	copy(out, e.corrections)
	return out
}

// ResetRetry clears the retry counter for an action (called after a success).
func (e *ClosedLoopEngine) ResetRetry(action string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.retryCount, action)
}
