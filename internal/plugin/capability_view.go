package plugin

import (
	"sort"
	"strings"
)

// CapabilityViewLayer names for the four-layer capability matrix.
const (
	CapabilityIDProtocol    = "protocol"
	CapabilityIDCore        = "core"
	CapabilityIDInteractive = "interactive"
	CapabilityIDApps        = "apps"

	LayerProtocol    = "Protocol Connection"
	LayerCore        = "Core Host"
	LayerInteractive = "Interactive Host"
	LayerApps        = "Apps Host"

	CapabilityStateSupported   = "supported"
	CapabilityStateNegotiated  = "negotiated"
	CapabilityStateDegraded    = "degraded"
	CapabilityStateUnavailable = "unavailable"
)

// CapabilityView is one row of the public MCP capability matrix: what the host
// declares, whether the live sessions turned it into a negotiated capability,
// and a human-readable detail. Read-only diagnostics — there is no editable
// switch; capabilities follow the frontend's profile.
type CapabilityView struct {
	ID         string `json:"id"`
	Layer      string `json:"layer"`
	State      string `json:"state"`
	Negotiated bool   `json:"negotiated"`
	Detail     string `json:"detail"`
}

// CapabilityViews computes the host's four-layer capability matrix from the
// profile and the live sessions' negotiation results.
func (h *Host) CapabilityViews() []CapabilityView {
	profile := h.Profile()
	profileCaps := profile.Capabilities()

	clients := h.snapshotClients()
	anyConnected := false
	newestProtocol := ""
	elicitationLive := false
	appsLive := false
	for _, c := range clients {
		if c.closed.Load() {
			continue
		}
		anyConnected = true
		if c.protocolVersion > newestProtocol {
			newestProtocol = c.protocolVersion
		}
		if profileCaps.ElicitationForms && c.elicitationUsable() {
			elicitationLive = true
		}
		if profileCaps.AppsUI && c.capabilities.appsUI() {
			appsLive = true
		}
	}

	views := make([]CapabilityView, 0, 4)
	protocolDetail := "JSON-RPC protocol revision negotiated per server"
	protocolState := CapabilityStateSupported
	if anyConnected {
		protocolState = CapabilityStateNegotiated
		if newestProtocol != "" {
			protocolDetail = "Newest negotiated revision: " + newestProtocol
		}
	} else {
		protocolDetail = "No server connected yet; " + protocolDetail
	}
	views = append(views, CapabilityView{
		ID: CapabilityIDProtocol, Layer: LayerProtocol,
		State: protocolState, Negotiated: anyConnected, Detail: protocolDetail,
	})

	coreState := CapabilityStateSupported
	coreDetail := "Tools, prompts, resources over the core protocol"
	if anyConnected {
		coreState = CapabilityStateNegotiated
	}
	views = append(views, CapabilityView{
		ID: CapabilityIDCore, Layer: LayerCore,
		State: coreState, Negotiated: anyConnected, Detail: coreDetail,
	})

	interactiveState := CapabilityStateUnavailable
	interactiveDetail := "Host profile " + profile.String() + " declares no elicitation"
	if profileCaps.ElicitationForms || profileCaps.ElicitationURL {
		interactiveState = CapabilityStateSupported
		interactiveDetail = "Form and URL elicitation declared (profile " + profile.String() + ")"
		if elicitationLive {
			interactiveState = CapabilityStateNegotiated
			interactiveDetail = "Elicitation active on at least one compatible session"
		} else if anyConnected {
			interactiveDetail = "Declared; no connected session can deliver elicitation yet"
		}
	}
	views = append(views, CapabilityView{
		ID: CapabilityIDInteractive, Layer: LayerInteractive,
		State: interactiveState, Negotiated: elicitationLive, Detail: interactiveDetail,
	})

	appsState := CapabilityStateUnavailable
	appsDetail := "Host profile " + profile.String() + " declares no Apps extension"
	if profileCaps.AppsUI {
		appsState = CapabilityStateSupported
		appsDetail = "io.modelcontextprotocol/ui declared (profile " + profile.String() + ")"
		if appsLive {
			appsState = CapabilityStateNegotiated
			appsDetail = "Apps extension agreed by at least one server"
		} else if anyConnected {
			appsDetail = "Declared; no connected server answered with the Apps extension"
		}
	}
	views = append(views, CapabilityView{
		ID: CapabilityIDApps, Layer: LayerApps,
		State: appsState, Negotiated: appsLive, Detail: appsDetail,
	})
	return views
}

// fillServerNegotiation stamps one ServerStatus with the host profile and the
// per-server negotiated elicitation/Apps state.
func fillServerNegotiation(s *ServerStatus, profile HostProfile, c *Client) {
	s.HostProfile = profile.String()
	s.ElicitationNegotiated = profile.Capabilities().ElicitationForms && c.elicitationUsable()
	s.AppsNegotiated = profile.Capabilities().AppsUI && c.capabilities.appsUI()
}

// snapshotClients copies the host's client list under its lock.
func (h *Host) snapshotClients() []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Client, len(h.clients))
	copy(out, h.clients)
	return out
}

// FormatCapabilityViews renders the matrix as aligned text for /mcp status.
func FormatCapabilityViews(views []CapabilityView) string {
	widths := [2]int{}
	for _, v := range views {
		if len(v.Layer) > widths[0] {
			widths[0] = len(v.Layer)
		}
		if len(v.State) > widths[1] {
			widths[1] = len(v.State)
		}
	}
	rows := make([]string, 0, len(views))
	for _, v := range views {
		rows = append(rows, "  "+v.Layer+strings.Repeat(" ", widths[0]-len(v.Layer)+2)+
			v.State+strings.Repeat(" ", widths[1]-len(v.State)+2)+v.Detail)
	}
	return strings.Join(rows, "\n")
}

// SortCapabilityViews orders matrix rows protocol → core → interactive → apps.
func SortCapabilityViews(views []CapabilityView) {
	order := map[string]int{
		CapabilityIDProtocol: 0, CapabilityIDCore: 1,
		CapabilityIDInteractive: 2, CapabilityIDApps: 3,
	}
	sort.SliceStable(views, func(i, j int) bool {
		return order[views[i].ID] < order[views[j].ID]
	})
}
