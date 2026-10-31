package control

import "strings"

// SessionGrant is what a dispatching session passes to the session it opens: the
// model it runs on, its role preset, its tool-approval posture and whether it is
// planning. The opened session takes these as they are, so it is never given
// more than the one that asked for it; sandbox, MCP and skill switches live in
// the user's file, which every session on the machine already shares.
type SessionGrant struct {
	Model    string
	Preset   string
	Approval string
	Plan     bool
}

// DispatchGrant reads the grant this session would pass on.
func (c *Controller) DispatchGrant() SessionGrant {
	return SessionGrant{Model: c.ModelRef(), Preset: c.AgentPreset(), Approval: c.effectiveApprovalMode(), Plan: c.PlanMode()}
}

// AdoptSessionGrant sets this session's posture to the grant's. A field the grant
// leaves empty is left as it is; the approval posture is always the grant's when
// it names one, whether that is stricter or looser than this session's default,
// and planning is the sender's whether on or off, because the grant is the
// sender's own and the session must not outrank it.
func (c *Controller) AdoptSessionGrant(g SessionGrant) {
	if mode, ok := ParseToolApprovalMode(g.Approval); ok {
		c.SetToolApprovalMode(mode)
	}
	if strings.TrimSpace(g.Preset) != "" {
		c.SetAgentPreset(g.Preset)
	}
	c.SetPlanMode(g.Plan)
}
