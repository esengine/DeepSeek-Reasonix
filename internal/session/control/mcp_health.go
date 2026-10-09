package control

import "reasonix/internal/ext/mcpsetup"

const (
	MCPHealthReady      = "ready"
	MCPHealthConnecting = "connecting"
	MCPHealthFailed     = "failed"
	MCPHealthPending    = "pending"
	MCPHealthDisabled   = "disabled"
	MCPHealthStandby    = "standby"
	MCPHealthIdle       = "idle"
)

// MCPApprovalReason is why a pending server waits for the user: a typed
// identity every surface renders in its own words.
type MCPApprovalReason string

const (
	MCPApprovalAwaiting MCPApprovalReason = "awaiting_user_decision"
	MCPApprovalChanged  MCPApprovalReason = "changed_since_enabled"
	// MCPApprovalDisabled refuses a connect of a server the user switched off;
	// health reports that one as disabled, never as pending.
	MCPApprovalDisabled MCPApprovalReason = "disabled_by_user"
)

// Text is the English fallback for surfaces without their own wording.
func (r MCPApprovalReason) Text() string {
	switch r {
	case MCPApprovalChanged:
		return "what it launches changed since you enabled it (or an earlier version enabled it)"
	case MCPApprovalAwaiting:
		return "declared by the project and not approved yet"
	case MCPApprovalDisabled:
		return "switched off; enable it to run the command shown"
	}
	return ""
}

// MCPHealth is the observable state of one server in this session. A cached
// tool surface can be callable before its process starts, so standby is distinct
// from both ready and idle.
type MCPHealth struct {
	Name       string
	Status     string
	Error      string
	HTTPStatus int
	Tools      int
	Reason     MCPApprovalReason // set only when Status is pending
}

// MCPServerHealth combines declarations, the callable catalog, and live host
// state for every frontend. A server supplied only by the session host is
// included even when it has no durable config entry.
func (c *Controller) MCPServerHealth() []MCPHealth {
	configured := c.ConfiguredMCPServers()
	var out []MCPHealth
	seen := make(map[string]bool, len(configured))
	if host := c.Host(); host != nil {
		for _, srv := range host.Servers() {
			seen[srv.Name] = true
			out = append(out, MCPHealth{Name: srv.Name, Status: MCPHealthReady, Tools: srv.Tools})
		}
		for _, name := range host.ConnectingServers() {
			if !seen[name] {
				seen[name] = true
				out = append(out, MCPHealth{Name: name, Status: MCPHealthConnecting})
			}
		}
		for _, f := range host.Failures() {
			if !seen[f.Name] {
				seen[f.Name] = true
				health := MCPHealth{Name: f.Name, Status: MCPHealthFailed, Error: f.Error, HTTPStatus: f.HTTPStatus}
				if f.RequiresLaunchApproval {
					health.Status, health.Reason = MCPHealthPending, MCPApprovalAwaiting
					if f.DeclarationChanged {
						health.Reason = MCPApprovalChanged
					}
				}
				out = append(out, health)
			}
		}
	}
	catalog := c.MCPCatalogTools()
	for _, st := range configured {
		if seen[st.Entry.Name] {
			continue
		}
		health := MCPHealth{Name: st.Entry.Name,
			Status: configuredMCPStatus(st, catalog[st.Entry.Name]), Tools: catalog[st.Entry.Name]}
		if health.Status == MCPHealthPending {
			health.Reason = MCPApprovalAwaiting
			if st.Changed {
				health.Reason = MCPApprovalChanged
			}
		}
		out = append(out, health)
	}
	if out == nil {
		return []MCPHealth{}
	}
	return out
}

func configuredMCPStatus(st MCPServerState, tools int) string {
	switch {
	case st.Pending:
		return MCPHealthPending
	case !st.Enabled:
		return MCPHealthDisabled
	case tools > 0:
		return MCPHealthStandby
	default:
		return MCPHealthIdle
	}
}

// PendingMCPApproval is one configured server waiting for the user: why, and
// the launch line an approval would cover.
type PendingMCPApproval struct {
	Name, Launch string
	Reason       MCPApprovalReason
}

// PendingMCPApprovals lists the servers MCPServerHealth reports as pending,
// for surfaces that say so in text.
func (c *Controller) PendingMCPApprovals() []PendingMCPApproval {
	var out []PendingMCPApproval
	for _, h := range c.MCPServerHealth() {
		if h.Status != MCPHealthPending {
			continue
		}
		p := PendingMCPApproval{Name: h.Name, Reason: h.Reason}
		if entry, err := c.configuredMCPServer(h.Name); err == nil {
			p.Launch = mcpsetup.LaunchLine(entry)
		}
		out = append(out, p)
	}
	return out
}
