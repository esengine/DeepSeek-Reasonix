// Main-conversation automatic steering to running sub-agents.
//
// The zcode-inspired interaction (docs/AGENT_ARCHITECTURE.md §2): while a
// sub-agent runs in the background, the user can keep talking to the main
// conversation and send guidance that gets forwarded into the running
// sub-agent's next turn. Auto-forwarding is deliberately conservative —
// only inputs carrying an explicit forward marker are routed; everything
// else stays a normal main-conversation turn.
package agent

import (
	"strings"

	"reasonix/internal/jobs"
)

// forwardMarkers are the explicit prefixes/phrases that mark an input as
// guidance for a running sub-agent. Matching is case-insensitive.
var forwardMarkers = []string{
	"→",
	"->",
	"注入：",
	"注入:",
	"inject:",
	"告诉子智能体",
	"给子任务",
	"to the subagent",
	"steer subagent",
}

// HasForwardMarker reports whether the input carries an explicit forward
// marker (auto-forward gate 1 of 2 — the second is a running target existing).
func HasForwardMarker(text string) bool {
	return markerPrefix(text) != ""
}

// SteerForwarder routes marked main-conversation inputs into the most
// recently started running sub-agent job of the session.
type SteerForwarder struct {
	jm *jobs.Manager
}

// NewSteerForwarder builds a forwarder. A nil manager disables forwarding
// (Forward always returns false, preserving normal-turn behavior).
func NewSteerForwarder(jm *jobs.Manager) *SteerForwarder {
	return &SteerForwarder{jm: jm}
}

// Forward attempts to route text as mid-run guidance to the session's most
// recently started running sub-agent. It returns true only when the text was
// accepted by a live sub-agent (the caller must then NOT also process it as a
// normal turn); false means "no target or injection failed — fall back to a
// normal main-conversation turn".
func (f *SteerForwarder) Forward(parentSession, text string) bool {
	marker := markerPrefix(text)
	if marker == "" {
		return false
	}
	if f == nil || f.jm == nil {
		return false
	}
	body := strings.TrimSpace(text[len(marker):])
	if body == "" {
		return false
	}
	target := f.latestRunningSubagent(parentSession)
	if target == "" {
		return false
	}
	return f.jm.SteerJob(parentSession, target, body)
}

// latestRunningSubagent returns the ID of the most recently started running
// "task" job of the session (background sub-agents), or "" when none.
func (f *SteerForwarder) latestRunningSubagent(parentSession string) string {
	var bestID string
	var bestStart int64
	for _, v := range f.jm.RunningForSession(parentSession) {
		if v.Kind != "task" {
			continue
		}
		if bestID == "" || v.StartedAt > bestStart {
			bestID, bestStart = v.ID, v.StartedAt
		}
	}
	return bestID
}

// markerPrefix returns the matched forward marker prefix, or "". A marker
// alone (nothing after it) does not count — there must be actual guidance.
func markerPrefix(text string) string {
	t := strings.TrimSpace(text)
	lower := strings.ToLower(t)
	for _, m := range forwardMarkers {
		if len(t) > len(m) && strings.HasPrefix(lower, strings.ToLower(m)) {
			return t[:len(m)]
		}
	}
	return ""
}
