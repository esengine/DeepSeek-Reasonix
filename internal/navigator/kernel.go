package navigator

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Options configures a Navigator. The zero value is usable but gives an
// in-memory-only kernel with default windows; callers set fields as needed.
type Options struct {
	// HistoryWindow bounds the episodic state history. 0 = unbounded. A
	// bounded window keeps memory predictable on multi-hour OSWorld 2.0 tasks
	// where unbounded history would itself become the bottleneck.
	HistoryWindow int
	// MaxRetriesPerAction is forwarded to the ClosedLoopEngine. 0 uses default.
	MaxRetriesPerAction int
}

// Navigator is the OSWorld 2.0 "state-based navigator" kernel. It wraps every
// host action in a continuous-state, closed-loop cycle:
//
//  1. sensors snapshot the env (before)
//  2. state manager predicts the expected outcome (hypothesis)
//  3. host adapter executes the action (act)
//  4. sensors snapshot the env (after)
//  5. comparator checks prediction vs reality (verify)
//  6. corrector picks a strategy if they diverged (correct)
//  7. state manager records the real outcome (update)
//
// Steps 1-7 run on every action, so the navigator maintains state continuously
// — not just when the prompt nears the context window. That is the difference
// between compaction-based state (lost on fold) and navigator-based state
// (lives in the kernel, survives compaction and crashes).
type Navigator struct {
	mu      sync.Mutex
	state   *ContinuousStateManager
	loop    *ClosedLoopEngine
	sensor  *DynamicEnvSensor
	adapter HostAdapter

	// lastGoodStep is the most recent step that completed without deviation.
	// The corrector rewinds here on a rollback. -1 means no known-good yet.
	lastGoodStep int
	// started guards Seed() so it runs exactly once.
	started bool

	// ObserveToolCall (advisory path) state: throttled sensor snapshots and
	// the last observation used for drift detection. The advisory path feeds
	// host-executed tools into the state graph without re-executing them.
	lastEnvHash, lastIfaceHash string
	lastObserve                time.Time
	lastObserved               *StateSnapshot
	observeEvery               time.Duration
}

// New creates a Navigator wired to the given adapter. The adapter is the only
// dependency on a specific host — everything else is host-agnostic.
func New(adapter HostAdapter, opts Options) *Navigator {
	histWin := opts.HistoryWindow
	if histWin == 0 {
		histWin = 50 // default: keep last 50 steps in episodic memory
	}
	return &Navigator{
		state:        NewContinuousStateManager(histWin),
		loop:         NewClosedLoopEngine(),
		sensor:       NewDynamicEnvSensor(),
		adapter:      adapter,
		lastGoodStep: -1,
		// Throttle advisory-path sensor snapshots (filesystem walks on every
		// tool call are too expensive for long sessions).
		observeEvery: 3 * time.Second,
	}
}

// StateManager returns the continuous-state manager (for the host to query
// implicit facts, e.g. to inject into a compaction summary).
func (n *Navigator) StateManager() *ContinuousStateManager { return n.state }

// Loop returns the closed-loop engine (for the host to audit corrections).
func (n *Navigator) Loop() *ClosedLoopEngine { return n.loop }

// Sensor returns the dynamic-env sensor (for the host to attach sensors).
func (n *Navigator) Sensor() *DynamicEnvSensor { return n.sensor }

// AddSensor attaches a background sensor to the kernel.
func (n *Navigator) AddSensor(s Sensor) { n.sensor.Add(s) }

// Seed captures the initial environment and creates the root state snapshot.
// Must be called once before Execute. It is idempotent: a second call is a
// no-op so a host that re-initializes (e.g. after a crash recovery) doesn't
// reset the navigator's accumulated state.
func (n *Navigator) Seed(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.started {
		return nil
	}
	ifaceHash, envDigest, _, err := n.sensor.SnapshotAll(ctx)
	if err != nil {
		// Sensors may not be ready at seed time; seed with empty hashes and
		// let the first Execute's before-snapshot fill them in.
		ifaceHash, envDigest = "", ""
	}
	// Use the adapter's own env snapshot as the authoritative env hash if the
	// sensors returned nothing.
	envHash := envDigest
	if envHash == "" {
		if snap, serr := n.adapter.SnapshotEnv(ctx); serr == nil {
			envHash = snap
		}
	}
	n.state.Seed(ctx, ifaceHash, envHash)
	n.started = true
	return nil
}

