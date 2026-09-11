// Package loopguard detects and breaks tool-call loops that would waste
// context window and tokens by repeatedly attempting the same failing operation.
//
// It implements Hermes-style guardrails with configurable thresholds:
//   - exact_failure: same error message → stop at 5
//   - same_tool_failure: same tool name → stop at 8
//   - idempotent_no_progress: same tool+args without state change → stop at 5
//
// The guard is designed to be called by the tool execution layer before each
// tool invocation. It returns a Decision: Proceed, Warn, or Stop.
package loopguard

import (
	"fmt"
	"sync"
	"time"
)

// Decision is what the guardrail recommends.
type Decision int

const (
	Proceed Decision = iota
	Warn
	Stop
)

func (d Decision) String() string {
	switch d {
	case Proceed:
		return "proceed"
	case Warn:
		return "warn"
	case Stop:
		return "stop"
	default:
		return "unknown"
	}
}

// Record is one tool invocation outcome.
type Record struct {
	Tool      string    // tool name, e.g. "edit_file"
	Error     string    // error string, "" = success
	Args      string    // normalized arguments fingerprint
	Timestamp time.Time // when the call completed
}

// CallFingerprint creates a stable key from tool name + args for
// idempotent-no-progress detection.
func CallFingerprint(tool, args string) string {
	return fmt.Sprintf("%s(%s)", tool, args)
}

// Config sets the guardrail thresholds.
type Config struct {
	WarnAfterExactFailure      int `toml:"warn_after_exact_failure"`
	WarnAfterSameToolFailure   int `toml:"warn_after_same_tool_failure"`
	WarnAfterNoProgress        int `toml:"warn_after_idempotent_no_progress"`
	HardStopAfterExactFailure  int `toml:"hard_stop_after_exact_failure"`
	HardStopAfterSameToolFail  int `toml:"hard_stop_after_same_tool_failure"`
	HardStopAfterNoProgress    int `toml:"hard_stop_after_idempotent_no_progress"`
	WindowSeconds              int `toml:"window_seconds"` // lookback window
}

// DefaultConfig returns sensible defaults matching Hermes' guardrails.
func DefaultConfig() Config {
	return Config{
		WarnAfterExactFailure:     2,
		WarnAfterSameToolFailure:  3,
		WarnAfterNoProgress:       2,
		HardStopAfterExactFailure: 5,
		HardStopAfterSameToolFail: 8,
		HardStopAfterNoProgress:   5,
		WindowSeconds:             300, // 5 min lookback
	}
}

// Guardrail tracks tool-call history and makes stop/warn/proceed decisions.
type Guardrail struct {
	mu      sync.Mutex
	history []Record
	config  Config
}

// New creates a guardrail with the given config.
func New(cfg Config) *Guardrail {
	return &Guardrail{
		history: make([]Record, 0, 100),
		config:  cfg,
	}
}

// Record records a tool invocation outcome and returns the guard's decision.
func (g *Guardrail) Record(tool, errorMsg, args string) Decision {
	g.mu.Lock()
	defer g.mu.Unlock()

	r := Record{
		Tool:      tool,
		Error:     errorMsg,
		Args:      args,
		Timestamp: time.Now(),
	}
	g.history = append(g.history, r)
	g.prune()

	return g.evaluate(r)
}

// Reset clears all history. Call when a task changes or the user intervenes.
func (g *Guardrail) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.history = g.history[:0]
}

// History returns the recent record history for diagnostics.
func (g *Guardrail) History() []Record {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]Record, len(g.history))
	copy(out, g.history)
	return out
}

// prune removes records outside the lookback window.
func (g *Guardrail) prune() {
	cutoff := time.Now().Add(-time.Duration(g.config.WindowSeconds) * time.Second)
	i := 0
	for ; i < len(g.history); i++ {
		if g.history[i].Timestamp.After(cutoff) {
			break
		}
	}
	if i > 0 {
		g.history = g.history[i:]
	}
}

// evaluate checks the last record against all three detection patterns.
func (g *Guardrail) evaluate(r Record) Decision {
	if r.Error == "" {
		return Proceed // success is always fine
	}

	// 1. Exact failure: same error message repeatedly
	exactCount := 0
	for i := len(g.history) - 1; i >= 0; i-- {
		if g.history[i].Error == r.Error {
			exactCount++
		}
	}
	if exactCount >= g.config.HardStopAfterExactFailure {
		return Stop
	}
	if exactCount >= g.config.WarnAfterExactFailure {
		return Warn
	}

	// 2. Same tool failure: same tool failing repeatedly
	sameToolFailCount := 0
	for i := len(g.history) - 1; i >= 0; i-- {
		if g.history[i].Tool == r.Tool && g.history[i].Error != "" {
			sameToolFailCount++
		}
	}
	if sameToolFailCount >= g.config.HardStopAfterSameToolFail {
		return Stop
	}
	if sameToolFailCount >= g.config.WarnAfterSameToolFailure {
		return Warn
	}

	// 3. Idempotent no-progress: same tool+args getting the same error
	fp := CallFingerprint(r.Tool, r.Args)
	noProgressCount := 0
	for i := len(g.history) - 1; i >= 0; i-- {
		if CallFingerprint(g.history[i].Tool, g.history[i].Args) == fp && g.history[i].Error != "" {
			noProgressCount++
		}
	}
	if noProgressCount >= g.config.HardStopAfterNoProgress {
		return Stop
	}
	if noProgressCount >= g.config.WarnAfterNoProgress {
		return Warn
	}

	return Proceed
}
