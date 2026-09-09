package readcoord

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"reasonix/internal/tool"
)

// Advice is a non-blocking steering signal for a stalled read.
type Advice string

const (
	// AdvicePivot tells the caller to change approach once before pausing.
	AdvicePivot Advice = "pivot"
)

// Policy bounds automatic continuation of one logical read. The zero value
// imposes no bound.
type Policy struct {
	MaxPages      int
	MaxActiveTime time.Duration
	// PivotAfter is the consecutive no-progress count that triggers one
	// strategy change; PauseAfter more stops the read.
	PivotAfter int
	PauseAfter int
}

// DefaultPolicy is the internal continuation bound: 64 pages or 120 seconds of
// active reading per logical read, with a single strategy change after two
// stalled pages and a pause two stalled pages later.
func DefaultPolicy() Policy {
	return Policy{MaxPages: 64, MaxActiveTime: 120 * time.Second, PivotAfter: 2, PauseAfter: 2}
}

// Transition reports what one observation changed. Callers commit progress
// from it; nothing else mutates an obligation.
type Transition struct {
	Key        string
	Scope      Scope
	From, To   State
	Generation uint64
	Sequence   uint64
	// Added is the coverage this delivery contributed that was not already
	// known for the current version.
	Added []tool.ReadRange
	// Missing is what the requirement still lacks after the delivery.
	Missing []tool.ReadRange
	// Covered is the accumulated coverage on the current content version.
	Covered   []tool.ReadRange
	SourceEnd *int
	Stale     bool
	Progress  bool
	Advice    Advice
	Stop      *Block
}

// Coordinator owns every obligation. It is safe for concurrent use, but the
// agent feeds it from the single mutation-ordered finalizer so decisions
// follow provider order.
type Coordinator struct {
	mu       sync.Mutex
	byKey    map[string]*Obligation
	sequence uint64
	policy   Policy
}

// New returns an empty coordinator with the default continuation bound.
func New() *Coordinator { return NewWithPolicy(DefaultPolicy()) }

// NewWithPolicy returns a coordinator bounded by policy.
func NewWithPolicy(policy Policy) *Coordinator {
	return &Coordinator{byKey: map[string]*Obligation{}, policy: policy}
}

// Begin registers a requirement before its first call runs. Re-registering a
// key refreshes the requirement and keeps accumulated coverage.
func (c *Coordinator) Begin(key string, scope Scope, req Requirement) Obligation {
	c.mu.Lock()
	defer c.mu.Unlock()

	ob := c.byKey[key]
	if ob == nil {
		ob = &Obligation{Key: key, Scope: scope}
		c.byKey[key] = ob
	}
	ob.Scope = scope
	ob.Requirement = Requirement{Intent: req.Intent, Ranges: append([]tool.ReadRange(nil), req.Ranges...), WholeFile: req.WholeFile}
	// A new requirement revives a finished obligation: coverage stays valid
	// because it is scoped to one content version.
	if ob.State == StateCreated || ob.State.Terminal() {
		ob.State = StateFetching
	}
	return ob.clone()
}

