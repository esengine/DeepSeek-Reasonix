package control

const (
	MCPHealthReady      = "ready"
	MCPHealthConnecting = "connecting"
	MCPHealthFailed     = "failed"
	MCPHealthPending    = "pending"
	MCPHealthDisabled   = "disabled"
	MCPHealthStandby    = "standby"
	MCPHealthIdle       = "idle"
)

// MCPHealth is the observable state of one server in this session. A cached
// tool surface can be callable before its process starts, so standby is distinct
// from both ready and idle.
type MCPHealth struct {
	Name       string
	Status     string
	Error      string
	HTTPStatus int
	Tools      int
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
				status := MCPHealthFailed
				if f.RequiresLaunchApproval {
					status = MCPHealthPending
				}
				out = append(out, MCPHealth{Name: f.Name, Status: status, Error: f.Error, HTTPStatus: f.HTTPStatus})
			}
		}
	}
	catalog := c.MCPCatalogTools()
	for _, st := range configured {
		if seen[st.Entry.Name] {
			continue
		}
		out = append(out, MCPHealth{Name: st.Entry.Name,
			Status: configuredMCPStatus(st, catalog[st.Entry.Name]), Tools: catalog[st.Entry.Name]})
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