// ObserveToolCall feeds one host-executed tool call into the navigator without
// re-executing it (advisory mode). The host has already run the tool through its
// own permission/hooks/evidence path; the navigator only records the observation,
// recovers implicit facts, detects failure deviations, and returns correction
// advice as text ("" = continue). The host decides whether to act on the advice
// — the navigator never executes tools on the host's behalf, so the agent's
// security boundary is never bypassed.
func (n *Navigator) ObserveToolCall(ctx context.Context, toolName string, args string, result string, err error) string {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Auto-seed on first observe (mirrors Execute's auto-seed).
	if !n.started {
		n.started = true
		ifaceHash, envDigest, _, serr := n.sensor.SnapshotAll(ctx)
		if serr != nil {
			ifaceHash, envDigest = "", ""
		}
		envHash := envDigest
		if envHash == "" {
			if snap, aerr := n.adapter.SnapshotEnv(ctx); aerr == nil {
				envHash = snap
			}
		}
		n.state.Seed(ctx, ifaceHash, envHash)
		n.lastEnvHash, n.lastIfaceHash = envHash, ifaceHash
	}

	action := HostAction{Verb: actionVerbForTool(toolName), Target: toolName, Args: args}
	label := actionLabel(action)

	// Throttle sensor snapshots: reuse the last hashes within the interval.
	now := time.Now()
	ifaceHash, envHash := n.lastIfaceHash, n.lastEnvHash
	if now.Sub(n.lastObserve) >= n.observeEvery {
		if h, e, _, serr := n.sensor.SnapshotAll(ctx); serr == nil {
			ifaceHash, envHash = h, e
			if envHash == "" {
				if snap, aerr := n.adapter.SnapshotEnv(ctx); aerr == nil {
					envHash = snap
				}
			}
			n.lastIfaceHash, n.lastEnvHash = ifaceHash, envHash
			n.lastObserve = now
		}
	}

	recovered := ExtractImplicitFacts(label, result, err)
	observed := StateSnapshot{InterfaceHash: ifaceHash, EnvHash: envHash}
	observed = n.state.AfterAction(ctx, label, observed, recovered)

	// Deviation: tool failures are the signal we act on. Env-hash changes are
	// expected when tools modify files, so they are not treated as drift here
	// (Execute's prediction-based Compare handles that in full-loop mode).
	dev := Deviation{Kind: DeviationNone}
	if err != nil {
		dev = Deviation{Kind: DeviationTotalMismatch, Message: "tool reported an error: " + err.Error()}
	}

	corr := n.loop.Decide(label, dev, n.lastGoodStep)
	n.loop.RecordCorrection(corr)
	if dev.Kind == DeviationNone {
		n.lastGoodStep = observed.Step
	}
	n.lastObserved = &observed

	// Flush correlated sensor events.
	for _, ev := range n.sensor.FlushEvents() {
		n.adapter.Emit(ctx, HostEvent{
			Kind:   "sensor",
			Level:  sensorLevel(ev.Severity),
			Text:   fmt.Sprintf("%s %s: %s", ev.Source, ev.Kind, ev.Subject),
			Detail: ev.Detail,
			Step:   observed.Step,
		})
	}

	switch corr.Strategy {
	case StrategyContinue:
		return ""
	case StrategyRetry:
		return fmt.Sprintf("navigator advises retrying %q: %s (attempt %d)", label, corr.Reason, corr.RetryCount)
	case StrategyRollback:
		return fmt.Sprintf("navigator advises rolling back to step %d after %q: %s", corr.RewindTo, label, corr.Reason)
	default:
		return fmt.Sprintf("navigator requests host review of %q: %s", label, corr.Reason)
	}
}

// actionVerbForTool maps a Reasonix tool name to a coarse verb so the state
// graph's action-effect model can classify it. Unknown tools default to
// "exec" (a generic side-effecting action).
func actionVerbForTool(name string) string {
	switch {
	case strings.HasPrefix(name, "read") || strings.Contains(name, "view") || strings.Contains(name, "list"):
		return "read"
	case strings.HasPrefix(name, "write") || strings.Contains(name, "edit") || strings.Contains(name, "patch") || strings.Contains(name, "create"):
		return "write"
	case strings.Contains(name, "bash") || strings.Contains(name, "exec") || strings.Contains(name, "terminal"):
		return "exec"
	case strings.Contains(name, "click") || strings.Contains(name, "type") || strings.Contains(name, "scroll"):
		return "click"
	default:
		return "exec"
	}
}

// Execute is the main entry point the host calls instead of its own tool
// dispatcher. It runs the full verify-act-correct cycle and returns the action
// output (for the host to feed to its model) plus any correction that fired.
//
// The host should call this for every action it wants the navigator to govern.
// Actions the host runs outside the navigator (e.g. internal bookkeeping) are
// invisible to the kernel and won't get state protection — that's the trade.
func (n *Navigator) Execute(ctx context.Context, action HostAction) (HostResult, Correction, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.started {
		// Auto-seed on first execute so a host that forgot Seed() still works.
		n.started = true
		ifaceHash, envDigest, _, _ := n.sensor.SnapshotAll(ctx)
		envHash := envDigest
		if envHash == "" {
			if snap, serr := n.adapter.SnapshotEnv(ctx); serr == nil {
				envHash = snap
			}
		}
		n.state.Seed(ctx, ifaceHash, envHash)
	}

	actionLabel := actionLabel(action)
	predicted := n.state.BeforeAction(ctx, actionLabel)

	// Pre-action: permission check (fail-closed — never bypass the host gate).
	if allowed, reason := n.adapter.Permission(ctx, action); !allowed {
		n.adapter.Emit(ctx, HostEvent{
			Kind: "correction", Level: "warn",
			Text:   "Action blocked by host permission gate",
			Detail: reason, Step: predicted.Step,
		})
		return HostResult{}, Correction{Strategy: StrategyAskHost, Reason: "permission denied: " + reason, At: time.Now()}, ErrAskHost
	}

	// Act: the host executes through its real path (hooks, evidence, etc.).
	result, err := n.adapter.Execute(ctx, action)
	if err != nil {
		// Execution error — record a deviation and let the corrector decide.
		dev := Deviation{Kind: DeviationTotalMismatch, Message: fmt.Sprintf("host execute error: %v", err)}
		corr := n.loop.Decide(actionLabel, dev, n.lastGoodStep)
		n.loop.RecordCorrection(corr)
		n.adapter.Emit(ctx, HostEvent{Kind: "correction", Level: "warn", Text: corr.Reason, Step: predicted.Step})
		return result, corr, err
	}

	// Observe: snapshot the env after the action.
	ifaceHash, envDigest, _, _ := n.sensor.SnapshotAll(ctx)
	if result.InterfaceProbe != "" {
		ifaceHash = result.InterfaceProbe
	} else if probe, perr := n.adapter.InterfaceProbe(ctx); perr == nil && probe != "" {
		ifaceHash = probe
	}
	envHash := envDigest
	if envHash == "" {
		if snap, serr := n.adapter.SnapshotEnv(ctx); serr == nil {
			envHash = snap
		}
	}

	// Recover implicit facts from the result (paths, IDs, errors).
	recovered := ExtractImplicitFacts(actionLabel, result.Output, result.Err)

	observed := StateSnapshot{
		InterfaceHash: ifaceHash,
		EnvHash:       envHash,
	}
	// Update state manager with the real observation.
	observed = n.state.AfterAction(ctx, actionLabel, observed, recovered)

	// Verify: compare prediction vs observation.
	dev := Compare(predicted, observed)
	if dev.Kind != DeviationNone {
		n.adapter.Emit(ctx, HostEvent{Kind: "deviation", Level: devLevel(dev), Text: dev.Message, Step: observed.Step})
	}

	// Correct: pick a strategy.
	corr := n.loop.Decide(actionLabel, dev, n.lastGoodStep)
	n.loop.RecordCorrection(corr)

	// Apply the correction.
	switch corr.Strategy {
	case StrategyContinue:
		n.lastGoodStep = observed.Step
	case StrategyReinjectFacts:
		// Re-inject lost facts into the next action's context. The host is
		// responsible for actually threading them into its prompt; we record
		// them on the correction and surface an event.
		n.adapter.Emit(ctx, HostEvent{
			Kind: "correction", Level: "info",
			Text:   corr.Reason,
			Detail: fmt.Sprintf("re-injecting: %s", factKeys(corr.Reinject)),
			Step:   observed.Step,
		})
		n.lastGoodStep = observed.Step
	case StrategyRetry:
		n.adapter.Emit(ctx, HostEvent{Kind: "correction", Level: "warn", Text: corr.Reason, Step: observed.Step})
		// The host decides whether to actually retry; we just signal.
	case StrategyRollback:
		if snap, ok := n.state.Rewind(corr.RewindTo); ok {
			n.adapter.Emit(ctx, HostEvent{
				Kind: "correction", Level: "error",
				Text:   corr.Reason,
				Detail: fmt.Sprintf("rewound to step %d: %s", corr.RewindTo, snap.Summary()),
				Step:   corr.RewindTo,
			})
		}
	case StrategyAskHost:
		n.adapter.Emit(ctx, HostEvent{Kind: "correction", Level: "error", Text: corr.Reason, Step: observed.Step})
		return result, corr, ErrAskHost
	}

	// Flush correlated sensor events.
	for _, ev := range n.sensor.FlushEvents() {
		n.adapter.Emit(ctx, HostEvent{
			Kind: "sensor", Level: sensorLevel(ev.Severity),
			Text:   fmt.Sprintf("%s %s: %s", ev.Source, ev.Kind, ev.Subject),
			Detail: ev.Detail, Step: observed.Step,
		})
	}

	return result, corr, nil
}

// ImplicitStateDigest returns the accumulated implicit facts as a text digest,
// for the host to inject into compaction summaries. This is the bridge between
// the navigator's continuous state and the host's prompt-level state.
func (n *Navigator) ImplicitStateDigest() string {
	return n.state.ImplicitStateDigest()
}

// Corrections returns the full correction history for auditing.
func (n *Navigator) Corrections() []Correction { return n.loop.Corrections() }

// actionLabel renders a HostAction as the string the state graph stores.
func actionLabel(a HostAction) string {
	if a.Target == "" {
		return a.Verb
	}
	return a.Verb + " " + a.Target
}

func devLevel(d Deviation) string {
	switch d.Severity() {
	case 0:
		return "info"
	case 1:
		return "info"
	case 2:
		return "warn"
	default:
		return "error"
	}
}

func sensorLevel(sev int) string {
	switch sev {
	case 0:
		return "info"
	case 1:
		return "warn"
	default:
		return "error"
	}
}

func factKeys(facts []Fact) string {
	keys := make([]string, 0, len(facts))
	for _, f := range facts {
		keys = append(keys, f.Key)
	}
	return strings.Join(keys, ", ")
}

// ExtractImplicitFacts scans a tool result for implicit facts — file paths,
// IDs, and error messages that the agent will need later but wasn't told
// directly. This is the navigator's proactive defense against OSWorld 2.0's
// implicit-state amnesia: facts are recovered on every action, not just when
// the model happens to mention them.
//
// The extraction is deliberately conservative: it prefers false negatives
// (missing a fact) over false positives (a "path" that's actually prose),
// because a wrong fact is worse than a missing one. The regexes match the
// StateTracker's pre-compiled patterns so the two layers stay consistent.
func ExtractImplicitFacts(action string, result string, err error) []Fact {
	var facts []Fact
	if err != nil {
		facts = append(facts, Fact{
			Key: "error:" + action, Value: err.Error(),
			Source: action, Kind: StateKindImplicit,
		})
	}
	// Extract file paths (Unix + Windows).
	for _, p := range extractPaths(result) {
		facts = append(facts, Fact{
			Key: "path:" + p, Value: p,
			Source: action + " result", Kind: StateKindImplicit,
		})
	}
	// Extract IDs (assignment + JSON).
	for _, id := range extractIDs(result) {
		facts = append(facts, Fact{
			Key: "id:" + id, Value: id,
			Source: action + " result", Kind: StateKindImplicit,
		})
	}
	return facts
}