// Observe folds one delivered envelope into its obligation. ok=false means the
// envelope carried no identity or the obligation was already terminal, so a
// cancelled or satisfied read is never resurrected by a late delivery.
func (c *Coordinator) Observe(env tool.ReadResultEnvelope, activeMillis int64) (Transition, bool) {
	if env.ReadID == "" || env.Source.CanonicalPath == "" {
		return Transition{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	ob := c.byKey[env.ReadID]
	if ob == nil {
		ob = &Obligation{
			Key:         env.ReadID,
			Scope:       Scope{WorkspaceID: env.Source.WorkspaceID, CanonicalPath: env.Source.CanonicalPath},
			Requirement: requirementFor(env),
		}
		c.byKey[ob.Key] = ob
	}
	if ob.State.Terminal() {
		// A verified repeat is accounting, not a new incomplete requirement.
		if ob.State == StateSatisfied && env.Source.Identity != "" && ob.Source == env.Source {
			ob.Pages++
			ob.ActiveTime += time.Duration(activeMillis) * time.Millisecond
		}
		return Transition{}, false
	}
	if env.Source.Identity != "" {
		ob.Source = env.Source
	}

	c.sequence++
	ob.Sequence = c.sequence
	tr := Transition{Key: ob.Key, Scope: ob.Scope, From: ob.State, Sequence: c.sequence}

	// Fragments of two content versions must never be stitched into one
	// coverage claim, so a version change discards what was accumulated.
	if (ob.Version != "" && env.Source.Snapshot != ob.Version) || (env.Source.Snapshot == "" && ob.Pages > 0) {
		ob.Covered = nil
		ob.SawEOF = false
		ob.SourceEnd = nil
		ob.Generation++
		tr.Stale = true
	}
	ob.Version = env.Source.Snapshot
	ob.SawEOF = ob.SawEOF || env.EOF
	if env.SourceEnd != nil {
		end := *env.SourceEnd
		ob.SourceEnd = &end
	}
	// A delivery supersedes an earlier stop reason: whatever blocked the read
	// no longer explains its state.
	ob.Stop = nil
	before := ob.Covered
	ob.Covered = Normalize(append(append([]tool.ReadRange(nil), ob.Covered...), env.DeliveredRanges...))
	tr.Added = Subtract(ob.Covered, before)
	tr.Progress = len(tr.Added) > 0
	ob.Pages++
	if tr.Progress {
		ob.Stagnant = 0
	} else {
		ob.Stagnant++
	}

	ob.ActiveTime += time.Duration(activeMillis) * time.Millisecond
	ob.State = evaluate(ob, env)
	tr.Advice = c.enforcePolicy(ob, tr)
	tr.To = ob.State
	tr.Generation = ob.Generation
	tr.Missing = missingFor(ob)
	tr.Covered = append([]tool.ReadRange(nil), ob.Covered...)
	if ob.SourceEnd != nil {
		end := *ob.SourceEnd
		tr.SourceEnd = &end
	}
	tr.Stop = ob.Stop
	return tr, true
}

// enforcePolicy applies the hard budget and the no-progress ladder. A content
// change never resets the budget: only satisfied or cancelled ends it.
func (c *Coordinator) enforcePolicy(ob *Obligation, tr Transition) Advice {
	if ob.State.Terminal() || ob.Stop != nil {
		return ""
	}
	switch {
	case c.policy.MaxPages > 0 && ob.Pages >= c.policy.MaxPages:
		ob.State = StateBlocked
		ob.Stop = &Block{
			Code:     "page_budget",
			Detail:   fmt.Sprintf("automatic continuation stopped after %d pages", ob.Pages),
			Recovery: "read the remaining lines explicitly, or work on an independent item",
		}
		return ""
	case c.policy.MaxActiveTime > 0 && ob.ActiveTime >= c.policy.MaxActiveTime:
		ob.State = StateBlocked
		ob.Stop = &Block{
			Code:     "time_budget",
			Detail:   fmt.Sprintf("automatic continuation used %s of active read time", ob.ActiveTime.Round(time.Second)),
			Recovery: "read the remaining lines explicitly, or work on an independent item",
		}
		return ""
	case !tr.Progress && c.policy.PivotAfter > 0 && ob.Stagnant >= c.policy.PivotAfter && !ob.Pivoted:
		ob.Pivoted = true
		return AdvicePivot
	case ob.Pivoted && c.policy.PauseAfter > 0 && ob.Stagnant >= c.policy.PivotAfter+c.policy.PauseAfter:
		ob.State = StateBlocked
		ob.Stop = &Block{
			Code:     "no_progress",
			Detail:   fmt.Sprintf("%d consecutive pages added no new content", ob.Stagnant),
			Recovery: "change approach or read a narrower window; the read stays paused until new content arrives",
		}
	}
	return ""
}

// Fail records a read that could not deliver at all.
func (c *Coordinator) Fail(key string, block Block) (Transition, bool) {
	return c.stop(key, StateBlocked, block)
}

// Narrow records that the requirement cannot be met within the current budget.
// Only a local requirement may narrow; a whole-file requirement reports
// needs_scope instead of silently downgrading.
func (c *Coordinator) Narrow(key string, block Block) (Transition, bool) {
	return c.stop(key, StateNeedsScope, block)
}

func (c *Coordinator) stop(key string, state State, block Block) (Transition, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ob := c.byKey[key]
	if ob == nil || ob.State.Terminal() {
		return Transition{}, false
	}
	c.sequence++
	ob.Sequence = c.sequence
	tr := Transition{Key: key, Scope: ob.Scope, From: ob.State, To: state, Sequence: c.sequence, Generation: ob.Generation}
	ob.State = state
	ob.Stop = &block
	tr.Stop = ob.Stop
	tr.Missing = missingFor(ob)
	tr.Covered = append([]tool.ReadRange(nil), ob.Covered...)
	if ob.SourceEnd != nil {
		end := *ob.SourceEnd
		tr.SourceEnd = &end
	}
	return tr, true
}

// Cancel marks an obligation cancelled. Its state is terminal, so a later
// delivery for the same key is ignored.
func (c *Coordinator) Cancel(key string) (Transition, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ob := c.byKey[key]
	if ob == nil || ob.State.Terminal() {
		return Transition{}, false
	}
	c.sequence++
	ob.Sequence = c.sequence
	tr := Transition{Key: key, Scope: ob.Scope, From: ob.State, To: StateCancelled, Sequence: c.sequence, Generation: ob.Generation}
	ob.State = StateCancelled
	return tr, true
}

// Get returns a copy of one obligation.
func (c *Coordinator) Get(key string) (Obligation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ob, ok := c.byKey[key]
	if !ok {
		return Obligation{}, false
	}
	return ob.clone(), true
}

// Snapshot returns every obligation ordered by key.
func (c *Coordinator) Snapshot() []Obligation {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]Obligation, 0, len(c.byKey))
	for _, ob := range c.byKey {
		out = append(out, ob.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func requirementFor(env tool.ReadResultEnvelope) Requirement {
	switch env.Intent {
	case tool.ReadIntentFull:
		return Requirement{Intent: tool.ReadIntentFull, WholeFile: true}
	case tool.ReadIntentRange:
		var ranges []tool.ReadRange
		if env.RequestedRange != nil {
			ranges = []tool.ReadRange{*env.RequestedRange}
		} else {
			ranges = append(ranges, env.DeliveredRanges...)
		}
		return Requirement{Intent: tool.ReadIntentRange, Ranges: Normalize(ranges)}
	default:
		return Requirement{Intent: tool.ReadIntentInspect}
	}
}

func evaluate(ob *Obligation, env tool.ReadResultEnvelope) State {
	switch ob.Requirement.Intent {
	case tool.ReadIntentInspect:
		// One bounded page completes an inspect requirement; content left in
		// the file is not an outstanding debt.
		return StateSatisfied
	case tool.ReadIntentRange:
		if len(ob.Requirement.Ranges) == 0 || Covers(ob.Covered, ob.Requirement.Ranges) {
			return StateSatisfied
		}
		// Reaching EOF satisfies a range only when the reader vouched for where
		// the source ends and that end is inside the requested window.
		if ob.SawEOF && ob.SourceEnd != nil {
			var required []tool.ReadRange
			for _, r := range ob.Requirement.Ranges {
				if end := min(r.End, *ob.SourceEnd); r.Start < end {
					required = append(required, tool.ReadRange{Start: r.Start, End: end})
				}
			}
			if Covers(ob.Covered, required) {
				return StateSatisfied
			}
		}
		return StateNeedsMore
	case tool.ReadIntentFull:
		return evaluateWholeFile(ob, env)
	default:
		return StateDelivered
	}
}

func evaluateWholeFile(ob *Obligation, _ tool.ReadResultEnvelope) State {
	// A whole-file read is only proven by a trustworthy source end plus
	// contiguous coverage from line 0 on one version.
	if !ob.SawEOF || ob.SourceEnd == nil {
		return StateNeedsMore
	}
	if *ob.SourceEnd == 0 {
		return StateSatisfied
	}
	if len(ob.Covered) == 1 && ob.Covered[0].Start == 0 && ob.Covered[0].End >= *ob.SourceEnd {
		return StateSatisfied
	}
	return StateNeedsMore
}

func missingFor(ob *Obligation) []tool.ReadRange {
	switch ob.Requirement.Intent {
	case tool.ReadIntentRange:
		return Subtract(ob.Requirement.Ranges, ob.Covered)
	case tool.ReadIntentFull:
		if ob.SourceEnd != nil {
			return Subtract([]tool.ReadRange{{Start: 0, End: *ob.SourceEnd}}, ob.Covered)
		}
		if len(ob.Covered) == 0 {
			return nil
		}
		return Subtract([]tool.ReadRange{{Start: 0, End: ob.Covered[len(ob.Covered)-1].End}}, ob.Covered)
	default:
		return nil
	}
}
