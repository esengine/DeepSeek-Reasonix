package control

import "reasonix/internal/contract/config"

// CompactionSettings is the compaction trigger as configured and as the
// session is actually running under it.
type CompactionSettings struct {
	Ratio float64 `json:"ratio"`
	// ContextWindow is the window in force, zero when nobody declared one —
	// which is also what turns automatic maintenance off entirely.
	ContextWindow int `json:"context_window"`
	// Trigger is the boundary this session folds at, read from the agent
	// rather than recomputed here: two implementations of one rule is how the
	// number a user reads stops matching the one that fires.
	Trigger int    `json:"trigger"`
	Path    string `json:"path"`
}

// CompactionSettings reports the fold bounds for this session.
func (c *Controller) CompactionSettings() CompactionSettings {
	path := config.UserConfigPath()
	out := CompactionSettings{
		Ratio: c.CompactRatio(),
		Path:  path,
	}
	if c.executor != nil {
		out.ContextWindow = c.executor.ContextWindow()
		out.Trigger = c.executor.CompactTrigger()
	}
	return out
}
