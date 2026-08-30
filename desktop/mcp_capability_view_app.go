package main

import "reasonix/internal/plugin"

// MCPCapabilityView is one row of the four-layer MCP capability matrix shown
// in the MCP panel: what this desktop declares and what live sessions
// negotiated.
type MCPCapabilityView struct {
	ID         string `json:"id"`
	Layer      string `json:"layer"`
	State      string `json:"state"`
	Negotiated bool   `json:"negotiated"`
	Detail     string `json:"detail"`
}

type MCPCapabilityMatrixView struct {
	Views       []MCPCapabilityView `json:"views"`
	HostProfile string              `json:"hostProfile"`
}

// MCPCapabilityMatrix returns the active tab's MCP capability matrix plus the
// host profile name for the status header.
func (a *App) MCPCapabilityMatrix() MCPCapabilityMatrixView {
	tab, ctrl := a.activeTabAndCtrl()
	if tab == nil || ctrl == nil {
		return MCPCapabilityMatrixView{}
	}
	capabilityViews, ok := ctrl.(interface {
		MCPCapabilityViews() []plugin.CapabilityView
	})
	if !ok {
		return MCPCapabilityMatrixView{}
	}
	views := capabilityViews.MCPCapabilityViews()
	plugin.SortCapabilityViews(views)
	out := make([]MCPCapabilityView, 0, len(views))
	profile := ""
	for _, v := range views {
		out = append(out, MCPCapabilityView{
			ID: v.ID, Layer: v.Layer, State: v.State, Negotiated: v.Negotiated, Detail: v.Detail,
		})
		if v.ID == plugin.CapabilityIDCore {
			if host := ctrl.Host(); host != nil {
				profile = host.Profile().String()
			}
		}
	}
	return MCPCapabilityMatrixView{Views: out, HostProfile: profile}
}
